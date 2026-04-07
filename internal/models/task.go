package models

import (
	"time"

	"github.com/google/uuid"
)

// TaskStatus is the lifecycle state of a task.
type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusCompleted  TaskStatus = "completed"
	TaskStatusBlocked    TaskStatus = "blocked"
)

// Task is a unit of work that can be assigned to an agent.
type Task struct {
	ID            uuid.UUID   `json:"id"`
	Title         string      `json:"title"`
	Description   string      `json:"description"`
	Status        TaskStatus  `json:"status"`
	PreferredRole string      `json:"preferred_role,omitempty"`
	Tags          []string    `json:"tags,omitempty"`
	AssigneeID    *uuid.UUID  `json:"assigneeId,omitempty"`
	AssigneeName  string      `json:"assigneeName,omitempty"`
	CreatedBy     uuid.UUID   `json:"createdBy"`
	Dependencies  []uuid.UUID `json:"dependencies,omitempty"`
	CreatedAt     time.Time   `json:"createdAt"`
	CompletedAt   *time.Time  `json:"completedAt,omitempty"`
}
