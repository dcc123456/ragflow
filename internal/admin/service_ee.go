//
//  Copyright (c) 2026 InfiniFlow. All rights reserved.
//
//  RAGFlow Enterprise Edition — Proprietary and Confidential.
//
//  This file is part of the RAGFlow Enterprise Edition and is licensed,
//  not sold, under the RAGFlow Enterprise Commercial License Agreement.
//  Unauthorized copying, modification, distribution, or disclosure of this
//  file, via any medium, is strictly prohibited.
//
//  Portions of this file may incorporate code from RAGFlow Community Edition,
//  which is licensed under the Apache License, Version 2.0.
//
//  For commercial licensing inquiries: sales@infiniflow.ai
//

//go:build ee

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/engine/clickhouse"
	"ragflow/internal/engine/elasticsearch"
	"ragflow/internal/entity"
	"ragflow/internal/server"
	servicepkg "ragflow/internal/service"
	"ragflow/internal/utility"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Global scheduled task for cleanup
var (
	globalUpdateRecordTask     *utility.ScheduledTask
	globalUpdateRecordTaskOnce sync.Once
	TimeRecordSaveInterval     int64 = 24 * 60 * 60 // 24 hours by default
	TimeRecordTaskDuration     int64 = 60 * 60      // 1 hour by default
)

// StartUpdateRecordTask starts the global update record scheduled task
func StartUpdateRecordTask() {
	globalUpdateRecordTaskOnce.Do(func() {
		//duration := 24 * time.Hour
		taskDuration := time.Duration(TimeRecordTaskDuration) * time.Second
		globalUpdateRecordTask = utility.NewScheduledTask("UpdateTimeRecord", taskDuration, func() {
			// interval := 24* time.Hour
			interval := time.Duration(TimeRecordSaveInterval) * time.Second
			if err := StartTimeRecordService(interval); err != nil {
				common.Warn(err.Error())
				//logger.Warn("Failed to update time record", zap.Error(err))
			}
		})
		globalUpdateRecordTask.Start()
		common.Info("Time record task is scheduled task started")
	})
}

// StopUpdateRecordTask stops the global update record scheduled task
func StopUpdateRecordTask() {
	if globalUpdateRecordTask != nil {
		globalUpdateRecordTask.Stop()
		globalUpdateRecordTask = nil
	}

	globalUpdateRecordTaskOnce = sync.Once{}
}

// UpdateServer update server status
func CheckServicePeriodically() {
	globalConfig := server.GetConfig()

	// By default, if no heartbeat is received for 10 times, then the server is considered as stale
	GlobalServerStore.CleanupStaleStatuses(globalConfig.GetHeartbeatInterval() * 10)
	GlobalServerStatusStore.CleanupStaleStatuses(globalConfig.GetHeartbeatInterval() * 10)
}

func UpdateServer(serverName string, status *common.BaseMessage) (common.ErrorCode, map[string]interface{}, string) {
	response, err := GlobalServerStatusStore.UpdateStatus(serverName, status)
	if err != nil {
		return 0, nil, err.Error()
	}

	licenseCode := GlobalServerStatusStore.GetLicenseCode()
	if licenseCode == common.CodeLicenseValid {
		currentLicense := GlobalServerStatusStore.GetLicense()
		errorCode := CheckLicenseValidity(currentLicense, licenseCode)
		if errorCode != common.CodeLicenseValid {
			GlobalServerStatusStore.SetLicenseStatusCode(errorCode)
			common.Warn(errorCode.Message())
			StopUpdateRecordTask()
			return errorCode, nil, "Invalid license"
		}

		common.Debug("License is loaded and valid")
		return common.CodeLicenseValid, response, ""
	}
	return licenseCode, nil, ""
}

func (s *Service) GetSystemFingerprint(ctx context.Context) (map[string]interface{}, error) {
	// Get fingerprint and generate digest
	collector := utility.NewFingerprintCollector(true)
	fingerprint, err := collector.CollectV2()
	if err != nil {
		common.Fatal(fmt.Sprintf("Failed to collect hardware info: %v", err))
		return nil, err
	}

	encryptedFingerprint, err := utility.EncryptFingerprint(fingerprint, utility.FingerprintPublicKey)

	data, err := json.Marshal(encryptedFingerprint)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"fingerprint": string(data),
	}, nil
}

func (s *Service) SetSystemLicense(ctx context.Context, licenseStr string) (map[string]interface{}, error) {
	// Verify license
	license, result, err := extractLicenseV2(licenseStr)
	if err != nil {
		common.Error("Set license: error parsing license, ", err)
		return nil, err
	}

	err = ValidateLicenseDigest(license.Digest)
	if err != nil {
		common.Error("Fail to validate license digest: ", err)
		return nil, err
	}

	// Store license to database
	err = s.licenseDAO.Create(ctx, dao.DB, license.LicenseID, licenseStr)
	if err != nil {
		common.Warn("Error storing license: ", zap.Error(err))
		return nil, err
	}

	err = s.timeRecordDAO.DeleteAll(ctx, dao.DB)
	if err != nil {
		common.Warn("Error deleting time records: ", zap.Error(err))
		return nil, err
	}

	GlobalServerStatusStore.SetLicense(license)
	err = InitFirstTimeRecord(ctx)
	if err != nil {
		common.Fatal("Error initializing first time record: %v", zap.Error(err))
	}
	common.Info("License is valid")
	StartUpdateRecordTask()
	return result, nil
}

func (s *Service) ShowSystemLicense(ctx context.Context) (map[string]interface{}, error) {
	licenseInMem := GlobalServerStatusStore.GetLicense()
	if licenseInMem == nil {
		return nil, fmt.Errorf("license is not ready")
	}
	// read license from database
	license, err := s.licenseDAO.GetLatest(ctx, dao.DB)
	if err != nil {
		common.Warn("Error reading license: ", zap.Error(err))
		return nil, err
	}

	_, result, err := extractLicenseV2(license.License)
	if err != nil {
		common.Error("Show license: error parsing license, ", err)
		return nil, err
	}

	return result, nil
}

