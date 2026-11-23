//go:build ee

package entity

import "time"

// CompletionLog retains a submitted message and its response independently of conversation history.
type CompletionLog struct {
	ID                string     `gorm:"column:id;primaryKey;size:32"`
	UserID            string     `gorm:"column:user_id;size:32;index"`
	TenantID          string     `gorm:"column:tenant_id;size:32;index"`
	ResourceType      string     `gorm:"column:resource_type;size:16"`
	ResourceID        string     `gorm:"column:resource_id;size:32;index"`
	SessionID         string     `gorm:"column:session_id;size:32;index"`
	UserMessage       string     `gorm:"column:user_message;type:longtext"`
	AssistantResponse string     `gorm:"column:assistant_response;type:longtext"`
	Status            string     `gorm:"column:status;size:16"`
	CreatedAt         time.Time  `gorm:"column:created_at;index"`
	FinishedAt        *time.Time `gorm:"column:finished_at"`
}

func (CompletionLog) TableName() string { return "completion_log" }
