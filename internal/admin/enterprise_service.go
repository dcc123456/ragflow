//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

package admin

import (
	"errors"
	"fmt"
	"os"
	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/engine/elasticsearch"
	"ragflow/internal/entity"
	"ragflow/internal/server"
	"sort"
	"strings"
	"time"
)

// Role management methods

// ListRoles list all roles
func (s *Service) ListRoles() ([]map[string]interface{}, error) {
	result := []map[string]interface{}{
		{
			"command": "list_roles",
			"error":   "'list roles' is not supported",
		},
	}

	return result, nil
}

// CreateRole create a new role
func (s *Service) CreateRole(roleName, description string) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"command":     "create_role",
		"role_name":   roleName,
		"description": description,
		"error":       "'create role' is not supported",
	}

	return result, nil
}

// ShowRole show role details
func (s *Service) ShowRole(roleName string) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"command":   "show_role",
		"role_name": roleName,
		"error":     "'show role' is not supported",
	}

	return result, nil

}

// UpdateRole update role
func (s *Service) UpdateRole(roleName, description string) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"command":     "update_role",
		"role_name":   roleName,
		"description": description,
		"error":       "'update role' is not supported",
	}

	return result, nil
}

// DropRole drop role
func (s *Service) DropRole(roleName string) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"command":   "drop_role",
		"role_name": roleName,
		"error":     "'drop role' is not supported",
	}

	return result, nil
}

// ShowRolePermission get role permissions
func (s *Service) ShowRolePermission(roleName string) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"command":   "show_role_permission",
		"role_name": roleName,
		"error":     "'show role permissions' is not supported",
	}

	return result, nil
}

// GrantRolePermission grant permission to role
func (s *Service) GrantRolePermission(roleName string, actions []string, resource string) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"command":   "grant_role_permission",
		"role_name": roleName,
		"actions":   actions,
		"resource":  resource,
		"error":     "'grant role permission' is not supported",
	}

	return result, nil
}

// RevokeRolePermission revoke permission from role
func (s *Service) RevokeRolePermission(roleName string, actions []string, resource string) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"command":   "revoke_role_permission",
		"role_name": roleName,
		"actions":   actions,
		"resource":  resource,
		"error":     "'revoke role permission' is not supported",
	}

	return result, nil
}

// ListResources list role resources
func (s *Service) ListResources() (map[string]interface{}, error) {
	result := map[string]interface{}{
		"command": "list_resources",
		"error":   "'list resources for role' is not supported",
	}

	return result, nil
}

func (s *Service) ShowRoleDefaultModels(roleName string) ([]map[string]interface{}, error) {
	return []map[string]interface{}{
		{
			"command":   "show_role_default_models",
			"role_name": roleName,
			"error":     "'show role default models' is not supported",
		},
	}, nil
}

// SetRoleDefaultModel set role default model
func (s *Service) SetRoleDefaultModel(roleName, modelID, modelType string) (map[string]interface{}, error) {
	return map[string]interface{}{
		"command":    "set_role_default_model",
		"role_name":  roleName,
		"model_id":   modelID,
		"model_type": modelType,
		"error":      "'set role default model' is not supported",
	}, nil
}

// ResetRoleDefaultModel reset role default model
func (s *Service) ResetRoleDefaultModel(roleName, modelType string) (map[string]interface{}, error) {
	return map[string]interface{}{
		"command":    "reset_role_default_model",
		"role_name":  roleName,
		"model_type": modelType,
		"error":      "'reset role default model' is not supported",
	}, nil
}

// ListModelProviders list model providers
func (s *Service) ListModelProviders() ([]map[string]interface{}, error) {
	return []map[string]interface{}{
		{
			"command": "list_model_providers",
			"error":   "'list model providers' is not supported",
		},
	}, nil
}

// AddModelProvider Add model provider
func (s *Service) AddModelProvider(userID, providerName string) (map[string]interface{}, error) {

	return map[string]interface{}{
		"command":     "add_model_provider",
		"user_id":     userID,
		"provider_id": providerName,
		"error":       "'add model provider' is not supported",
	}, nil
}

// DeleteModelProviders delete model providers
func (s *Service) DeleteModelProviders(userID string, providerNames []string) (map[string]interface{}, error) {
	return map[string]interface{}{
		"command":        "delete_model_providers",
		"user_id":        userID,
		"provider_names": providerNames,
		"error":          "'delete model providers' is not supported",
	}, nil
}

// ListModelInstances list model instances
func (s *Service) ListModelInstances(userID, providerName string) ([]map[string]interface{}, error) {

	return []map[string]interface{}{
		{
			"command":     "list_model_instances",
			"user_id":     userID,
			"provider_id": providerName,
			"error":       "'list model instances' is not supported",
		},
	}, nil
}

// ShowProviderInstance show provider instance
func (s *Service) ShowProviderInstance(userID, providerName, instanceName string) (map[string]interface{}, error) {

	return map[string]interface{}{
		"command":       "show_provider_instance",
		"user_id":       userID,
		"provider_id":   providerName,
		"instance_name": instanceName,
		"error":         "'show provider instance' is not supported",
	}, nil
}

// ShowProviderInstanceBalance show provider instance balance
func (s *Service) ShowProviderInstanceBalance(userID, providerName, instanceName string) (map[string]interface{}, error) {
	return map[string]interface{}{
		"command":       "show_provider_instance_balance",
		"user_id":       userID,
		"provider_id":   providerName,
		"instance_name": instanceName,
		"error":         "'show provider instance balance' is not supported",
	}, nil
}

// CheckInstanceConnection check instance connection
func (s *Service) CheckInstanceConnection(userID, providerName, instanceName string) (map[string]interface{}, error) {
	return map[string]interface{}{
		"command":       "check_instance_connection",
		"user_id":       userID,
		"provider_id":   providerName,
		"instance_name": instanceName,
		"error":         "'check instance connection' is not supported",
	}, nil
}

// CheckProviderConnection check provider connection
func (s *Service) CheckProviderConnection(userID, providerName, region, apiKey, baseURL string) (map[string]interface{}, error) {
	return map[string]interface{}{
		"command":     "check_provider_connection",
		"user_id":     userID,
		"provider_id": providerName,
		"region":      region,
		"api_key":     apiKey,
		"base_url":    baseURL,
	}, nil
}

// AlterProviderInstance alter provider instance
func (s *Service) AlterProviderInstance(userID, providerName, instanceName, newInstanceName, newAPIKey string) (map[string]interface{}, error) {
	return map[string]interface{}{
		"command":           "alter_provider_instance",
		"user_id":           userID,
		"provider_id":       providerName,
		"instance_name":     instanceName,
		"new_instance_name": newInstanceName,
		"new_api_key":       newAPIKey,
		"error":             "'alter provider instance' is not supported",
	}, nil
}

// AddModelInstance Add model instance
func (s *Service) AddModelInstance(userID, providerName, instanceName string) (map[string]interface{}, error) {

	return map[string]interface{}{
		"command":       "add_model_instance",
		"user_id":       userID,
		"provider_id":   providerName,
		"instance_name": instanceName,
		"error":         "'add model instance' is not supported",
	}, nil
}

// DeleteModelInstances delete model instances
func (s *Service) DeleteModelInstances(userID, providerName string, instances []string) (map[string]interface{}, error) {
	return map[string]interface{}{
		"command":     "delete_model_instances",
		"user_id":     userID,
		"provider_id": providerName,
		"instances":   instances,
		"error":       "'delete model instances' is not supported",
	}, nil
}

// ListInstanceModels list models for instance
func (s *Service) ListInstanceModels(userID, providerName, instanceName string) ([]map[string]interface{}, error) {
	return []map[string]interface{}{
		{
			"command":       "list_instance_models",
			"user_id":       userID,
			"provider_id":   providerName,
			"instance_name": instanceName,
			"error":         "'list instance models' is not supported",
		},
	}, nil
}

func (s *Service) EnableOrDisableModel(userID, providerName, instanceName, modelName, modelID, status string) (map[string]interface{}, error) {

	return map[string]interface{}{
		"command":       "enable_or_disable_model",
		"user_id":       userID,
		"provider_id":   providerName,
		"instance_name": instanceName,
		"model_name":    modelName,
		"model_id":      modelID,
		"status":        status,
		"error":         "'enable or disable model' is not supported",
	}, nil
}

// AddModel Add model

// AddModels Add models
func (s *Service) AddModels(userID, providerName, instanceName string, modelNames []string) (map[string]interface{}, error) {

	return map[string]interface{}{
		"command":       "add_model",
		"user_id":       userID,
		"provider_id":   providerName,
		"instance_name": instanceName,
		"model_names":   modelNames,
		"error":         "'add model' is not supported",
	}, nil
}