func (s *Service) VerifyLicense(ctx context.Context, licenseStr string) (map[string]interface{}, error) {
	// Verify license
	license, result, err := extractLicenseV2(licenseStr)
	if err != nil {
		common.Error("Verify license: error parsing license, ", err)
		return nil, err
	}

	err = ValidateLicenseDigest(license.Digest)
	if err != nil {
		common.Error("Fail to validate license digest: ", err)
		return nil, err
	}

	return result, nil
}

func (s *Service) CheckSystemLicense(ctx context.Context) (map[string]interface{}, error) {
	currentLicense := GlobalServerStatusStore.GetLicense()
	var licenseCheckResult string
	if currentLicense == nil {
		licenseCode := GlobalServerStatusStore.GetLicenseCode()
		if licenseCode != common.CodeLicenseValid {
			return map[string]interface{}{
				"result": licenseCode.Message(),
			}, nil
		}

		licenseCheckResult = "license is not ready"
		return map[string]interface{}{
			"result": licenseCheckResult,
		}, nil
	}

	lastRecord, err := GetLastTimeRecord(ctx)
	if err != nil {
		common.Warn(fmt.Sprintf("Fail to get last time record: %v", err))
		return nil, err
	}

	if lastRecord.Nonce != currentLicense.Digest {
		licenseCheckResult = "time record digest is mismatched"
		return map[string]interface{}{
			"result": licenseCheckResult,
		}, nil
	}

	nowTS := time.Now().Unix()
	now := time.Now()
	if currentLicense.ValidFrom.After(now) {
		licenseCheckResult = "license not valid yet"
		return map[string]interface{}{
			"result": licenseCheckResult,
		}, nil
	}

	if currentLicense.ValidUntil.Before(now) {
		licenseCheckResult = "license expired"
		return map[string]interface{}{
			"result": licenseCheckResult,
		}, nil
	}
	// 2. check time rollback
	lastTimestamp := lastRecord.Timestamp
	if nowTS < lastTimestamp {
		// convert following timestamp to time.Time
		licenseCheckResult = "time rollback detected"
		return map[string]interface{}{
			"result": licenseCheckResult,
		}, nil
	}

	licenseCheckResult = "license valid"

	return map[string]interface{}{
		"result": licenseCheckResult,
	}, nil
}

func (s *Service) UpdateSystemLicenseConfig(ctx context.Context, timeRecordSaveInterval, timeRecordTaskDuration int64) error {
	if !(timeRecordSaveInterval >= 3 && timeRecordSaveInterval <= 86400) {
		return errors.New("Value1 value range:  [3, 86400]")
	}
	if !(timeRecordTaskDuration > 0 && timeRecordTaskDuration <= 3600) {
		return errors.New("value2 value range: (0, 3600]")
	}
	TimeRecordSaveInterval = timeRecordSaveInterval
	TimeRecordTaskDuration = timeRecordTaskDuration
	StopUpdateRecordTask()
	StartUpdateRecordTask()
	return nil
}

func (s *Service) SetSoftFingerprint(ctx context.Context, softFingerprint string) error {
	return errors.New("set soft fingerprint is not supported")
}

func (s *Service) GetSoftFingerprint(ctx context.Context) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"command": "show_soft_fingerprint",
		"error":   "'show soft fingerprint' is not supported",
	}

	return result, nil
}

func (s *Service) DeleteSoftFingerprint(ctx context.Context) error {
	return errors.New("delete soft fingerprint is not supported")
}

// ShowUserActivity show user activity for enterprise edition
func (s *Service) ShowUserActivity(ctx context.Context, email string, days int) (map[string]interface{}, error) {
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
func (s *Service) ShowUserDatasetSummary(ctx context.Context, email, dataset string) (map[string]interface{}, error) {
	// Query user by email
	var user entity.User
	err := dao.DB.Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, common.ErrUserNotFound
	}

	tenantIDs, err := s.userTenantDAO.GetTenantIDsByUserID(ctx, dao.DB, user.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tenant IDs: %w", err)
	}

	var kb *entity.Knowledgebase
	for _, tenantID := range tenantIDs {
		found, ferr := s.kbDAO.GetByName(ctx, dao.DB, dataset, tenantID)
		if ferr != nil {
			if dao.IsNotFoundErr(ferr) {
				continue
			}
			return nil, fmt.Errorf("failed to query dataset: %w", ferr)
		}
		kb = found
		break
	}
	if kb == nil {
		return nil, fmt.Errorf("dataset '%s' not found for user '%s'", dataset, email)
	}

	totalDocSize, err := s.documentDAO.SumDocSizeByKBID(ctx, dao.DB, kb.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to query dataset doc size: %w", err)
	}

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

// ShowUserSummary show user summary for  edition
func (s *Service) ShowUserSummary(ctx context.Context, email string) (map[string]interface{}, error) {
	// Query user by email
	var user entity.User
	err := dao.DB.WithContext(ctx).Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, common.ErrUserNotFound
	}

	tenantIDs, err := s.userTenantDAO.GetTenantIDsByUserID(ctx, dao.DB, user.ID)
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
		if totalKB, err = s.kbDAO.CountKBByTenantIDs(ctx, dao.DB, tenantIDs); err != nil {
			return nil, fmt.Errorf("failed to count knowledgebases: %w", err)
		}
		if totalChunkNum, totalTokenNum, err = s.kbDAO.SumKBChunkTokenByTenantIDs(ctx, dao.DB, tenantIDs); err != nil {
			return nil, fmt.Errorf("failed to sum knowledgebase stats: %w", err)
		}
		if totalDoc, totalDocSize, err = s.documentDAO.DocCountAndSizeByTenantIDs(ctx, dao.DB, tenantIDs); err != nil {
			return nil, fmt.Errorf("failed to count documents: %w", err)
		}
		if totalChat, err = s.chatDAO.CountChatByTenantIDs(ctx, dao.DB, tenantIDs); err != nil {
			return nil, fmt.Errorf("failed to count chats: %w", err)
		}
		if totalAgent, err = s.canvasDAO.CountAgentCanvasByTenantIDs(ctx, dao.DB, tenantIDs); err != nil {
			return nil, fmt.Errorf("failed to count agents: %w", err)
		}
		if totalAPIToken, err = s.apiTokenDAO.CountAPITokenByTenantIDs(ctx, dao.DB, tenantIDs); err != nil {
			return nil, fmt.Errorf("failed to count api tokens: %w", err)
		}
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
func (s *Service) ShowUserStorage(ctx context.Context, email string) (map[string]interface{}, error) {
	// Query user by email
	var user entity.User
	err := dao.DB.Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, common.ErrUserNotFound
	}
	tenantID := user.ID

	docCount, docSize, err := s.documentDAO.DocCountAndSizeByTenantIDs(context.Background(), dao.DB, []string{tenantID})
	if err != nil {
		return nil, fmt.Errorf("failed to query user storage: %w", err)
	}

	topFiles, err := s.documentDAO.ListTopDocumentsByTenantIDs(context.Background(), dao.DB, []string{tenantID}, 10)
	if err != nil {
		return nil, fmt.Errorf("failed to query user top documents: %w", err)
	}

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
		"File_Count": common.FormatNumber(docCount),
		"Total_Size": common.FormatBytes(docSize),
		"files":      fileList,
	}

	return result, nil
}

