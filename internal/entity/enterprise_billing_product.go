package entity

type BillingProduct struct {
	ID                       string `gorm:"column:id;primaryKey;size:32" json:"id"`
	Name                     string `gorm:"column:name;size:255;not null;index" json:"name"`
	Priority                 int    `gorm:"column:priority;not null" json:"priority"`
	QuotaApps                int    `gorm:"column:quota_apps;not null" json:"quota_apps"`
	QuotaMembers             int    `gorm:"column:quota_members;not null" json:"quota_members"`
	QuotaStorage             int64  `gorm:"column:quota_storage;not null" json:"quota_storage"`
	TaskPriority             string `gorm:"column:task_priority;not null" json:"task_priority"`
	ProductType              string `gorm:"column:product_type;not null" json:"product_type"`
	Version                  int    `gorm:"column:version;not null" json:"version"`
	QuotaPoints              *int64 `gorm:"column:quota_points" json:"quota_points,omitempty"`
	APIRequestLimitPerMinute *int64 `gorm:"column:api_request_limit_per_minute" json:"api_request_limit_per_minute,omitempty"`
	BaseModel
}

func (BillingProduct) TableName() string {
	return "billing_product"
}