// DeleteModels delete models
func (s *Service) DeleteModels(userID, providerName, instanceName string, models []string) (map[string]interface{}, error) {
	return map[string]interface{}{
		"command":       "delete_models",
		"user_id":       userID,
		"provider_id":   providerName,
		"instance_name": instanceName,
		"models":        models,
		"error":         "'delete models' is not supported",
	}, nil
}

func (s *Service) GetSystemFingerprint() (map[string]interface{}, error) {
	result := map[string]interface{}{
		"command": "get_system_fingerprint",
		"error":   "'get system fingerprint' is not supported",
	}

	return result, nil
}

func (s *Service) SetSystemLicense(license string) error {
	return errors.New("'set system license' is not supported")
}

func (s *Service) ShowSystemLicense(check bool) (map[string]interface{}, error) {
	var result map[string]interface{}
	if check {
		result = map[string]interface{}{
			"command": "check_system_license",
			"error":   "'check system license' is not supported",
		}

	} else {
		result = map[string]interface{}{
			"command": "show_system_license",
			"error":   "'show system license' is not supported",
		}
	}

	return result, nil
}

func (s *Service) UpdateSystemLicenseConfig(timeRecordSaveInterval, timeRecordTaskDuration int64) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"command":                   "update_system_license_config",
		"time_record_save_interval": timeRecordSaveInterval,
		"time_record_task_duration": timeRecordTaskDuration,
		"error":                     "'update system license config' is not supported",
	}

	return result, nil
}

// ShowUserActivity show user activity for enterprise edition
func (s *Service) ShowUserActivity(email string, days int) (map[string]interface{}, error) {
	// Query user by email
	var user entity.User
	err := dao.DB.Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, common.ErrUserNotFound
	}

	result := map[string]interface{}{
		"email":    user.Email,
		"nickname": user.Nickname,
		"days":     days,
		"error":    "'show user activity' is not supported",
	}

	return result, nil
}

// ShowUserDatasetSummary show user dataset summary for enterprise edition
func (s *Service) ShowUserDatasetSummary(email, dataset string) (map[string]interface{}, error) {
	// Query user by email
	var user entity.User
	err := dao.DB.Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, common.ErrUserNotFound
	}

	kbs, err := s.kbDAO.GetKBByNameAndUserID(dataset, user.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to query dataset: %w", err)
	}
	if len(kbs) == 0 {
		return nil, fmt.Errorf("dataset '%s' not found for user '%s'", dataset, email)
	}

	kb := kbs[0]

	var totalDocSize int64
	dao.DB.Model(&entity.Document{}).
		Select("COALESCE(SUM(size), 0)").
		Where("kb_id = ?", kb.ID).
		Scan(&totalDocSize)

	result := map[string]interface{}{
		"Email":        user.Email,
		"Dataset_Name": kb.Name,
		"Dataset_ID":   kb.ID,
		"Doc_Count":    common.FormatNumber(kb.DocNum),
		"Doc_Size":     common.FormatBytes(totalDocSize),
		"Token_Count":  common.FormatNumber(kb.TokenNum),
		"Chunk_Count":  common.FormatNumber(kb.ChunkNum),
	}
	return result, nil
}

// GetUserSummary get user summary for enterprise edition
func (s *Service) ShowUserSummary(email string) (map[string]interface{}, error) {
	// Query user by email
	var user entity.User
	err := dao.DB.Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, common.ErrUserNotFound
	}

	tenantIDs, err := s.userTenantDAO.GetTenantIDsByUserID(user.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tenant IDs: %w", err)
	}

	var primaryTenantID string
	if len(tenantIDs) > 0 {
		primaryTenantID = tenantIDs[0]
	}

	planName := "Free"
	if primaryTenantID != "" {
		var bs entity.BillingSubscription
		if err := dao.DB.Where("tenant_id = ? AND subscription_status = ?", primaryTenantID, "active").First(&bs).Error; err == nil {
			if bs.PlanName != "" {
				planName = bs.PlanName
			}
		}
	}

	var totalKB, totalDoc, totalChat, totalAgent, totalAPIToken int64
	var totalDocSize, totalChunkNum, totalTokenNum int64

	if len(tenantIDs) > 0 {
		dao.DB.Model(&entity.Knowledgebase{}).
			Where("tenant_id IN ? AND status = ?", tenantIDs, string(entity.StatusValid)).
			Count(&totalKB)

		var kbAgg struct {
			TotalChunkNum int64
			TotalTokenNum int64
		}
		dao.DB.Model(&entity.Knowledgebase{}).
			Select("COALESCE(SUM(chunk_num), 0) as total_chunk_num, COALESCE(SUM(token_num), 0) as total_token_num").
			Where("tenant_id IN ? AND status = ?", tenantIDs, string(entity.StatusValid)).
			Scan(&kbAgg)
		totalChunkNum = kbAgg.TotalChunkNum
		totalTokenNum = kbAgg.TotalTokenNum

		var docAgg struct {
			TotalCount int64
			TotalSize  int64
		}
		kbSubQuery := dao.DB.Model(&entity.Knowledgebase{}).
			Select("id").
			Where("tenant_id IN ? AND status = ?", tenantIDs, string(entity.StatusValid))
		dao.DB.Model(&entity.Document{}).
			Select("COUNT(*) as total_count, COALESCE(SUM(size), 0) as total_size").
			Where("kb_id IN (?)", kbSubQuery).
			Scan(&docAgg)
		totalDoc = docAgg.TotalCount
		totalDocSize = docAgg.TotalSize

		dao.DB.Model(&entity.Chat{}).
			Where("tenant_id IN ? AND status = ?", tenantIDs, "1").
			Count(&totalChat)

		dao.DB.Model(&entity.UserCanvas{}).
			Where("user_id IN ? AND canvas_category = ?", tenantIDs, "agent_canvas").
			Count(&totalAgent)

		dao.DB.Model(&entity.APIToken{}).
			Where("tenant_id IN ?", tenantIDs).
			Count(&totalAPIToken)
	}

	lastLogin := "N/A"
	if user.LastLoginTime != nil {
		lastLogin = user.LastLoginTime.Format("2006-01-02 15:04:05")
	}

	tenantIDDisplay := "N/A"
	if primaryTenantID != "" {
		tenantIDDisplay = primaryTenantID
	}

	indexCount := totalKB
	indexSize := totalDocSize / 2

	result := map[string]interface{}{
		"Email":           user.Email,
		"Tenant_ID":       tenantIDDisplay,
		"Plan":            planName,
		"Dataset_Count":   totalKB,
		"Doc_Count":       common.FormatNumber(totalDoc),
		"Doc_Size":        common.FormatBytes(totalDocSize),
		"Chunk_Count":     common.FormatNumber(totalChunkNum),
		"Token_Count":     common.FormatNumber(totalTokenNum),
		"Index_Count":     indexCount,
		"Index_Size":      common.FormatBytes(indexSize),
		"Chat_Count":      totalChat,
		"Agent_Count":     totalAgent,
		"API_Token_Count": totalAPIToken,
		"Last_Login":      lastLogin,
	}

	return result, nil
}

// ShowUserStorage show user storage for enterprise edition
func (s *Service) ShowUserStorage(email string) (map[string]interface{}, error) {
	// Query user by email
	var user entity.User
	err := dao.DB.Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, common.ErrUserNotFound
	}
	tenantID := user.ID

	kbSubQuery := dao.DB.Model(&entity.Knowledgebase{}).
		Select("id").
		Where("tenant_id = ? AND status = '1'", tenantID)

	var docAgg struct {
		TotalCount int64
		TotalSize  int64
	}
	dao.DB.Model(&entity.Document{}).
		Select("COUNT(*) as total_count, COALESCE(SUM(size), 0) as total_size").
		Where("kb_id IN (?)", kbSubQuery).
		Scan(&docAgg)

	var topFiles []struct {
		Name string `gorm:"column:name"`
		Size int64  `gorm:"column:size"`
	}
	dao.DB.Model(&entity.Document{}).
		Select("name, size").
		Where("kb_id IN (?) AND status = '1'", kbSubQuery).
		Order("size DESC").
		Limit(10).
		Find(&topFiles)

	fileList := make([]map[string]interface{}, 0, len(topFiles))
	for _, f := range topFiles {
		fileList = append(fileList, map[string]interface{}{
			"name": f.Name,
			"size": common.FormatBytes(f.Size),
		})
	}

	result := map[string]interface{}{
		"Email":      user.Email,
		"Tenant_ID":  tenantID,
		"File_Count": common.FormatNumber(docAgg.TotalCount),
		"Total_Size": common.FormatBytes(docAgg.TotalSize),
		"files":      fileList,
	}

	return result, nil
}