// ShowUserQuota show user quota for  edition
func (s *Service) ShowUserQuota(ctx context.Context, email string) (map[string]interface{}, error) {
	// Query user by email
	var user entity.User
	err := dao.DB.WithContext(ctx).Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, common.ErrUserNotFound
	}
	tenantIDs, err := s.userTenantDAO.GetTenantIDsByUserID(ctx, dao.DB, user.ID)
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
		if chatCount, err = s.chatDAO.CountChatByTenantIDs(ctx, dao.DB, tenantIDs); err != nil {
			return nil, fmt.Errorf("failed to count chats: %w", err)
		}
		if searchCount, err = s.SearchDAO.CountSearchByTenantIDs(ctx, dao.DB, tenantIDs); err != nil {
			return nil, fmt.Errorf("failed to count searches: %w", err)
		}
		if agentCount, err = s.canvasDAO.CountAgentCanvasByTenantIDs(ctx, dao.DB, tenantIDs); err != nil {
			return nil, fmt.Errorf("failed to count agents: %w", err)
		}
		if memoryCount, err = s.MemoryDAO.CountMemoryByTenantIDs(ctx, dao.DB, tenantIDs); err != nil {
			return nil, fmt.Errorf("failed to count memories: %w", err)
		}
		numApps = chatCount + searchCount + agentCount + memoryCount

		if numMembers, err = s.userTenantDAO.CountUserTenantByTenantIDs(ctx, dao.DB, tenantIDs); err != nil {
			return nil, fmt.Errorf("failed to count members: %w", err)
		}
		if numAPITokens, err = s.apiTokenDAO.CountAPITokenByTenantIDs(ctx, dao.DB, tenantIDs); err != nil {
			return nil, fmt.Errorf("failed to count api tokens: %w", err)
		}

		if numKB, err = s.kbDAO.CountKBByTenantIDs(ctx, dao.DB, tenantIDs); err != nil {
			return nil, fmt.Errorf("failed to count knowledgebases: %w", err)
		}

		if numChunk, numToken, err = s.kbDAO.SumKBChunkTokenByTenantIDs(ctx, dao.DB, tenantIDs); err != nil {
			return nil, fmt.Errorf("failed to sum knowledgebase stats: %w", err)
		}

		if numDoc, numStorageBytes, err = s.documentDAO.DocCountAndSizeByTenantIDs(ctx, dao.DB, tenantIDs); err != nil {
			return nil, fmt.Errorf("failed to count documents: %w", err)
		}
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
func (s *Service) ShowUserIndex(ctx context.Context, email string) (map[string]interface{}, error) {
	// Query user by email
	var user entity.User
	err := dao.DB.Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, common.ErrUserNotFound
	}

	cfg := server.GetConfig()
	docEngine := cfg.DocEngineType()
	if docEngine != "elasticsearch" {
		return map[string]interface{}{
			"Email":   user.Email,
			"Message": "Elasticsearch is not in use, current doc engine: " + docEngine,
		}, nil
	}

	if !cfg.IsElasticConfigured() {
		return map[string]interface{}{
			"Email":   user.Email,
			"Message": "Elasticsearch configuration not found",
		}, nil
	}

	esConfig := cfg.GetElasticsearchConfig()

	esEngine, err := elasticsearch.NewEngine(ctx, esConfig)
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

	indexStats, err := esEngine.GetIndexStats(ctx, indices)
	if err != nil {
		return map[string]interface{}{
			"Email":   user.Email,
			"Message": fmt.Sprintf("Failed to get index stats: %s", err.Error()),
		}, nil
	}

	kbCount, err := s.kbDAO.CountKBByTenantIDs(context.Background(), dao.DB, []string{tenantID})
	if err != nil {
		return nil, fmt.Errorf("failed to count knowledgebases: %w", err)
	}

	var totalDocs, totalDocSize int64
	if totalDocs, totalDocSize, err = s.documentDAO.DocCountAndSizeByTenantIDs(context.Background(), dao.DB, []string{tenantID}); err != nil {
		return nil, fmt.Errorf("failed to count documents: %w", err)
	}

	var kbChunkNum, kbTokenNum int64
	if kbChunkNum, kbTokenNum, err = s.kbDAO.SumKBChunkTokenByTenantIDs(context.Background(), dao.DB, []string{tenantID}); err != nil {
		return nil, fmt.Errorf("failed to sum knowledgebase stats: %w", err)
	}

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
		"Chunk_Count":   common.FormatNumber(kbChunkNum),
		"Token_Count":   common.FormatNumber(kbTokenNum),
		"Index_Count":   len(indexStats),
		"Index_Size":    common.FormatBytes(totalIndexSize),
		"indices":       indexStats,
	}

	return result, nil
}

