package repository

import (
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/persistence"
)

const maxLockAttempts = 3

func lockExistingLists(tx *gorm.DB, ids ...uint) error {
	return lockExistingListsAs(tx, "UPDATE", ids...)
}

func lockExistingListsShared(tx *gorm.DB, ids ...uint) error {
	return lockExistingListsAs(tx, "KEY SHARE", ids...)
}

func lockListForAppend(tx *gorm.DB, id uint) error {
	return lockExistingListsAs(tx, "NO KEY UPDATE", id)
}

func lockExistingListsAs(tx *gorm.DB, strength string, ids ...uint) error {
	unique := uniqueIDs(ids)
	var rows []persistence.List
	if err := tx.Clauses(clause.Locking{Strength: strength}).
		Select("id").Where("id IN ?", unique).Order("id").Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) != len(unique) {
		return domain.ErrNotFound
	}
	return nil
}

func lockTaskRow(tx *gorm.DB, id uint) (persistence.Task, error) {
	for attempt := 0; attempt < maxLockAttempts; attempt++ {
		var seen persistence.Task
		if err := tx.Select("id", "list_id").Where("id = ?", id).First(&seen).Error; err != nil {
			return persistence.Task{}, err
		}
		if err := lockExistingListsShared(tx, seen.ListID); err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return persistence.Task{}, gorm.ErrRecordNotFound
			}
			return persistence.Task{}, err
		}
		var task persistence.Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&task).Error; err != nil {
			return persistence.Task{}, err
		}
		if task.ListID == seen.ListID {
			return task, nil
		}
	}
	return persistence.Task{}, domain.ErrConcurrentMove
}

func softDeleteTaskTree(tx *gorm.DB, id uint) error {
	if _, err := lockTaskRow(tx, id); err != nil {
		return err
	}
	res := tx.Exec(
		"WITH RECURSIVE tree AS ("+
			"SELECT id FROM tasks WHERE id = ? AND deleted_at IS NULL "+
			"UNION SELECT t.id FROM tasks t JOIN tree ON t.parent_id = tree.id WHERE t.deleted_at IS NULL) "+
			"UPDATE tasks SET deleted_at = now() WHERE id IN (SELECT id FROM tree)", id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func softDeleteList(tx *gorm.DB, id uint) error {
	if err := lockExistingLists(tx, id); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return gorm.ErrRecordNotFound
		}
		return err
	}
	if err := tx.Exec("UPDATE tasks SET deleted_at = now() WHERE list_id = ? AND deleted_at IS NULL", id).Error; err != nil {
		return err
	}
	return tx.Exec("UPDATE lists SET deleted_at = now() WHERE id = ? AND deleted_at IS NULL", id).Error
}

func softDeleteFolderTree(tx *gorm.DB, id uint) error {
	var folderIDs []uint
	if err := tx.Raw(
		"WITH RECURSIVE tree AS ("+
			"SELECT id FROM folders WHERE id = ? AND deleted_at IS NULL "+
			"UNION SELECT f.id FROM folders f JOIN tree ON f.parent_id = tree.id WHERE f.deleted_at IS NULL) "+
			"SELECT id FROM tree", id,
	).Scan(&folderIDs).Error; err != nil {
		return err
	}
	if len(folderIDs) == 0 {
		return gorm.ErrRecordNotFound
	}
	var listIDs []uint
	if err := tx.Raw(
		"SELECT id FROM lists WHERE folder_id IN ? AND deleted_at IS NULL ORDER BY id FOR UPDATE", folderIDs,
	).Scan(&listIDs).Error; err != nil {
		return err
	}
	if len(listIDs) > 0 {
		if err := tx.Exec("UPDATE tasks SET deleted_at = now() WHERE list_id IN ? AND deleted_at IS NULL", listIDs).Error; err != nil {
			return err
		}
		if err := tx.Exec("UPDATE lists SET deleted_at = now() WHERE id IN ?", listIDs).Error; err != nil {
			return err
		}
	}
	return tx.Exec("UPDATE folders SET deleted_at = now() WHERE id IN ?", folderIDs).Error
}

func softDeleteSpaceTrees(tx *gorm.DB, spaceIDs []uint) error {
	if len(spaceIDs) == 0 {
		return nil
	}
	var locked []uint
	if err := tx.Raw(
		"SELECT id FROM lists WHERE space_id IN ? AND deleted_at IS NULL ORDER BY id FOR UPDATE", spaceIDs,
	).Scan(&locked).Error; err != nil {
		return err
	}
	for _, target := range []struct{ table, column string }{
		{"tasks", "space_id"}, {"lists", "space_id"}, {"folders", "space_id"}, {"spaces", "id"},
	} {
		if err := tx.Exec(
			"UPDATE "+target.table+" SET deleted_at = now() WHERE "+target.column+" IN ? AND deleted_at IS NULL", spaceIDs,
		).Error; err != nil {
			return err
		}
	}
	return nil
}

func softDeleteSpace(tx *gorm.DB, id uint) error {
	var ids []uint
	if err := tx.Raw("SELECT id FROM spaces WHERE id = ? AND deleted_at IS NULL FOR UPDATE", id).Scan(&ids).Error; err != nil {
		return err
	}
	if len(ids) == 0 {
		return gorm.ErrRecordNotFound
	}
	return softDeleteSpaceTrees(tx, ids)
}