// ShowUserQuota show user quota for enterprise edition
func (s *Service) ShowUserQuota(email string) (map[string]interface{}, error) {
	// Query user by email
	var user entity.User
	err := dao.DB.Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, common.ErrUserNotFound
	}
	tenantIDs, err := s.userTenantDAO.GetTenantIDsByUserID(user.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tenant IDs: %w", err)
	}

	var primaryTenantID string
	if len(tenantIDs) > 0 {
		primaryTenantID = tenantIDs[0]
	}

	planName := "Free"
	var product entity.BillingProduct
	var subscription entity.BillingSubscription
	var addonStorageBytes int64

	if primaryTenantID != "" {
		if err := dao.DB.Where("tenant_id = ?", primaryTenantID).Order("create_time DESC").First(&subscription).Error; err == nil {
			if subscription.SubscriptionStatus == "active" && subscription.PlanName != "" {
				planName = subscription.PlanName
			}
			if subscription.AddonStorageBytes != nil {
				addonStorageBytes = *subscription.AddonStorageBytes
			}
		}
	}

	if err := dao.DB.Where("name = ?", planName).Order("version DESC").First(&product).Error; err != nil {
		product = entity.BillingProduct{
			QuotaApps:    0,
			QuotaMembers: 0,
			QuotaStorage: 0,
		}
	}

	storageLimit := product.QuotaStorage + addonStorageBytes

	var numApps, numMembers, numAPITokens, numKB, numDoc, numChunk, numToken int64
	var numStorageBytes int64

	if len(tenantIDs) > 0 {
		var chatCount, searchCount, agentCount, memoryCount int64
		dao.DB.Model(&entity.Chat{}).Where("tenant_id IN ? AND status = ?", tenantIDs, "1").Count(&chatCount)
		dao.DB.Model(&entity.Search{}).Where("tenant_id IN ? AND status = ?", tenantIDs, "1").Count(&searchCount)
		dao.DB.Model(&entity.UserCanvas{}).Where("user_id IN ? AND canvas_category = ?", tenantIDs, "agent_canvas").Count(&agentCount)
		dao.DB.Model(&entity.Memory{}).Where("tenant_id IN ?", tenantIDs).Count(&memoryCount)
		numApps = chatCount + searchCount + agentCount + memoryCount

		dao.DB.Model(&entity.UserTenant{}).Where("tenant_id IN ?", tenantIDs).Count(&numMembers)
		dao.DB.Model(&entity.APIToken{}).Where("tenant_id IN ?", tenantIDs).Count(&numAPITokens)

		dao.DB.Model(&entity.Knowledgebase{}).Where("tenant_id IN ? AND status = ?", tenantIDs, string(entity.StatusValid)).Count(&numKB)

		var kbAgg struct {
			TotalChunkNum int64
			TotalTokenNum int64
		}
		dao.DB.Model(&entity.Knowledgebase{}).
			Select("COALESCE(SUM(chunk_num), 0) as total_chunk_num, COALESCE(SUM(token_num), 0) as total_token_num").
			Where("tenant_id IN ? AND status = ?", tenantIDs, string(entity.StatusValid)).
			Scan(&kbAgg)
		numChunk = kbAgg.TotalChunkNum
		numToken = kbAgg.TotalTokenNum

		kbSubQuery := dao.DB.Model(&entity.Knowledgebase{}).
			Select("id").
			Where("tenant_id IN ? AND status = ?", tenantIDs, string(entity.StatusValid))
		var docAgg struct {
			TotalCount int64
			TotalSize  int64
		}
		dao.DB.Model(&entity.Document{}).
			Select("COUNT(*) as total_count, COALESCE(SUM(size), 0) as total_size").
			Where("kb_id IN (?)", kbSubQuery).
			Scan(&docAgg)
		numDoc = docAgg.TotalCount
		numStorageBytes = docAgg.TotalSize
	}

	quotaPointsUsed := "-"
	quotaPointsLimit := "-"
	if product.QuotaPoints != nil {
		quotaPointsUsed = "0"
		quotaPointsLimit = common.FormatNumber(*product.QuotaPoints)
	}

	apiLimitDisplay := "500"
	if product.APIRequestLimitPerMinute != nil {
		apiLimitDisplay = common.FormatNumber(*product.APIRequestLimitPerMinute)
	}

	storageLimitDisplay := "-"
	if storageLimit > 0 {
		storageLimitDisplay = common.FormatBytes(storageLimit)
	}
	appsLimitDisplay := "-"
	if product.QuotaApps > 0 {
		appsLimitDisplay = fmt.Sprintf("%d", product.QuotaApps)
	}
	membersLimitDisplay := "-"
	if product.QuotaMembers > 0 {
		membersLimitDisplay = fmt.Sprintf("%d", product.QuotaMembers)
	}

	rows := []map[string]interface{}{
		{"Metric": "Email", "Used": user.Email, "Limit": "-"},
		{"Metric": "Plan", "Used": planName, "Limit": "-"},
		{"Metric": "Storage", "Used": common.FormatBytes(numStorageBytes), "Limit": storageLimitDisplay},
		{"Metric": "Apps_Chat_Search_Agent", "Used": numApps, "Limit": appsLimitDisplay},
		{"Metric": "Members", "Used": numMembers, "Limit": membersLimitDisplay},
		{"Metric": "API_Tokens", "Used": numAPITokens, "Limit": "-"},
		{"Metric": "Knowledgebases", "Used": numKB, "Limit": "-"},
		{"Metric": "Documents", "Used": numDoc, "Limit": "-"},
		{"Metric": "Chunks", "Used": common.FormatNumber(numChunk), "Limit": "-"},
		{"Metric": "Tokens_LLM_used", "Used": common.FormatNumber(numToken), "Limit": "-"},
		{"Metric": "Points", "Used": quotaPointsUsed, "Limit": quotaPointsLimit},
		{"Metric": "API_Requests_per_min", "Used": "-", "Limit": apiLimitDisplay},
	}

	result := map[string]interface{}{
		"rows": rows,
	}

	return result, nil
}

// ShowUserIndex show user index for enterprise edition
func (s *Service) ShowUserIndex(email string) (map[string]interface{}, error) {
	// Query user by email
	var user entity.User
	err := dao.DB.Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, common.ErrUserNotFound
	}

	docEngine := os.Getenv("DOC_ENGINE")
	if docEngine == "" {
		docEngine = "elasticsearch"
	}
	if docEngine != "elasticsearch" {
		return map[string]interface{}{
			"Email":   user.Email,
			"Message": "Elasticsearch is not in use, current doc engine: " + docEngine,
		}, nil
	}

	cfg := server.GetConfig()
	if cfg == nil || cfg.DocEngine.ES == nil {
		return map[string]interface{}{
			"Email":   user.Email,
			"Message": "Elasticsearch configuration not found",
		}, nil
	}

	esEngine, err := elasticsearch.NewEngine(cfg.DocEngine.ES)
	if err != nil {
		return map[string]interface{}{
			"Email":   user.Email,
			"Message": fmt.Sprintf("Failed to connect to Elasticsearch: %s", err.Error()),
		}, nil
	}
	defer esEngine.Close()

	tenantID := user.ID
	indices := []string{
		fmt.Sprintf("ragflow_%s", tenantID),
		fmt.Sprintf("ragflow_doc_meta_%s", tenantID),
	}

	indexStats, err := esEngine.GetIndexStats(indices)
	if err != nil {
		return map[string]interface{}{
			"Email":   user.Email,
			"Message": fmt.Sprintf("Failed to get index stats: %s", err.Error()),
		}, nil
	}

	var kbCount int64
	dao.DB.Model(&entity.Knowledgebase{}).Where("tenant_id = ? AND status = '1'", tenantID).Count(&kbCount)

	var totalDocs int64
	var totalDocSize int64
	kbSubQuery := dao.DB.Model(&entity.Knowledgebase{}).
		Select("id").
		Where("tenant_id = ? AND status = '1'", tenantID)
	var docAgg struct {
		TotalCount int64
		TotalSize  int64
	}
	dao.DB.Model(&entity.Document{}).
		Select("COUNT(*) as total_count, COALESCE(SUM(size), 0) as total_size").
		Where("kb_id IN (?)", kbSubQuery).
		Scan(&docAgg)
	totalDocs = docAgg.TotalCount
	totalDocSize = docAgg.TotalSize

	var kbAgg struct {
		TotalChunkNum int64
		TotalTokenNum int64
	}
	dao.DB.Model(&entity.Knowledgebase{}).
		Select("COALESCE(SUM(chunk_num), 0) as total_chunk_num, COALESCE(SUM(token_num), 0) as total_token_num").
		Where("tenant_id = ? AND status = '1'", tenantID).
		Scan(&kbAgg)

	var totalIndexSize int64
	for _, stat := range indexStats {
		if storeSize, ok := stat["store.size"]; ok {
			if sizeStr, ok := storeSize.(string); ok {
				if sizeBytes := common.ParseBytesString(sizeStr); sizeBytes > 0 {
					totalIndexSize += sizeBytes
				}
			}
		}
	}

	result := map[string]interface{}{
		"Email":         user.Email,
		"Tenant_ID":     tenantID,
		"Dataset_Count": kbCount,
		"Doc_Count":     common.FormatNumber(totalDocs),
		"Doc_Size":      common.FormatBytes(totalDocSize),
		"Chunk_Count":   common.FormatNumber(kbAgg.TotalChunkNum),
		"Token_Count":   common.FormatNumber(kbAgg.TotalTokenNum),
		"Index_Count":   len(indexStats),
		"Index_Size":    common.FormatBytes(totalIndexSize),
		"indices":       indexStats,
	}

	return result, nil
}

