package realtime

import (
	"strconv"
	"time"

	"rabbit-hole-server/internal/domain"
)

const (
	EventReady       = "ready"
	EventTaskCreated = "task.created"
	EventTaskUpdated = "task.updated"
	EventTaskDeleted = "task.deleted"
)

type Event struct {
	Type    string       `json:"type"`
	Topic   string       `json:"topic,omitempty"`
	SpaceID uint         `json:"space_id,omitempty"`
	ListID  uint         `json:"list_id,omitempty"`
	TaskID  uint         `json:"task_id,omitempty"`
	Task    *domain.Task `json:"task,omitempty"`
	ActorID uint         `json:"actor_id,omitempty"`
	At      time.Time    `json:"at"`
}

func ListTopic(listID uint) string   { return "list:" + strconv.FormatUint(uint64(listID), 10) }
func SpaceTopic(spaceID uint) string { return "space:" + strconv.FormatUint(uint64(spaceID), 10) }
