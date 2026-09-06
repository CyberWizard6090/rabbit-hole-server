package domain

import (
	"time"

	"gorm.io/gorm"
)

type Task struct {
	gorm.Model
	SpaceID      uint       `gorm:"index;not null;column:space_id" json:"-"`
	ListID       uint       `gorm:"index;not null;column:list_id" json:"list_id"`
	StatusID     uint       `gorm:"not null;column:status_id" json:"status_id"`
	ParentID     *uint      `json:"parent_id"`
	Title        string     `gorm:"not null" json:"title" binding:"required,min=3"`
	Description  string     `json:"description"`
	Priority     int        `gorm:"default:2" json:"priority"`
	UserID       uint       `gorm:"not null" json:"user_id"`
	Assignees    []User     `gorm:"many2many:task_assignees;constraint:OnDelete:CASCADE" json:"assignees"`
	Tags         []Tag      `gorm:"many2many:task_tags;constraint:OnDelete:CASCADE" json:"tags"`
	StartDate    *time.Time `json:"start_date"`
	DueDate      *time.Time `json:"due_date"`
	TimeEstimate int        `gorm:"default:0" json:"time_estimate"`
	TimeSpent    int        `gorm:"default:0" json:"time_spent"`
}

type TaskRepository interface {
	Create(task *Task) error
	GetAll(uid uint, limit, offset int) ([]Task, int64, error)
	GetByID(id uint, uid uint) (*Task, error)
	GetByListID(ListID uint) ([]Task, error)
	Update(task *Task) error
	Delete(id uint, uid uint) error
	AddTags(taskID uint, tagIDs []uint) error
}