// UpdateUserRole update user role
func (s *Service) UpdateUserRole(email, roleName string) (map[string]interface{}, error) {
	// Query user by email
	var user entity.User
	err := dao.DB.Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, common.ErrUserNotFound
	}

	result := map[string]interface{}{
		"command":  "update_user_role",
		"role":     roleName,
		"email":    user.Email,
		"nickname": user.Nickname,
		"error":    "'update user role' is not supported",
	}

	return result, nil
}

// ShowUserPermission show user permissions for enterprise edition
func (s *Service) ShowUserPermission(email string) (map[string]interface{}, error) {
	// Query user by email
	var user entity.User
	err := dao.DB.Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, common.ErrUserNotFound
	}

	result := map[string]interface{}{
		"command":  "show_user_permission",
		"email":    user.Email,
		"nickname": user.Nickname,
		"error":    "'show user permission' is not supported",
	}

	return result, nil
}

// ListUserDatasets show user datasets for enterprise edition
func (s *Service) ListUserDatasets(email string) ([]map[string]interface{}, error) {
	// Query user by email
	var user entity.User
	err := dao.DB.Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, common.ErrUserNotFound
	}

	result := []map[string]interface{}{
		{
			"command":  "list_user_datasets",
			"email":    user.Email,
			"nickname": user.Nickname,
			"error":    "'list user datasets' is not supported",
		},
	}

	return result, nil
}

// ListUserAgents show user agents for enterprise edition
func (s *Service) ListUserAgents(email string) ([]map[string]interface{}, error) {
	// Query user by email
	var user entity.User
	err := dao.DB.Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, common.ErrUserNotFound
	}

	result := []map[string]interface{}{
		{
			"command":  "list_user_agents",
			"email":    user.Email,
			"nickname": user.Nickname,
			"error":    "'list user agents' is not supported",
		},
	}

	return result, nil
}

// ListUserChats show user chats for enterprise edition
func (s *Service) ListUserChats(email string) ([]map[string]interface{}, error) {
	// Query user by email
	var user entity.User
	err := dao.DB.Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, common.ErrUserNotFound
	}

	result := []map[string]interface{}{
		{
			"command":  "list_user_chats",
			"email":    user.Email,
			"nickname": user.Nickname,
			"error":    "'list user chats' is not supported",
		},
	}

	return result, nil
}

// ListUserSearches show user searches for enterprise edition
func (s *Service) ListUserSearches(email string) ([]map[string]interface{}, error) {
	// Query user by email
	var user entity.User
	err := dao.DB.Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, common.ErrUserNotFound
	}

	result := []map[string]interface{}{
		{
			"command":  "list_user_searches",
			"email":    user.Email,
			"nickname": user.Nickname,
			"error":    "'list user searches' is not supported",
		},
	}

	return result, nil
}

// ListUserModels show user models for enterprise edition
func (s *Service) ListUserModels(email string) ([]map[string]interface{}, error) {
	// Query user by email
	var user entity.User
	err := dao.DB.Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, common.ErrUserNotFound
	}

	result := []map[string]interface{}{
		{
			"command":  "list_user_models",
			"email":    user.Email,
			"nickname": user.Nickname,
			"error":    "'list user models' is not supported",
		},
	}

	return result, nil
}

// ListUserFiles show user files for enterprise edition
func (s *Service) ListUserFiles(email string) ([]map[string]interface{}, error) {
	// Query user by email
	var user entity.User
	err := dao.DB.Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, common.ErrUserNotFound
	}

	result := []map[string]interface{}{
		{
			"command":  "list_user_files",
			"email":    user.Email,
			"nickname": user.Nickname,
			"error":    "'list user files' is not supported",
		},
	}

	return result, nil
}

// ListUserProviders show user providers for enterprise edition
func (s *Service) ListUserProviders(email string) ([]map[string]interface{}, error) {
	// Query user by email
	var user entity.User
	err := dao.DB.Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, common.ErrUserNotFound
	}

	result := []map[string]interface{}{
		{
			"command":  "list_user_providers",
			"email":    user.Email,
			"nickname": user.Nickname,
			"error":    "'list user providers' is not supported",
		},
	}

	return result, nil
}

// ListUserProviderInstances show user provider instances for enterprise edition
func (s *Service) ListUserProviderInstances(email, providerName string) ([]map[string]interface{}, error) {
	// Query user by email
	var user entity.User
	err := dao.DB.Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, common.ErrUserNotFound
	}

	result := []map[string]interface{}{
		{
			"command":       "list_user_provider_instances",
			"email":         user.Email,
			"nickname":      user.Nickname,
			"provider_name": providerName,
			"error":         "'list user provider instances' is not supported",
		},
	}

	return result, nil
}

// ListUserProviderInstanceModels show user provider instance models for enterprise edition
func (s *Service) ListUserProviderInstanceModels(email, providerName, instanceName string) ([]map[string]interface{}, error) {
	// Query user by email
	var user entity.User
	err := dao.DB.Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, common.ErrUserNotFound
	}

	result := []map[string]interface{}{
		{
			"command":       "list_user_provider_instance_models",
			"email":         user.Email,
			"nickname":      user.Nickname,
			"provider_name": providerName,
			"instance_name": instanceName,
			"error":         "'list user provider instance models' is not supported",
		},
	}

	return result, nil
}

// ListUserDefaultModels show user default models for enterprise edition
func (s *Service) ListUserDefaultModels(email string) ([]map[string]interface{}, error) {
	// Query user by email
	var user entity.User
	err := dao.DB.Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, common.ErrUserNotFound
	}

	result := []map[string]interface{}{
		{
			"command":  "list_user_default_models",
			"email":    user.Email,
			"nickname": user.Nickname,
			"error":    "'list user default models' is not supported",
		},
	}

	return result, nil
}

// ShowUsersSummary show users summary
func (s *Service) ShowUsersSummary() (map[string]interface{}, error) {
	thirtyDaysAgo := time.Now().AddDate(0, 0, -30)

	var totalUsers int64
	dao.DB.Model(&entity.User{}).Where("is_anonymous = ?", "0").Count(&totalUsers)

	var activeUsers int64
	dao.DB.Model(&entity.User{}).
		Where("is_anonymous = ? AND last_login_time >= ?", "0", thirtyDaysAgo).
		Count(&activeUsers)

	var newUsers int64
	dao.DB.Model(&entity.User{}).
		Where("is_anonymous = ? AND create_time >= ?", "0", thirtyDaysAgo).
		Count(&newUsers)

	result := map[string]interface{}{
		"Total_Users":      common.FormatNumber(totalUsers),
		"Active_Users_30d": common.FormatNumber(activeUsers),
		"New_Users_30d":    common.FormatNumber(newUsers),
	}

	return result, nil
}

// ShowUsersActivity show users activity for enterprise edition
func (s *Service) ShowUsersActivity(days, windows *int) (map[string]interface{}, error) {
	daysInt := 0
	if days != nil {
		daysInt = *days
	}
	windowsInt := 0
	if windows != nil {
		windowsInt = *windows
	}
	result := map[string]interface{}{
		"days":    daysInt,
		"windows": windowsInt,
		"command": "show_users_activity",
		"error":   "'show users activity' is not supported",
	}

	return result, nil
}

func (s *Service) listUsersInactiveByPlan(days int) ([]map[string]interface{}, error) {
	rows, err := dao.NewEnterpriseUserDAO().ListInactiveByPlan(days)
	if err != nil {
		return nil, fmt.Errorf("failed to query inactive users: %w", err)
	}

	var totalInactive int64
	for _, r := range rows {
		totalInactive += r.InactiveCount
	}

	result := make([]map[string]interface{}, 0, len(rows)+1)
	for _, r := range rows {
		result = append(result, map[string]interface{}{
			"plan":           r.PlanName,
			"inactive_users": common.FormatNumber(r.InactiveCount),
		})
	}

	result = append(result, map[string]interface{}{
		"plan":           "Total",
		"inactive_users": common.FormatNumber(totalInactive),
	})

	return result, nil
}