// UpdateUserRole assigns a system role to a user and records the change.
func (s *Service) UpdateUserRole(ctx context.Context, actorUserID, email, roleName string) (map[string]interface{}, error) {
	actorUserID = strings.TrimSpace(actorUserID)
	if actorUserID == "" {
		return nil, common.NewCodedError(common.CodeUnauthorized, "Admin user is required")
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, common.NewCodedError(common.CodeBadRequest, "User email is required")
	}
	roleName = strings.TrimSpace(roleName)
	if roleName == "" {
		return nil, common.NewCodedError(common.CodeBadRequest, "Role name is required")
	}

	var result map[string]interface{}
	err := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user entity.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("email = ?", email).Take(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return common.ErrUserNotFound
			}
			return fmt.Errorf("failed to find user by email: %w", err)
		}

		role, err := findSystemRole(tx, roleName, true)
		if err != nil {
			return err
		}
		if user.RoleID == nil || *user.RoleID != role.ID {
			var previousRole *entity.Role
			if user.RoleID != nil {
				var oldRole entity.Role
				err := tx.Where("id = ?", *user.RoleID).Take(&oldRole).Error
				if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					return fmt.Errorf("failed to load previous user role: %w", err)
				}
				if err == nil {
					previousRole = &oldRole
				}
			}

			if err := tx.Model(&entity.User{}).Where("id = ?", user.ID).Update("role_id", role.ID).Error; err != nil {
				return fmt.Errorf("failed to update user role: %w", err)
			}
			if err := recordUserRoleChange(tx, actorUserID, &user, previousRole, role); err != nil {
				return err
			}
		}

		result = map[string]interface{}{
			"user_id":   user.ID,
			"email":     user.Email,
			"nickname":  user.Nickname,
			"role_id":   role.ID,
			"role_name": role.RoleName,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ShowUserPermission show user permissions for enterprise edition
func (s *Service) ShowUserPermission(ctx context.Context, email string) (map[string]interface{}, error) {
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

// ListUserDatasets lists datasets created by the user identified by email.
func (s *Service) ListUserDatasets(ctx context.Context, email string, page, pageSize int, order dao.OrderTerm) ([]map[string]interface{}, int64, error) {
	user, err := s.userDAO.GetByEmail(ctx, dao.DB, email)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, 0, common.ErrUserNotFound
		}
		return nil, 0, fmt.Errorf("failed to get user: %w", err)
	}

	datasets, total, err := s.kbDAO.ListCreatedByUser(ctx, dao.DB, user.ID, page, pageSize, order)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list user datasets: %w", err)
	}

	datasetIDs := make([]string, 0, len(datasets))
	for _, dataset := range datasets {
		datasetIDs = append(datasetIDs, dataset.ID)
	}
	sizes, err := s.documentDAO.SumSizeByDatasetIDs(ctx, dao.DB, datasetIDs)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get sizes for user datasets: %w", err)
	}

	result := make([]map[string]interface{}, 0, len(datasets))
	for _, dataset := range datasets {
		status := ""
		if dataset.Status != nil {
			status = *dataset.Status
		}
		tenantName := ""
		if dataset.TenantName != nil {
			tenantName = *dataset.TenantName
		}
		result = append(result, map[string]interface{}{
			"id":          dataset.ID,
			"name":        dataset.Name,
			"tenant_id":   dataset.TenantID,
			"tenant_name": tenantName,
			"avatar":      dataset.Avatar,
			"language":    dataset.Language,
			"doc_num":     dataset.DocNum,
			"token_num":   dataset.TokenNum,
			"chunk_num":   dataset.ChunkNum,
			"file_size":   sizes[dataset.ID],
			"status":      status,
			"create_date": dataset.CreateDate,
			"update_date": dataset.UpdateDate,
		})
	}
	return result, total, nil
}

// ListUserAgents lists canvases created by the user identified by email,
// filtered by the stored canvasCategory.
func (s *Service) ListUserAgents(ctx context.Context, email, canvasCategory string, page, pageSize int, order dao.OrderTerm) ([]map[string]interface{}, int64, error) {
	user, err := s.userDAO.GetByEmail(ctx, dao.DB, email)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, 0, common.ErrUserNotFound
		}
		return nil, 0, fmt.Errorf("failed to get user: %w", err)
	}

	agents, total, err := s.canvasDAO.ListCreatedByUser(ctx, dao.DB, user.ID, canvasCategory, page, pageSize, order)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get user agents: %w", err)
	}

	result := make([]map[string]interface{}, 0, len(agents))
	for _, agent := range agents {
		title := ""
		if agent.Title != nil {
			title = *agent.Title
		}
		tenantName := ""
		if agent.TenantName != nil {
			tenantName = *agent.TenantName
		}
		result = append(result, map[string]interface{}{
			"id":              agent.ID,
			"tenant_id":       agent.TenantID,
			"tenant_name":     tenantName,
			"title":           title,
			"canvas_category": agent.CanvasCategory,
			"avatar":          agent.Avatar,
			"create_date":     agent.CreateDate,
			"update_date":     agent.UpdateDate,
		})
	}
	return result, total, nil
}

