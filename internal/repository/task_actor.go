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

func decorateTasks(db *gorm.DB, tasks []domain.Task) error {
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