func (s *Service) ListUsersEnterprise(pageIndex, pageSize int, status, orderBy, plan *string, top, days, quota *int) ([]map[string]interface{}, error) {
	if status != nil && strings.EqualFold(*status, "inactive") {
		daysVal := 90
		if days != nil && *days > 0 {
			daysVal = *days
		}
		return s.listUsersInactiveByPlan(daysVal)
	}

	if orderBy != nil && strings.EqualFold(*orderBy, "storage") {
		topVal := 10
		if top != nil && *top > 0 {
			topVal = *top
		}
		return s.listUsersStorageRows(topVal)
	}

	if orderBy != nil && strings.EqualFold(*orderBy, "documents") {
		topVal := 10
		if top != nil && *top > 0 {
			topVal = *top
		}
		return s.listUsersDocumentsRows(topVal)
	}

	if orderBy != nil && strings.EqualFold(*orderBy, "index") {
		topVal := 10
		if top != nil && *top > 0 {
			topVal = *top
		}
		return s.listUsersIndexRows(topVal)
	}

	if plan != nil && quota != nil {
		topVal := 30
		if top != nil && *top > 0 {
			topVal = *top
		}
		daysVal := 30
		if days != nil && *days > 0 {
			daysVal = *days
		}
		return s.listUsersPlanQuotaRows(topVal, *plan, *quota, daysVal)
	}

	if quota != nil {
		topVal := 10
		if top != nil && *top > 0 {
			topVal = *top
		}
		return s.listUsersQuotaRows(topVal, *quota)
	}

	if plan != nil {
		topVal := 30
		if top != nil && *top > 0 {
			topVal = *top
		}
		daysVal := 30
		if days != nil && *days > 0 {
			daysVal = *days
		}
		return s.listUsersPlanRows(topVal, *plan, daysVal)
	}

	item := map[string]interface{}{}
	if status != nil {
		item["status"] = *status
	}
	if orderBy != nil {
		item["order_by"] = *orderBy
	}
	if plan != nil {
		item["plan"] = *plan
	}
	if top != nil {
		item["top"] = *top
	}
	if days != nil {
		item["days"] = *days
	}
	if quota != nil {
		item["quota"] = *quota
	}

	var result []map[string]interface{}
	result = append(result, item)
	return result, nil
}

// ListUsersReports list users reports for enterprise edition
func (s *Service) ListUsersReports(pageIndex, pageSize int, status, plan *string, days *int) (map[string]interface{}, error) {

	statusStr := "all"
	if status != nil {
		statusStr = *status
	}
	planStr := "all"
	daysInt := 0
	if days != nil {
		daysInt = *days
	}
	if plan != nil {
		planStr = *plan
	}

	result := map[string]interface{}{
		"page_index": pageIndex,
		"page_size":  pageSize,
		"status":     statusStr,
		"plan":       planStr,
		"days":       daysInt,
		"command":    "list_users_reports",
		"error":      "'List users reports' is not supported",
	}

	return result, nil
}

// ListUsersStorage list users storage for enterprise edition
func (s *Service) ListUsersStorage(pageIndex, pageSize, top int) (map[string]interface{}, error) {
	if top <= 0 {
		top = 10
	}

	users, err := s.listUsersStorageRows(top)
	if err != nil {
		return nil, err
	}

	result := map[string]interface{}{
		"top":   top,
		"count": len(users),
		"users": users,
	}

	return result, nil
}

func (s *Service) listUsersStorageRows(top int) ([]map[string]interface{}, error) {
	rows, err := dao.NewEnterpriseUserDAO().ListStorageRows(top)
	if err != nil {
		return nil, fmt.Errorf("failed to query users storage: %w", err)
	}

	result := make([]map[string]interface{}, 0, len(rows))
	for i, row := range rows {
		result = append(result, map[string]interface{}{
			"rank":      i + 1,
			"email":     row.Email,
			"nickname":  row.Nickname,
			"doc_count": common.FormatNumber(row.DocCount),
			"doc_size":  common.FormatBytes(row.TotalSize),
		})
	}

	return result, nil
}

func (s *Service) listUsersDocumentsRows(top int) ([]map[string]interface{}, error) {
	rows, err := dao.NewEnterpriseUserDAO().ListDocumentsRows(top)
	if err != nil {
		return nil, fmt.Errorf("failed to query users documents: %w", err)
	}

	result := make([]map[string]interface{}, 0, len(rows))
	for i, row := range rows {
		result = append(result, map[string]interface{}{
			"rank":      i + 1,
			"email":     row.Email,
			"nickname":  row.Nickname,
			"doc_count": common.FormatNumber(row.DocCount),
		})
	}

	return result, nil
}

func (s *Service) listUsersIndexRows(top int) ([]map[string]interface{}, error) {
	docEngine := os.Getenv("DOC_ENGINE")
	if docEngine == "" {
		docEngine = "elasticsearch"
	}
	if docEngine != "elasticsearch" {
		return nil, fmt.Errorf("elasticsearch is not in use, current doc engine: %s", docEngine)
	}

	cfg := server.GetConfig()
	if cfg == nil || cfg.DocEngine.ES == nil {
		return nil, fmt.Errorf("elasticsearch configuration not found")
	}

	esEngine, err := elasticsearch.NewEngine(cfg.DocEngine.ES)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to elasticsearch: %w", err)
	}
	defer esEngine.Close()

	allIndexStats, err := esEngine.GetIndexStats([]string{"ragflow*"})
	if err != nil {
		return nil, fmt.Errorf("failed to get index stats: %w", err)
	}

	type indexRow struct {
		TenantID  string
		IndexName string
		Health    string
		DocsCount string
		StoreSize string
		SizeBytes int64
	}
	var rows []indexRow

	for _, stat := range allIndexStats {
		indexName, _ := stat["index"].(string)
		if indexName == "" {
			continue
		}

		var tenantID string
		if strings.HasPrefix(indexName, "ragflow_doc_meta_") {
			tenantID = strings.TrimPrefix(indexName, "ragflow_doc_meta_")
		} else if strings.HasPrefix(indexName, "ragflow_") {
			tenantID = strings.TrimPrefix(indexName, "ragflow_")
		} else {
			continue
		}

		if tenantID == "" {
			continue
		}

		health, _ := stat["health"].(string)
		docsCount, _ := stat["docs.count"].(string)
		storeSize, _ := stat["store.size"].(string)

		rows = append(rows, indexRow{
			TenantID:  tenantID,
			IndexName: indexName,
			Health:    health,
			DocsCount: docsCount,
			StoreSize: storeSize,
			SizeBytes: common.ParseBytesString(storeSize),
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].SizeBytes > rows[j].SizeBytes
	})

	if top > len(rows) {
		top = len(rows)
	}
	rows = rows[:top]

	tenantIDs := make([]string, 0, len(rows))
	seen := make(map[string]bool)
	for _, r := range rows {
		if !seen[r.TenantID] {
			seen[r.TenantID] = true
			tenantIDs = append(tenantIDs, r.TenantID)
		}
	}

	type userInfo struct {
		ID       string `gorm:"column:id"`
		Email    string `gorm:"column:email"`
		Nickname string `gorm:"column:nickname"`
	}
	var users []userInfo
	if len(tenantIDs) > 0 {
		dao.DB.Raw(`SELECT id, email, nickname FROM user WHERE id IN (?) AND status != '0'`, tenantIDs).Scan(&users)
	}

	userMap := make(map[string]userInfo, len(users))
	for _, u := range users {
		userMap[u.ID] = u
	}

	result := make([]map[string]interface{}, 0, len(rows))
	for i, r := range rows {
		user, ok := userMap[r.TenantID]
		tenant := r.TenantID
		if ok {
			tenant = user.Email
		}

		result = append(result, map[string]interface{}{
			"rank":       i + 1,
			"tenant":     tenant,
			"tenant_id":  r.TenantID,
			"index_name": r.IndexName,
			"health":     r.Health,
			"docs.count": r.DocsCount,
			"store.size": r.StoreSize,
		})
	}

	return result, nil
}

func (s *Service) listUsersQuotaRows(top, quotaThreshold int) ([]map[string]interface{}, error) {
	rows, err := dao.NewEnterpriseUserDAO().ListQuotaRows(top, quotaThreshold)
	if err != nil {
		return nil, fmt.Errorf("failed to query users quota: %w", err)
	}

	result := make([]map[string]interface{}, 0, len(rows))
	for i, r := range rows {
		limitDisplay := "-"
		if r.StorageLimit > 0 {
			limitDisplay = common.FormatBytes(r.StorageLimit)
		}

		result = append(result, map[string]interface{}{
			"rank":  i + 1,
			"user":  r.Email,
			"plan":  r.PlanName,
			"used":  common.FormatBytes(r.StorageUsed),
			"limit": limitDisplay,
		})
	}

	return result, nil
}