// ListUserChats show user chats for enterprise edition
func (s *Service) ListUserChats(ctx context.Context, email string) ([]map[string]interface{}, error) {
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
func (s *Service) ListUserSearches(ctx context.Context, email string) ([]map[string]interface{}, error) {
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
func (s *Service) ListUserModels(ctx context.Context, email string) ([]map[string]interface{}, error) {
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
func (s *Service) ListUserFiles(ctx context.Context, email string) ([]map[string]interface{}, error) {
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
func (s *Service) ListUserProviders(ctx context.Context, email string) ([]map[string]interface{}, error) {
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
func (s *Service) ListUserProviderInstances(ctx context.Context, email, providerName string) ([]map[string]interface{}, error) {
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
func (s *Service) ListUserProviderInstanceModels(ctx context.Context, email, providerName, instanceName string) ([]map[string]interface{}, error) {
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
func (s *Service) ListUserDefaultModels(ctx context.Context, email string) ([]map[string]interface{}, error) {
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

// ShowUsersSummary show users summary for enterprise edition
func (s *Service) ShowUsersSummary(ctx context.Context) (map[string]interface{}, error) {
	row, err := s.userDAO.UserCountTotalActiveNew(context.Background(), dao.DB, 30)
	if err != nil {
		return nil, fmt.Errorf("failed to query users summary: %w", err)
	}

	result := map[string]interface{}{
		"Total_Users":      common.FormatNumber(row.TotalUsers),
		"Active_Users_30d": common.FormatNumber(row.ActiveUsers30d),
		"New_Users_30d":    common.FormatNumber(row.NewUsers30d),
	}

	return result, nil
}

// ShowUsersActivity show users activity for enterprise edition
func (s *Service) ShowUsersActivity(ctx context.Context, days, windows *int) (map[string]interface{}, error) {
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

func (s *Service) ListUsersEE(ctx context.Context, pageIndex, pageSize int, name string, status, role, sort, orderBy, plan string, top, days int, quota *int) ([]map[string]interface{}, int64, error) {
	if orderBy != "" && strings.EqualFold(orderBy, "storage") {
		topVal := 10
		if top > 0 {
			topVal = top
		}
		return s.listUsersStorageRows(ctx, topVal)
	}

	if orderBy != "" && strings.EqualFold(orderBy, "documents") {
		topVal := 10
		if top > 0 {
			topVal = top
		}
		return s.listUsersDocumentsRows(ctx, topVal)
	}

	if orderBy != "" && strings.EqualFold(orderBy, "index") {
		topVal := 10
		if top > 0 {
			topVal = top
		}
		return s.listUsersIndexRows(ctx, topVal)
	}

	if plan != "" && quota != nil {
		topVal := 30
		if top > 0 {
			topVal = top
		}
		daysVal := 30
		if days > 0 {
			daysVal = days
		}
		return s.listUsersPlanQuotaRows(ctx, topVal, plan, *quota, daysVal)
	}

	if quota != nil {
		topVal := 10
		if top > 0 {
			topVal = top
		}
		return s.listUsersQuotaRows(ctx, topVal, *quota)
	}

	opts, err := parseUserListOptions(pageIndex, pageSize, name, status, sort, orderBy)
	if err != nil {
		return nil, 0, err
	}
	rows, total, err := s.userDAO.ListForAdmin(ctx, dao.DB, opts, plan)
	if err != nil {
		return nil, 0, err
	}
	users := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		users = append(users, map[string]interface{}{
			"email":           row.Email,
			"nickname":        row.Nickname,
			"create_date":     row.CreateTime,
			"is_active":       row.IsActive,
			"is_superuser":    row.IsSuperuser,
			"last_login_time": row.LastLoginTime,
			"plan":            row.Plan,
		})
	}
	return users, total, nil
}

// ListUsersStorage list users storage for enterprise edition
func (s *Service) ListUsersStorage(ctx context.Context, pageIndex, pageSize, top int) (map[string]interface{}, error) {
	if top <= 0 {
		top = 10
	}

	users, _, err := s.listUsersStorageRows(ctx, top)
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

func (s *Service) listUsersStorageRows(ctx context.Context, top int) ([]map[string]interface{}, int64, error) {
	rows, err := s.userDAO.ListStorageRows(ctx, dao.DB, top)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query users storage: %w", err)
	}

	result := make([]map[string]interface{}, 0, len(rows))
	for i, row := range rows {
		limitDisplay := "-"
		if row.StorageLimit > 0 {
			limitDisplay = common.FormatBytes(row.StorageLimit)
		}
		lastLogin := "-"
		if row.LastLoginTime != nil {
			lastLogin = row.LastLoginTime.Format("2006-01-02 15:04:05")
		}
		result = append(result, map[string]interface{}{
			"rank":       i + 1,
			"user":       row.Email,
			"plan":       row.PlanName,
			"doc_count":  common.FormatNumber(row.DocCount),
			"doc_size":   common.FormatBytes(row.TotalSize),
			"limit":      limitDisplay,
			"last_login": lastLogin,
		})
	}

	return result, int64(len(rows)), nil
}

func (s *Service) listUsersDocumentsRows(ctx context.Context, top int) ([]map[string]interface{}, int64, error) {
	rows, err := s.userDAO.ListDocumentsRows(ctx, dao.DB, top)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query users documents: %w", err)
	}

	result := make([]map[string]interface{}, 0, len(rows))
	for i, row := range rows {
		lastLogin := "-"
		if row.LastLoginTime != nil {
			lastLogin = row.LastLoginTime.Format("2006-01-02 15:04:05")
		}
		result = append(result, map[string]interface{}{
			"rank":       i + 1,
			"user":       row.Email,
			"plan":       row.PlanName,
			"doc_count":  common.FormatNumber(row.DocCount),
			"last_login": lastLogin,
		})
	}

	return result, int64(len(rows)), nil
}

func (s *Service) listUsersIndexRows(ctx context.Context, top int) ([]map[string]interface{}, int64, error) {

	cfg := server.GetConfig()
	docEngine := cfg.DocEngineType()
	if docEngine != "elasticsearch" {
		return nil, 0, fmt.Errorf("elasticsearch is not in use, current doc engine: %s", docEngine)
	}

	if !cfg.IsElasticConfigured() {
		return nil, 0, fmt.Errorf("elasticsearch configuration not found")
	}

	esConfig := cfg.GetElasticsearchConfig()

	esEngine, err := elasticsearch.NewEngine(ctx, esConfig)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to connect to elasticsearch: %w", err)
	}
	defer esEngine.Close()

	allIndexStats, err := esEngine.GetIndexStats(ctx, []string{"ragflow*"})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get index stats: %w", err)
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

	users, err := s.userDAO.ListUsersWithPlan(context.Background(), dao.DB, tenantIDs)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query users with plan: %w", err)
	}

	userMap := make(map[string]dao.UserWithPlanRow, len(users))
	for _, u := range users {
		userMap[u.ID] = u
	}

	result := make([]map[string]interface{}, 0, len(rows))
	for i, r := range rows {
		user, ok := userMap[r.TenantID]
		tenant := r.TenantID
		plan := "-"
		lastLogin := "-"
		if ok {
			tenant = user.Email
			plan = user.PlanName
			if user.LastLoginTime != nil {
				lastLogin = user.LastLoginTime.Format("2006-01-02 15:04:05")
			}
		}

		result = append(result, map[string]interface{}{
			"rank":       i + 1,
			"tenant":     tenant,
			"plan":       plan,
			"index_name": r.IndexName,
			"health":     r.Health,
			"docs.count": r.DocsCount,
			"store.size": r.StoreSize,
			"last_login": lastLogin,
		})
	}

	return result, int64(len(rows)), nil
}

func (s *Service) listUsersQuotaRows(ctx context.Context, top, quotaThreshold int) ([]map[string]interface{}, int64, error) {
	rows, err := s.userDAO.ListQuotaRows(ctx, dao.DB, top, quotaThreshold)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query users quota: %w", err)
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

	return result, int64(len(rows)), nil
}

// listUsersPlanQuotaRows returns active users filtered by plan name whose storage usage
// percentage meets or exceeds the quota threshold within the last N days.
// Active users are defined by last_login_time >= now - days.
// Results are sorted by storage_used descending, limited to top N.
func (s *Service) listUsersPlanQuotaRows(ctx context.Context, top int, planName string, quotaThreshold, days int) ([]map[string]interface{}, int64, error) {
	rows, err := s.userDAO.ListPlanQuotaRows(ctx, dao.DB, top, planName, quotaThreshold, days)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query users plan quota: %w", err)
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

	return result, int64(len(rows)), nil
}

// ListUsersDocuments list users documents for enterprise edition
func (s *Service) ListUsersDocuments(ctx context.Context, pageIndex, pageSize, top int) (map[string]interface{}, error) {

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
func (s *Service) ListUsersIndex(ctx context.Context, pageIndex, pageSize, top int) (map[string]interface{}, error) {

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
func (s *Service) ListUsersQuota(ctx context.Context, pageIndex, pageSize, top int, quotaThreshold *int, plan *string, days *int) (map[string]interface{}, error) {

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
func (s *Service) ShowUsersPlanSummary(ctx context.Context) (map[string]interface{}, error) {

	result := map[string]interface{}{
		"command": "show_users_plan_summary",
		"error":   "'Show users plan summary' is not supported",
	}

	return result, nil
}

// ShowUsersPlanQuota show users plan quota for enterprise edition
func (s *Service) ShowUsersPlanQuota(ctx context.Context, quota int) (map[string]interface{}, error) {
	if quota <= 0 {
		quota = 50
	}

	rows, err := s.userDAO.GetPlanQuotaHitRate(context.Background(), dao.DB, quota)
	if err != nil {
		return nil, fmt.Errorf("failed to query plan quota hit rate: %w", err)
	}

	result := map[string]interface{}{
		"quota": quota,
	}

	for _, r := range rows {
		hitRate := "0.0%"
		if r.TotalUsers > 0 {
			hitRate = fmt.Sprintf("%.1f%%", float64(r.HitUsers)*100.0/float64(r.TotalUsers))
		}
		result[fmt.Sprintf("Total_Users_%s", r.PlanName)] = common.FormatNumber(r.TotalUsers)
		result[fmt.Sprintf("Quota_Hit_%d%%_%s", quota, r.PlanName)] = common.FormatNumber(r.HitUsers)
		result[fmt.Sprintf("Hit_Rate_%s", r.PlanName)] = hitRate
	}

	return result, nil
}

// ShowUsersQuotaSummary show users quota summary for enterprise edition
func (s *Service) ShowUsersQuotaSummary(ctx context.Context) (map[string]interface{}, error) {
	products, err := s.BillingProductDAO.ListBillingProducts(context.Background(), dao.DB)
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

	usageRows, err := s.BillingSubscriptionDAO.ListQuotaUsage(context.Background(), dao.DB)
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

// ShowIngestionTasksSummary show ingestion tasks summary for enterprise edition
func (s *Service) ShowIngestionTasksSummary(ctx context.Context) (map[string]interface{}, error) {
	agg, err := s.taskDAO.TaskAggByStatus(context.Background(), dao.DB)
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

	typeRows, err := s.taskDAO.GetTaskTypeCounts(context.Background(), dao.DB)
	if err != nil {
		return nil, fmt.Errorf("failed to query task type counts: %w", err)
	}

	taskTypeLabels := map[string]string{
		"":          "Parse",
		"build_toc": "PageIndex",
	}
	for _, r := range typeRows {
		label, ok := taskTypeLabels[r.TaskType]
		if !ok {
			label = r.TaskType
		}
		result[label] = common.FormatNumber(r.Count)
	}

	return result, nil
}

// ShowDataSummary show data summary for enterprise edition
func (s *Service) ShowDataSummary(ctx context.Context) (map[string]interface{}, error) {
	storageRow, err := s.fileDAO.StorageFileSizeDocSize(context.Background(), dao.DB)
	if err != nil {
		return nil, fmt.Errorf("failed to query storage summary: %w", err)
	}

	taskRow, err := s.taskDAO.TaskCountByStatus(context.Background(), dao.DB)
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

	cfg := server.GetConfig()
	docEngine := cfg.DocEngineType()
	if docEngine != "elasticsearch" {
		return nil, fmt.Errorf("elasticsearch is not in use, current doc engine: %s", docEngine)
	}

	if !cfg.IsElasticConfigured() {
		return nil, fmt.Errorf("elasticsearch configuration not found")
	}

	esConfig := cfg.GetElasticsearchConfig()

	esEngine, err := elasticsearch.NewEngine(ctx, esConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect elasticsearch: %w", err)
	}
	defer esEngine.Close()

	var clusterStats map[string]interface{}
	clusterStats, err = esEngine.GetClusterStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query elasticsearch cluster stats: %w", err)
	}

	if v, ok := clusterStats["store_size"]; ok {
		result["ES_Store_Size"] = v
	}
	if v, ok := clusterStats["total_dataset_size"]; ok {
		result["ES_Vector_Dataset_Size"] = v
	}

	return result, nil
}

// ShowDataOrphan show data orphan for enterprise edition
func (s *Service) ShowDataOrphan(ctx context.Context) (map[string]interface{}, error) {
	kbRow, err := s.kbDAO.GetKBOrphans(context.Background(), dao.DB)
	if err != nil {
		return nil, fmt.Errorf("failed to query knowledgebase orphans: %w", err)
	}

	docRow, err := s.documentDAO.GetDocOrphans(context.Background(), dao.DB)
	if err != nil {
		return nil, fmt.Errorf("failed to query document orphans: %w", err)
	}

	taskNoDoc, err := s.taskDAO.GetTaskOrphans(context.Background(), dao.DB)
	if err != nil {
		return nil, fmt.Errorf("failed to query task orphans: %w", err)
	}

	fileRow, err := s.fileDAO.GetFileOrphans(context.Background(), dao.DB)
	if err != nil {
		return nil, fmt.Errorf("failed to query file orphans: %w", err)
	}

	utRow, err := s.userTenantDAO.GetUserTenantOrphans(context.Background(), dao.DB)
	if err != nil {
		return nil, fmt.Errorf("failed to query user_tenant orphans: %w", err)
	}

	esOrphanCount := int64(0)

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
		"ES_Index_No_Tenant":   common.FormatNumber(esOrphanCount),
	}

	cfg := server.GetConfig()
	docEngine := cfg.DocEngineType()
	if docEngine != "elasticsearch" {
		result["ES Error"] = fmt.Sprintf("elasticsearch is not in use, current doc engine: %s", docEngine)
		return result, nil
	}

	if !cfg.IsElasticConfigured() {
		result["ES Error"] = "elasticsearch configuration not found"
		return result, nil
	}

	esConfig := cfg.GetElasticsearchConfig()
	esEngine, err := elasticsearch.NewEngine(ctx, esConfig)
	if err != nil {
		result["ES Error"] = fmt.Sprintf("failed to connect elasticsearch: %v", err)
		return result, nil
	}

	defer esEngine.Close()

	allIndexStats, err := esEngine.GetIndexStats(ctx, []string{"ragflow*"})
	if err != nil {
		result["ES Error"] = fmt.Sprintf("ES query failed: %s", err.Error())
		return result, nil
	}

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

	return result, nil
}

// ShowDataStorage show data storage for enterprise edition
func (s *Service) ShowDataStorage(ctx context.Context) (map[string]interface{}, error) {
	fileCount, fileSize, err := s.fileDAO.FileCountAndSize(context.Background(), dao.DB)
	if err != nil {
		return nil, fmt.Errorf("failed to query file stats: %w", err)
	}

	docStat, err := s.documentDAO.DocCountSizeChunkToken(context.Background(), dao.DB)
	if err != nil {
		return nil, fmt.Errorf("failed to query document stats: %w", err)
	}

	totalKB, err := s.kbDAO.CountAllValidKB(context.Background(), dao.DB)
	if err != nil {
		return nil, fmt.Errorf("failed to count knowledgebases: %w", err)
	}

	totalTenants, err := s.tenantDAO.CountAllTenants(context.Background(), dao.DB)
	if err != nil {
		return nil, fmt.Errorf("failed to count tenants: %w", err)
	}

	totalChat, err := s.chatDAO.CountAllChat(context.Background(), dao.DB)
	if err != nil {
		return nil, fmt.Errorf("failed to count chats: %w", err)
	}

	totalAgent, err := s.canvasDAO.CountAllAgentCanvas(context.Background(), dao.DB)
	if err != nil {
		return nil, fmt.Errorf("failed to count agents: %w", err)
	}

	totalAPIToken, err := s.apiTokenDAO.CountAllAPIToken(context.Background(), dao.DB)
	if err != nil {
		return nil, fmt.Errorf("failed to count api tokens: %w", err)
	}

	activeSubs, err := s.BillingSubscriptionDAO.CountActiveSubscriptions(context.Background(), dao.DB)
	if err != nil {
		return nil, fmt.Errorf("failed to count subscriptions: %w", err)
	}

	totalApps := totalChat + totalAgent

	result := map[string]interface{}{
		"Total_Files":                  common.FormatNumber(fileCount),
		"Total_File_Size":              common.FormatBytes(fileSize),
		"Total_Documents":              common.FormatNumber(docStat.TotalDocs),
		"Total_Document_Size":          common.FormatBytes(docStat.TotalDocSize),
		"Total_Knowledgebases":         common.FormatNumber(totalKB),
		"Total_Chunks":                 common.FormatNumber(docStat.TotalChunks),
		"Total_Tokens_LLM_used":        common.FormatNumber(docStat.TotalTokens),
		"Total_Tenants":                common.FormatNumber(totalTenants),
		"Total_Apps_Chat_Search_Agent": common.FormatNumber(totalApps),
		"Total_API_Tokens":             common.FormatNumber(totalAPIToken),
		"Active_Subscriptions":         common.FormatNumber(activeSubs),
	}

	fileTypeRows, err := s.fileDAO.ListFileTypeStats(context.Background(), dao.DB)
	if err != nil {
		return nil, fmt.Errorf("failed to query file type stats: %w", err)
	}

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
func (s *Service) ShowDataIndex(ctx context.Context) (map[string]interface{}, error) {
	cfg := server.GetConfig()
	docEngine := cfg.DocEngineType()
	if docEngine != "elasticsearch" {
		return nil, fmt.Errorf("elasticsearch is not in use, current doc engine: %s", docEngine)
	}

	if !cfg.IsElasticConfigured() {
		return nil, fmt.Errorf("elasticsearch configuration not found")
	}

	esConfig := cfg.GetElasticsearchConfig()

	esEngine, err := elasticsearch.NewEngine(ctx, esConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect elasticsearch: %w", err)
	}
	defer esEngine.Close()

	clusterStats, err := esEngine.GetClusterStats(ctx)
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
func (s *Service) PurgeOrphanData(ctx context.Context, preview bool) (map[string]interface{}, error) {

	result := map[string]interface{}{
		"command": "purge_orphan_data",
		"preview": preview,
		"error":   "'Purge orphan data' is not supported",
	}

	return result, nil
}

// PurgeUserData purge user data for enterprise edition
func (s *Service) PurgeUserData(ctx context.Context, email string, preview bool) (map[string]interface{}, error) {
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
func (s *Service) PurgeUsersData(ctx context.Context, preview bool, days int, userPlan *string, userActivity *string) (map[string]interface{}, error) {

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
func (s *Service) GenerateUserAPIKey(ctx context.Context, username string) (map[string]interface{}, error) {

	user, err := s.userDAO.GetByEmail(ctx, dao.DB, username)
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
func (s *Service) DeleteUserAPIKey(ctx context.Context, username, key string) (map[string]interface{}, error) {

	user, err := s.userDAO.GetByEmail(ctx, dao.DB, username)
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
func (s *Service) ListUserAPIKeys(ctx context.Context, username string) ([]map[string]interface{}, error) {

	user, err := s.userDAO.GetByEmail(ctx, dao.DB, username)
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

// ListIngestionTasks lists current ingestion tasks across all users for administrators.
func (s *Service) ListIngestionTasks(ctx context.Context, filter dao.AdminIngestionTaskFilter) ([]servicepkg.AdminIngestionTask, int64, error) {
	return s.ingestionTaskSvc.ListForAdmin(ctx, filter)
}

// RemoveIngestionTasks reports that admin task removal is not supported.
func (s *Service) RemoveIngestionTasks(ctx context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{
		"command": "remove_ingestion_tasks",
		"error":   "'remove ingestion tasks' is not supported",
	}, nil
}

// StopIngestionTasks reports that admin task stopping is not supported.
func (s *Service) StopIngestionTasks(ctx context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{
		"command": "stop_ingestion_tasks",
		"error":   "'stop ingestion tasks' is not supported",
	}, nil
}

func CheckClientStatus(serverName string, status *common.BaseMessage) error {
	return nil
}

func CheckLicense() (common.ErrorCode, map[string]interface{}, string) {
	return common.CodeLicenseValid, map[string]interface{}{}, ""
}

// DownloadSensitiveWords download sensitive words
func (s *Service) DownloadSensitiveWords(ctx context.Context) ([]map[string]interface{}, error) {
	result := []map[string]interface{}{
		{
			"command": "download_sensitive_words",
			"error":   "'Download sensitive words' is not supported",
		},
	}
	return result, nil
}

// UploadSensitiveWords upload sensitive words
func (s *Service) UploadSensitiveWords(ctx context.Context, file *multipart.FileHeader) ([]map[string]interface{}, error) {
	result := []map[string]interface{}{
		{
			"command":  "upload_sensitive_words",
			"filename": file.Filename,
			"error":    "'Upload sensitive words' is not supported",
		},
	}
	return result, nil
}

// BindVerificationEmail bind verification email
func (s *Service) BindVerificationEmail(ctx context.Context, email, host string, port int, username, password string, useTLS, useSSL bool) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"command":  "bind_verification_email",
		"email":    email,
		"host":     host,
		"port":     port,
		"username": username,
		"password": password,
		"use_tls":  useTLS,
		"use_ssl":  useSSL,
		"error":    "'Bind verification email' is not supported",
	}
	return result, nil
}

// ShowVerificationEmail show verification email
func (s *Service) ShowVerificationEmail(ctx context.Context) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"command": "show_verification_email",
		"error":   "'Show verification email' is not supported",
	}
	return result, nil
}

// GetTokenStats returns API token statistics for the user.
func (s *Service) GetTokenStats(ctx context.Context, userName, fromDate, toDate, granularity string) ([]map[string]interface{}, error) {
	user, err := s.userDAO.GetByEmail(ctx, dao.DB, userName)
	if err != nil {
		return nil, err
	}
	clickhouseDriver := clickhouse.GetDriver()
	tokenStats, err := clickhouseDriver.UserStats(user.ID, fromDate, toDate, granularity)
	if err != nil {
		return nil, err
	}

	maps := make([]map[string]interface{}, len(tokenStats))

	for i, r := range tokenStats {
		maps[i] = map[string]interface{}{
			"stat_date":            r.StatDate,
			"user_id":              r.UserID,
			"email":                r.UserEmail,
			"group_id":             r.GroupID,
			"group_name":           r.GroupName,
			"app_id":               r.AppID,
			"app_name":             r.AppName,
			"provider_name":        r.ProviderName,
			"model_name":           r.ModelName,
			"request_count":        r.RequestCount,
			"total_input_tokens":   r.TotalInputTokens,
			"total_output_tokens":  r.TotalOutputTokens,
			"total_tokens":         r.TotalTokens,
			"avg_response_time_ms": r.AvgResponseTime,
			"error_count":          r.ErrorCount,
			"success_rate":         r.SuccessRate,
		}
	}

	return maps, nil
}

// GetTokenUsersStats returns API token statistics for all users.
func (s *Service) GetTokenUsersStats(ctx context.Context, fromDate, toDate string, top int) ([]map[string]interface{}, error) {
	result := []map[string]interface{}{
		{
			"command":   "get_token_users_stats",
			"from_date": fromDate,
			"to_date":   toDate,
			"top":       top,
			"error":     "'Get API token users stats' is not supported",
		},
	}
	return result, nil
}

// GetTokenStatsSummary returns API token statistics summary for all users.
func (s *Service) GetTokenStatsSummary(ctx context.Context, fromDate, toDate string) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"command":   "get_token_stats_summary",
		"from_date": fromDate,
		"to_date":   toDate,
		"error":     "'Get API token stats summary' is not supported",
	}

	return result, nil
}

// ListLogs lists operation logs for the user.
func (s *Service) ListLogs(ctx context.Context, userName string, days int) ([]map[string]interface{}, error) {
	user, err := s.userDAO.GetByEmail(ctx, dao.DB, userName)
	if err != nil {
		return nil, err
	}
	clickhouseDriver := clickhouse.GetDriver()
	endTime := time.Now()
	startAt := endTime.AddDate(0, 0, -days)
	startAt = startAt.Truncate(24 * time.Hour)
	operationLogs, err := clickhouseDriver.OperationLogStats(user.ID, startAt, endTime, 100, 0)
	if err != nil {
		return nil, err
	}

	maps := make([]map[string]interface{}, len(operationLogs))
	for i, r := range operationLogs {
		maps[i] = map[string]interface{}{
			"event_id":      r.EventID,
			"event_time":    r.EventTime,
			"email":         userName,
			"ip":            r.IPAddress,
			"operation":     r.Operation,
			"method":        r.HTTPMethod,
			"resource_type": r.ResourceType,
			"resource_id":   r.ResourceID,
			"resource_name": r.ResourceName,
			"error_code":    r.ErrorCode,
			"message":       r.Message,
			"duration_ms":   r.DurationMS,
		}
	}

	return maps, nil
}

func (s *Service) GetEEServicesStatus(ctx context.Context) []ServiceStatus {
	return []ServiceStatus{}
}
