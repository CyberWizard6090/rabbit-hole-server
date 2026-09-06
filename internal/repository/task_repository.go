package repository

import (
	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
)

type TaskRepository interface {
	Create(task *domain.Task, assigneeIDs []uint, tagIDs []uint) error
	GetAll(userID uint, limit, offset int) ([]domain.Task, int64, error)
	GetByID(id uint, userID uint) (*domain.Task, error)
	Update(task *domain.Task) error
	Delete(id uint, userID uint) error
	AddTags(taskID uint, tagIDs []uint) error
}

type taskRepository struct {
	db *gorm.DB
}

func NewTaskRepository(db *gorm.DB) TaskRepository {
	return &taskRepository{db: db}
}

func (r *taskRepository) Create(task *domain.Task, assigneeIDs []uint, tagIDs []uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if len(assigneeIDs) > 0 {
			var users []domain.User
			if err := tx.Find(&users, assigneeIDs).Error; err != nil {
				return err
			}
			task.Assignees = users
		}

		if len(tagIDs) > 0 {
			var tags []domain.Tag
			if err := tx.Find(&tags, tagIDs).Error; err != nil {
				return err
			}
			task.Tags = tags
		}

		return tx.Create(task).Error
	})
}

func (r *taskRepository) GetAll(userID uint, limit, offset int) ([]domain.Task, int64, error) {
	var tasks []domain.Task
	var total int64

	r.db.Model(&domain.Task{}).Where("user_id = ?", userID).Count(&total)

	err := r.db.Where("user_id = ?", userID).
		Preload("Assignees").
		Preload("Tags").
		Limit(limit).
		Offset(offset).
		Order("created_at DESC").
		Find(&tasks).Error

	return tasks, total, err
}

func (r *taskRepository) GetByID(id uint, userID uint) (*domain.Task, error) {
	var task domain.Task
	err := r.db.Where("id = ? AND user_id = ?", id, userID).
		Preload("Assignees").
		Preload("Tags").
		First(&task).Error
	return &task, err
}

func (r *taskRepository) Update(task *domain.Task) error {
	return r.db.Save(task).Error
}

func (r *taskRepository) Delete(id uint, userID uint) error {
	return r.db.Where("id = ? AND user_id = ?", id, userID).Delete(&domain.Task{}).Error
}

func (r *taskRepository) AddTags(taskID uint, tagIDs []uint) error {
	if len(tagIDs) == 0 {
		return nil
	}

	var task domain.Task
	if err := r.db.First(&task, taskID).Error; err != nil {
		return err
	}

	var tags []domain.Tag
	if err := r.db.Find(&tags, tagIDs).Error; err != nil {
		return err
	}

	return r.db.Model(&task).Association("Tags").Append(tags)
}