// listUsersPlanRows returns active users filtered by plan name within the last N days.
// Active users are defined by last_login_time >= now - days.
// Results are sorted by last_login_time descending, limited to top N.
func (s *Service) listUsersPlanRows(top int, planName string, days int) ([]map[string]interface{}, error) {
	rows, err := dao.NewEnterpriseUserDAO().ListPlanRows(top, planName, days)
	if err != nil {
		return nil, fmt.Errorf("failed to query users by plan: %w", err)
	}

	result := make([]map[string]interface{}, 0, len(rows))
	for i, r := range rows {
		lastLogin := "-"
		if r.LastLoginTime != nil {
			lastLogin = r.LastLoginTime.Format("2006-01-02 15:04:05")
		}

		result = append(result, map[string]interface{}{
			"rank":       i + 1,
			"user":       r.Email,
			"plan":       r.PlanName,
			"last_login": lastLogin,
		})
	}

	return result, nil
}

// listUsersPlanQuotaRows returns active users filtered by plan name whose storage usage
// percentage meets or exceeds the quota threshold within the last N days.
// Active users are defined by last_login_time >= now - days.
// Results are sorted by storage_used descending, limited to top N.
func (s *Service) listUsersPlanQuotaRows(top int, planName string, quotaThreshold, days int) ([]map[string]interface{}, error) {
	rows, err := dao.NewEnterpriseUserDAO().ListPlanQuotaRows(top, planName, quotaThreshold, days)
	if err != nil {
		return nil, fmt.Errorf("failed to query users plan quota: %w", err)
	}

	result := make([]map[string]interface{}, 0, len(rows))
	for i, r := range rows {
		limitDisplay := "-"
		if r.StorageLimit > 0 {
			limitDisplay = common.FormatBytes(r.StorageLimit)
		}
		lastLogin := "-"
		if r.LastLoginTime != nil {
			lastLogin = r.LastLoginTime.Format("2006-01-02 15:04:05")
		}

		result = append(result, map[string]interface{}{
			"rank":       i + 1,
			"user":       r.Email,
			"plan":       r.PlanName,
			"used":       common.FormatBytes(r.StorageUsed),
			"limit":      limitDisplay,
			"last_login": lastLogin,
		})
	}

	return result, nil
}

// ListUsersDocuments list users documents for enterprise edition
func (s *Service) ListUsersDocuments(pageIndex, pageSize, top int) (map[string]interface{}, error) {

	result := map[string]interface{}{
		"page_index": pageIndex,
		"page_size":  pageSize,
		"top":        top,
		"command":    "list_users_documents",
		"error":      "'List users documents' is not supported",
	}

	return result, nil
}

// ListUsersIndex list users index for enterprise edition
func (s *Service) ListUsersIndex(pageIndex, pageSize, top int) (map[string]interface{}, error) {

	result := map[string]interface{}{
		"page_index": pageIndex,
		"page_size":  pageSize,
		"top":        top,
		"command":    "list_users_index",
		"error":      "'List users index' is not supported",
	}

	return result, nil
}

// ListUsersQuota list users quota for enterprise edition
func (s *Service) ListUsersQuota(pageIndex, pageSize, top int, quotaThreshold *int, plan *string, days *int) (map[string]interface{}, error) {

	quotaThresholdInt := 0
	if quotaThreshold != nil {
		quotaThresholdInt = *quotaThreshold
	}
	planStr := "all"
	daysInt := 0
	if days != nil {
		daysInt = *days
	}
	if plan != nil {
		planStr = *plan
	}

	result := map[string]interface{}{
		"page_index":      pageIndex,
		"page_size":       pageSize,
		"top":             top,
		"quota_threshold": quotaThresholdInt,
		"plan":            planStr,
		"days":            daysInt,
		"command":         "list_users_quota",
		"error":           "'List users quota' is not supported",
	}

	return result, nil
}

// ShowUsersPlanSummary show users plan summary for enterprise edition
func (s *Service) ShowUsersPlanSummary() (map[string]interface{}, error) {

	result := map[string]interface{}{
		"command": "show_users_plan_summary",
		"error":   "'Show users plan summary' is not supported",
	}

	return result, nil
}

// ShowUsersQuotaSummary show users quota summary for enterprise edition
func (s *Service) ShowUsersQuotaSummary() (map[string]interface{}, error) {
	products, err := dao.NewEnterpriseUserDAO().ListBillingProducts()
	if err != nil {
		return nil, fmt.Errorf("failed to query billing products: %w", err)
	}

	planLimits := make(map[string]dao.ProductRow)
	seen := make(map[string]bool)
	for _, p := range products {
		if !seen[p.Name] {
			seen[p.Name] = true
			planLimits[p.Name] = p
		}
	}

	usageRows, err := dao.NewEnterpriseUserDAO().ListQuotaUsage()
	if err != nil {
		return nil, fmt.Errorf("failed to query quota usage: %w", err)
	}

	storageRows := make([]map[string]interface{}, 0, len(usageRows))
	for _, r := range usageRows {
		limit, ok := planLimits[r.PlanName]
		var storageLimit int64
		if ok {
			storageLimit = limit.QuotaStorage
		}
		usagePct := "N/A"
		if storageLimit > 0 {
			usagePct = fmt.Sprintf("%.1f%%", r.AvgStorageUsed*100.0/float64(storageLimit))
		}
		storageRows = append(storageRows, map[string]interface{}{
			"Plan":      r.PlanName,
			"Users":     common.FormatNumber(r.UserCount),
			"Avg_Used":  common.FormatBytes(int64(r.AvgStorageUsed)),
			"Limit":     common.FormatBytes(storageLimit),
			"Avg_Usage": usagePct,
		})
	}

	appsRows := make([]map[string]interface{}, 0, len(usageRows))
	for _, r := range usageRows {
		limit, ok := planLimits[r.PlanName]
		var appsLimit int
		if ok {
			appsLimit = limit.QuotaApps
		}
		usagePct := "N/A"
		if appsLimit > 0 {
			usagePct = fmt.Sprintf("%.1f%%", r.AvgAppsUsed*100.0/float64(appsLimit))
		}
		appsRows = append(appsRows, map[string]interface{}{
			"Plan":      r.PlanName,
			"Avg_Used":  fmt.Sprintf("%.1f", r.AvgAppsUsed),
			"Limit":     fmt.Sprintf("%d", appsLimit),
			"Avg_Usage": usagePct,
		})
	}

	apiRows := make([]map[string]interface{}, 0, len(usageRows))
	for _, r := range usageRows {
		limit, ok := planLimits[r.PlanName]
		var apiLimit int64
		if ok && limit.APIRequestLimitPerMinute != nil {
			apiLimit = *limit.APIRequestLimitPerMinute
		}
		limitDisplay := "N/A"
		if apiLimit > 0 {
			limitDisplay = common.FormatNumber(apiLimit)
		}
		apiRows = append(apiRows, map[string]interface{}{
			"Plan":          r.PlanName,
			"Tokens":        common.FormatNumber(r.TotalAPITokens),
			"Limit_per_min": limitDisplay,
		})
	}

	result := map[string]interface{}{
		"storage": storageRows,
		"apps":    appsRows,
		"api":     apiRows,
	}
	return result, nil
}

// ShowIngestionTasksSummary show ingestion tasks summary
func (s *Service) ShowIngestionTasksSummary() (map[string]interface{}, error) {
	agg, err := dao.NewEnterpriseTaskDAO().GetTaskAggSummary()
	if err != nil {
		return nil, fmt.Errorf("failed to query task summary: %w", err)
	}

	avgDurStr := fmt.Sprintf("%.1fs", agg.AvgDuration)

	result := map[string]interface{}{
		"Total_Tasks":             common.FormatNumber(agg.Total),
		"Pending_progress_0":      common.FormatNumber(agg.Pending),
		"Running_0_lt_progress_1": common.FormatNumber(agg.Running),
		"Completed_progress_1":    common.FormatNumber(agg.Completed),
		"Failed_progress_-1":      common.FormatNumber(agg.Failed),
		"Avg_Duration_completed":  avgDurStr,
		"Retried_Tasks":           common.FormatNumber(agg.Retried),
		"Abandoned_retry_ge_3":    common.FormatNumber(agg.Abandoned),
	}

	typeRows, err := dao.NewEnterpriseTaskDAO().GetTaskTypeCounts()
	if err != nil {
		return nil, fmt.Errorf("failed to query task type counts: %w", err)
	}

	for _, r := range typeRows {
		label := r.TaskType
		if label == "" {
			label = "Parse"
		}
		result[label] = common.FormatNumber(r.Count)
	}

	return result, nil
}

