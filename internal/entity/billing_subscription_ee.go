package entity

import "time"

type BillingSubscription struct {
	ID                 string     `gorm:"column:id;primaryKey;size:32" json:"id"`
	TenantID           string     `gorm:"column:tenant_id;size:32;not null;index" json:"tenant_id"`
	PlanName           string     `gorm:"column:plan_name;size:255;index" json:"plan_name"`
	SubscriptionStatus string     `gorm:"column:subscription_status;size:255;index" json:"subscription_status"`
	StartTime          *time.Time `gorm:"column:start_time;index" json:"start_time,omitempty"`
	EndTime            *time.Time `gorm:"column:end_time;index" json:"end_time,omitempty"`
	AddonStorageBytes  *int64     `gorm:"column:addon_storage_bytes" json:"addon_storage_bytes,omitempty"`
	TargetStorageBytes *int64     `gorm:"column:target_storage_bytes" json:"target_storage_bytes,omitempty"`
	BaseModel
}

func (BillingSubscription) TableName() string {
	return "billing_subscription"
}
