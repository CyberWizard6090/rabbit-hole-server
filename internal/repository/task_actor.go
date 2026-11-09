package repository

import (
	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
)

func loadPublicUsers(db *gorm.DB, ids []uint) (map[uint]domain.CommentUser, error) {
	ids = uniqueIDs(ids)
	out := make(map[uint]domain.CommentUser, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []commentUserRow
	if err := db.Table("users").Select("id, username, first_name, last_name, avatar_url").
		Where("id IN ?", ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ID] = row.toDomain()
	}
	return out, nil
}

func loadSubtaskCounts(db *gorm.DB, tasks []domain.Task) error {
	ids := make([]uint, 0, len(tasks))
	for i := range tasks {
		ids = append(ids, tasks[i].ID)
	}
	if len(ids) == 0 {
		return nil
	}
	var rows []struct {
		ParentID uint
		Total    int
	}
	if err := db.Table("tasks").Select("parent_id, count(*) AS total").
		Where("parent_id IN ? AND deleted_at IS NULL", ids).Group("parent_id").Scan(&rows).Error; err != nil {
		return err
	}
	counts := make(map[uint]int, len(rows))
	for _, row := range rows {
		counts[row.ParentID] = row.Total
	}
	for i := range tasks {
		tasks[i].SubtaskCount = counts[tasks[i].ID]
	}
	return nil
}

func decorateTasks(db *gorm.DB, tasks []domain.Task) error {
	if err := loadSubtaskCounts(db, tasks); err != nil {
		return err
	}
	var userIDs []uint
	for i := range tasks {
		if tasks[i].Actor.IsUser() {
			userIDs = append(userIDs, tasks[i].UserID)
		}
		if r := tasks[i].RequesterInfo; r != nil && r.UserID != nil {
			userIDs = append(userIDs, *r.UserID)
		}
	}
	users, err := loadPublicUsers(db, userIDs)
	if err != nil {
		return err
	}
	for i := range tasks {
		task := &tasks[i]
		var creator *domain.CommentUser
		if task.Actor.IsUser() {
			if u, ok := users[task.UserID]; ok {
				creator = &u
			}
			task.Actor.ID = task.UserID
		}
		task.CreatedBy = domain.NewActorView(task.Actor, creator)
		if r := task.RequesterInfo; r != nil && r.UserID != nil {
			if u, ok := users[*r.UserID]; ok {
				r.User = &u
			} else {
				r.UserID = nil
			}
		}
	}
	return nil
}

func decorateTask(db *gorm.DB, task *domain.Task) error {
	items := []domain.Task{*task}
	if err := decorateTasks(db, items); err != nil {
		return err
	}
	*task = items[0]
	return nil
}