// ShowDataSummary show data summary for enterprise edition
func (s *Service) ShowDataSummary() (map[string]interface{}, error) {
	storageRow, err := dao.NewEnterpriseDataDAO().GetStorageSummary()
	if err != nil {
		return nil, fmt.Errorf("failed to query storage summary: %w", err)
	}

	taskRow, err := dao.NewEnterpriseDataDAO().GetTaskSummary()
	if err != nil {
		return nil, fmt.Errorf("failed to query task summary: %w", err)
	}

	result := map[string]interface{}{
		"Total_File_Size":     common.FormatBytes(storageRow.TotalFileSize),
		"Total_Document_Size": common.FormatBytes(storageRow.TotalDocSize),
		"Total_Storage":       common.FormatBytes(storageRow.TotalFileSize + storageRow.TotalDocSize),

		"ES_Store_Size":           "N/A",
		"ES_Vector_Dataset_Size":  "N/A",
		"Total_Tasks":             common.FormatNumber(taskRow.Total),
		"Pending_progress_0":      common.FormatNumber(taskRow.Pending),
		"Running_0_lt_progress_1": common.FormatNumber(taskRow.Running),
		"Completed_progress_1":    common.FormatNumber(taskRow.Completed),
		"Failed_progress_-1":      common.FormatNumber(taskRow.Failed),
	}

	docEngine := os.Getenv("DOC_ENGINE")
	if docEngine == "" {
		docEngine = "elasticsearch"
	}
	if docEngine == "elasticsearch" {
		cfg := server.GetConfig()
		if cfg != nil && cfg.DocEngine.ES != nil {
			esEngine, err := elasticsearch.NewEngine(cfg.DocEngine.ES)
			if err == nil {
				defer esEngine.Close()
				clusterStats, err := esEngine.GetClusterStats()
				if err == nil {
					if v, ok := clusterStats["store_size"]; ok {
						result["ES_Store_Size"] = v
					}
					if v, ok := clusterStats["total_dataset_size"]; ok {
						result["ES_Vector_Dataset_Size"] = v
					}
				}
			}
		}
	}

	return result, nil
}

// ShowDataOrphan show data orphan for enterprise edition
func (s *Service) ShowDataOrphan() (map[string]interface{}, error) {
	kbRow, err := dao.NewEnterpriseDataDAO().GetKBOrphans()
	if err != nil {
		return nil, fmt.Errorf("failed to query knowledgebase orphans: %w", err)
	}

	docRow, err := dao.NewEnterpriseDataDAO().GetDocOrphans()
	if err != nil {
		return nil, fmt.Errorf("failed to query document orphans: %w", err)
	}

	taskNoDoc, err := dao.NewEnterpriseDataDAO().GetTaskOrphans()
	if err != nil {
		return nil, fmt.Errorf("failed to query task orphans: %w", err)
	}

	fileRow, err := dao.NewEnterpriseDataDAO().GetFileOrphans()
	if err != nil {
		return nil, fmt.Errorf("failed to query file orphans: %w", err)
	}

	utRow, err := dao.NewEnterpriseDataDAO().GetUserTenantOrphans()
	if err != nil {
		return nil, fmt.Errorf("failed to query user_tenant orphans: %w", err)
	}

	esOrphanCount := int64(0)
	esErrorMsg := ""
	docEngine := os.Getenv("DOC_ENGINE")
	if docEngine == "" {
		docEngine = "elasticsearch"
	}
	if docEngine != "elasticsearch" {
		esErrorMsg = "ES not in use (" + docEngine + ")"
	} else {
		cfg := server.GetConfig()
		if cfg == nil || cfg.DocEngine.ES == nil {
			esErrorMsg = "ES config not found"
		} else {
			esEngine, err := elasticsearch.NewEngine(cfg.DocEngine.ES)
			if err != nil {
				esErrorMsg = fmt.Sprintf("ES connect failed: %s", err.Error())
			} else {
				defer esEngine.Close()
				allIndexStats, err := esEngine.GetIndexStats([]string{"ragflow*"})
				if err != nil {
					esErrorMsg = fmt.Sprintf("ES query failed: %s", err.Error())
				} else {
					var validTenantIDs []string
					dao.DB.Table("tenant").Select("id").Scan(&validTenantIDs)
					validSet := make(map[string]bool, len(validTenantIDs))
					for _, id := range validTenantIDs {
						validSet[id] = true
					}
					for _, stat := range allIndexStats {
						indexName, _ := stat["index"].(string)
						if indexName == "" {
							continue
						}
						var tenantID string
						if strings.HasPrefix(indexName, "ragflow_doc_meta_") {
							tenantID = strings.TrimPrefix(indexName, "ragflow_doc_meta_")
						} else if strings.HasPrefix(indexName, "ragflow_") && !strings.Contains(strings.TrimPrefix(indexName, "ragflow_"), "_") {
							tenantID = strings.TrimPrefix(indexName, "ragflow_")
						} else {
							continue
						}
						if tenantID != "" && !validSet[tenantID] {
							esOrphanCount++
						}
					}
				}
			}
		}
	}

	result := map[string]interface{}{

		"KB_No_Tenant":  common.FormatNumber(kbRow.KbNoTenant),
		"KB_No_Creator": common.FormatNumber(kbRow.KbNoUser),

		"Doc_No_Knowledgebase":               common.FormatNumber(docRow.DocNoKb),
		"Doc_No_Creator":                     common.FormatNumber(docRow.DocNoUser),
		"Doc_KB_exists_but_KB_has_no_Tenant": common.FormatNumber(docRow.DocKbNoTenant),

		"Task_No_Document": common.FormatNumber(taskNoDoc),

		"File_No_Tenant":  common.FormatNumber(fileRow.FileNoTenant),
		"File_No_Creator": common.FormatNumber(fileRow.FileNoUser),

		"UserTenant_No_User":   common.FormatNumber(utRow.UTNoUser),
		"UserTenant_No_Tenant": common.FormatNumber(utRow.UTNoTenantRecord),

		"ES_Index_No_Tenant": common.FormatNumber(esOrphanCount),
	}

	if esErrorMsg != "" {
		result["ES Error"] = esErrorMsg
	}

	return result, nil
}

// ShowDataStorage show data storage for enterprise edition
func (s *Service) ShowDataStorage() (map[string]interface{}, error) {

	var fileAgg struct {
		TotalFiles    int64
		TotalFileSize int64
	}
	dao.DB.Model(&entity.File{}).Select(
		"COUNT(*) AS total_files, COALESCE(SUM(size), 0) AS total_file_size",
	).Scan(&fileAgg)

	var docAgg struct {
		TotalDocs    int64
		TotalDocSize int64
		TotalChunks  int64
		TotalTokens  int64
	}
	dao.DB.Model(&entity.Document{}).Select(
		"COUNT(*) AS total_docs, COALESCE(SUM(size), 0) AS total_doc_size, COALESCE(SUM(chunk_num), 0) AS total_chunks, COALESCE(SUM(token_num), 0) AS total_tokens",
	).Scan(&docAgg)

	var totalKB int64
	dao.DB.Model(&entity.Knowledgebase{}).Where("status = ?", string(entity.StatusValid)).Count(&totalKB)

	var totalTenants int64
	dao.DB.Model(&entity.Tenant{}).Count(&totalTenants)

	var totalChat int64
	dao.DB.Model(&entity.Chat{}).Where("status = ?", "1").Count(&totalChat)

	var totalAgent int64
	dao.DB.Model(&entity.UserCanvas{}).Where("canvas_category = ?", "agent_canvas").Count(&totalAgent)

	var totalAPIToken int64
	dao.DB.Model(&entity.APIToken{}).Count(&totalAPIToken)

	var activeSubs int64
	dao.DB.Model(&entity.BillingSubscription{}).Where("subscription_status = ?", "active").Count(&activeSubs)

	totalApps := totalChat + totalAgent

	result := map[string]interface{}{
		"Total_Files":                  common.FormatNumber(fileAgg.TotalFiles),
		"Total_File_Size":              common.FormatBytes(fileAgg.TotalFileSize),
		"Total_Documents":              common.FormatNumber(docAgg.TotalDocs),
		"Total_Document_Size":          common.FormatBytes(docAgg.TotalDocSize),
		"Total_Knowledgebases":         common.FormatNumber(totalKB),
		"Total_Chunks":                 common.FormatNumber(docAgg.TotalChunks),
		"Total_Tokens_LLM_used":        common.FormatNumber(docAgg.TotalTokens),
		"Total_Tenants":                common.FormatNumber(totalTenants),
		"Total_Apps_Chat_Search_Agent": common.FormatNumber(totalApps),
		"Total_API_Tokens":             common.FormatNumber(totalAPIToken),
		"Active_Subscriptions":         common.FormatNumber(activeSubs),
	}

	var fileTypeRows []struct {
		Type  string `gorm:"column:type"`
		Count int64  `gorm:"column:count"`
		Size  int64  `gorm:"column:size"`
	}
	dao.DB.Model(&entity.File{}).
		Select("type, COUNT(*) AS count, COALESCE(SUM(size), 0) AS size").
		Group("type").
		Order("count DESC").
		Find(&fileTypeRows)

	if len(fileTypeRows) > 0 {
		for _, r := range fileTypeRows {
			label := r.Type
			if label == "" {
				label = "(unknown)"
			}
			result[label] = fmt.Sprintf("%s (%s)", common.FormatNumber(r.Count), common.FormatBytes(r.Size))
		}
	}

	return result, nil
}

// ShowDataIndex show data index for enterprise edition
func (s *Service) ShowDataIndex() (map[string]interface{}, error) {
	docEngine := os.Getenv("DOC_ENGINE")
	if docEngine == "" {
		docEngine = "elasticsearch"
	}
	if docEngine != "elasticsearch" {
		return nil, fmt.Errorf("elasticsearch is not in use, current doc engine: %s", docEngine)
	}

	cfg := server.GetConfig()
	if cfg == nil || cfg.DocEngine.ES == nil {
		return nil, fmt.Errorf("elasticsearch configuration not found")
	}

	esEngine, err := elasticsearch.NewEngine(cfg.DocEngine.ES)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to elasticsearch: %w", err)
	}
	defer esEngine.Close()

	clusterStats, err := esEngine.GetClusterStats()
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster stats: %w", err)
	}

	result := map[string]interface{}{
		"Cluster_Name":        clusterStats["cluster_name"],
		"Cluster_Health":      clusterStats["status"],
		"Total_Indices":       clusterStats["indices"],
		"Total_Shards":        clusterStats["indices_shards"],
		"Total_Documents":     clusterStats["docs"],
		"Deleted_Documents":   clusterStats["docs_deleted"],
		"Store_Size":          clusterStats["store_size"],
		"Vector_Dataset_Size": clusterStats["total_dataset_size"],
		"Total_Nodes":         clusterStats["nodes"],
		"ES_Version":          clusterStats["nodes_version"],
		"OS_Memory":           clusterStats["os_mem"],
		"OS_Memory_Used":      clusterStats["os_mem_used"],
		"JVM_Heap_Used":       clusterStats["jvm_heap_used"],
		"JVM_Heap_Max":        clusterStats["jvm_heap_max"],
	}

	if osMemUsed, ok := clusterStats["os_mem_used"]; ok {
		if osMemPct, ok2 := clusterStats["os_mem_used_percent"]; ok2 {
			result["OS_Memory_Used"] = fmt.Sprintf("%s (%.0f%%)", osMemUsed, osMemPct)
		}
	}

	if nodesVersion, ok := clusterStats["nodes_version"].([]interface{}); ok && len(nodesVersion) > 0 {
		if v, ok := nodesVersion[0].(string); ok {
			result["ES_Version"] = v
		}
	}

	return result, nil
}

// PurgeOrphanData purge orphan data for enterprise edition
func (s *Service) PurgeOrphanData(preview bool) (map[string]interface{}, error) {

	result := map[string]interface{}{
		"command": "purge_orphan_data",
		"preview": preview,
		"error":   "'Purge orphan data' is not supported",
	}

	return result, nil
}

// PurgeUserData purge user data for enterprise edition
func (s *Service) PurgeUserData(email string, preview bool) (map[string]interface{}, error) {
	// Query user by email
	var user entity.User
	err := dao.DB.Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, common.ErrUserNotFound
	}

	result := map[string]interface{}{
		"email":    user.Email,
		"nickname": user.Nickname,
		"preview":  preview,
		"error":    "'Purge user data' is not supported",
	}

	return result, nil
}

// PurgeUsersData purge users data for enterprise edition
func (s *Service) PurgeUsersData(preview bool, days int, userPlan *string, userActivity *string) (map[string]interface{}, error) {

	plan := "all"
	activity := "all"
	if userPlan != nil {
		plan = *userPlan
	}
	if userActivity != nil {
		activity = *userActivity
	}

	result := map[string]interface{}{
		"command":  "purge_users_data",
		"preview":  preview,
		"days":     days,
		"plan":     plan,
		"activity": activity,
		"error":    "'Purge users data' is not supported",
	}

	return result, nil
}

// GenerateUserAPIKey create tenant API key for tenant
func (s *Service) GenerateUserAPIKey(username string) (map[string]interface{}, error) {

	user, err := s.userDAO.GetByEmail(username)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	result := map[string]interface{}{
		"command":  "create_user_api_key",
		"email":    user.Email,
		"nickname": user.Nickname,
		"error":    "'Create user API key' is not supported",
	}

	return result, nil
}

// DeleteUserAPIKey delete user API key
func (s *Service) DeleteUserAPIKey(username, key string) (map[string]interface{}, error) {

	user, err := s.userDAO.GetByEmail(username)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	result := map[string]interface{}{
		"command":  "delete_user_api_key",
		"email":    user.Email,
		"nickname": user.Nickname,
		"api_key":  key,
		"error":    "'Delete user API key' is not supported",
	}

	return result, nil
}

// ListUserAPIKeys list user API keys
func (s *Service) ListUserAPIKeys(username string) ([]map[string]interface{}, error) {

	user, err := s.userDAO.GetByEmail(username)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	result := []map[string]interface{}{
		{
			"command":  "list_user_api_keys",
			"email":    user.Email,
			"nickname": user.Nickname,
			"error":    "'List user API keys' is not supported",
		},
	}

	return result, nil
}

func progressToStatus(progress float64) string {
	switch {
	case progress == 0:
		return "CREATED"
	case progress > 0 && progress < 1:
		return "RUNNING"
	case progress == 1:
		return "COMPLETED"
	case progress < 0:
		return "FAILED"
	default:
		return "UNKNOWN"
	}
}

func statusToProgressCondition(status string) string {
	switch strings.ToUpper(status) {
	case "CREATED":
		return "t.progress = 0"
	case "RUNNING":
		return "t.progress > 0 AND t.progress < 1"
	case "COMPLETED":
		return "t.progress = 1"
	case "FAILED":
		return "t.progress < 0"
	default:
		return ""
	}
}

func (s *Service) ListIngestionTasksByCondition(email, status *string) ([]map[string]interface{}, error) {
	if email == nil {
		return nil, fmt.Errorf("email is required")
	}

	user, err := s.userDAO.GetByEmail(*email)
	if err != nil {
		return nil, common.ErrUserNotFound
	}

	tenantIDs, err := s.userTenantDAO.GetTenantIDsByUserID(user.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tenant IDs: %w", err)
	}
	if len(tenantIDs) == 0 {
		return []map[string]interface{}{}, nil
	}

	condition := ""
	if status != nil {
		condition = statusToProgressCondition(*status)
		if condition == "" {
			return nil, fmt.Errorf("unsupported status '%s', supported: CREATED, RUNNING, COMPLETED, FAILED", *status)
		}
	}

	rows, err := dao.NewEnterpriseTaskDAO().ListTasksByTenantIDs(tenantIDs, condition)
	if err != nil {
		return nil, fmt.Errorf("failed to query tasks: %w", err)
	}

	showTasks := make([]map[string]interface{}, 0, len(rows))
	for _, r := range rows {
		docName := "N/A"
		if r.DocName != nil {
			docName = *r.DocName
		}

		beginAt := "N/A"
		if r.BeginAt != nil {
			beginAt = r.BeginAt.Format("2006-01-02 15:04:05")
		}

		createdTime := common.FormatTime(r.CreateTime)

		showTasks = append(showTasks, map[string]interface{}{
			"id":         r.ID,
			"document":   docName,
			"status":     progressToStatus(r.Progress),
			"progress":   fmt.Sprintf("%.2f", r.Progress),
			"duration":   fmt.Sprintf("%.1fs", r.ProcessDuration),
			"begin_at":   beginAt,
			"created_at": createdTime,
		})
	}

	return showTasks, nil
}

func (s *Service) StopIngestionTasksByCondition(tasks []string, email, status *string) ([]map[string]interface{}, error) {

	if email == nil && status == nil {
		return nil, fmt.Errorf("email or status are required")
	}

	element := map[string]interface{}{
		"command": "stop_ingestion_tasks_by_condition",
		"tasks":   tasks,
		"error":   "'Stop ingestion tasks by condition' is not supported",
	}

	if email != nil {
		element["email"] = *email
	}
	if status != nil {
		element["status"] = *status
	}

	return []map[string]interface{}{element}, nil
}

func (s *Service) RemoveIngestionTasksByCondition(tasks []string, email, status *string) ([]map[string]interface{}, error) {

	if email == nil && status == nil {
		return nil, fmt.Errorf("email or status are required")
	}

	element := map[string]interface{}{
		"command": "remove_ingestion_tasks_by_condition",
		"tasks":   tasks,
		"error":   "'Remove ingestion tasks by condition' is not supported",
	}

	if email != nil {
		element["email"] = *email
	}
	if status != nil {
		element["status"] = *status
	}

	return []map[string]interface{}{element}, nil
}
