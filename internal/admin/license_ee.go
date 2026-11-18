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
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/utility"
	"strconv"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

func InitLicense(ctx context.Context) error {

	var license *utility.LicenseV2 = nil
	// Check if license can be read from database
	licenseDAO := dao.NewLicenseDAO()
	licenseRecord, err := licenseDAO.GetLatest(ctx, dao.DB)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			collector := utility.NewFingerprintCollector(true)
			fingerprint, err2 := collector.CollectV2()
			if err2 != nil {
				common.Fatal(fmt.Sprintf("Failed to collect hardware info: %v", err2))
				return err2
			}

			trialLicenseLimit := os.Getenv("TRIAL_LICENSE_LIMIT")
			if trialLicenseLimit == "" {
				trialLicenseLimit = "10080" // 7 * 24 * 60 minutes
			}

			var trialLicenseLimitInt int
			trialLicenseLimitInt, err = strconv.Atoi(trialLicenseLimit)
			if err != nil {
				common.Fatal(fmt.Sprintf("Error parsing trial license limit: %v", err))
				return err
			}

			if trialLicenseLimitInt <= 0 || trialLicenseLimitInt > 30*24*60 {
				common.Fatal("Invalid trial license limit")
				return errors.New("invalid trial license limit")
			}

			license, err = generateTrialLicenseV2(trialLicenseLimitInt, fingerprint)
			if err != nil {
				common.Fatal(fmt.Sprintf("Error generating trial license: %v", err))
				return err
			}

			var encryptedData string
			encryptedData, err = utility.GenerateLicenseV2(license)
			if err != nil {
				common.Fatal(fmt.Sprintf("Error generating license: %v", err2))
				return err
			}

			err = licenseDAO.Create(ctx, dao.DB, license.LicenseID, encryptedData)
			if err != nil {
				common.Fatal(fmt.Sprintf("Error storing license: %v", err2))
				return err
			}
		} else {
			return err
		}
	} else {
		license, err = utility.DecodeLicenseV2(licenseRecord.License)
		if err != nil {
			common.Fatal(fmt.Sprintf("Init license: error parsing license, %v", err))
			return err
		}
	}

	// Set to global status
	GlobalServerStatusStore.SetLicense(license)
	err = InitFirstTimeRecord(ctx)
	if err != nil {
		common.Fatal("Error initializing first time record: %v", zap.Error(err))
	}
	common.Info("License is valid")
	StartUpdateRecordTask()
	return nil
}

func AppendServiceList(serviceInfos []*common.BaseMessage) []*common.BaseMessage {
	serviceInfos = append(serviceInfos, GlobalServerStatusStore.GetAllStatuses()...)
	return serviceInfos
}

func generateTrialLicenseV2(minutes int, fingerprint *utility.Fingerprint) (*utility.LicenseV2, error) {
	fingerprintJson, err := json.Marshal(fingerprint)
	if err != nil {
		common.Fatal(fmt.Sprintf("Failed to marshal fingerprint: %v", err))
		return nil, err
	}
	hash := md5.Sum(fingerprintJson)
	digest := hex.EncodeToString(hash[:])

	licenseV2 := &utility.LicenseV2{
		Version:      2,
		IssuedAt:     time.Now(),
		ValidFrom:    time.Now(),
		Minutes:      minutes,
		CustomerName: "trial-customer",
		Digest:       digest,
		Fingerprint:  *fingerprint,
		APIQuota: utility.APIQuota{
			Count:          0,
			CPUNum:         0,
			MaxConcurrency: 0,
		},
		IngestorQuota: utility.IngestorQuota{
			Count:     0,
			CPUNum:    0,
			WorkerNum: 0,
		},
		SyncerQuota: utility.SyncerQuota{
			Count:     0,
			CPUNum:    0,
			WorkerNum: 0,
		},
		DeepDocCPUQuota: utility.DeepDocCPUQuota{
			Count:          0,
			CPUNum:         0,
			WorkerNum:      0,
			MaxConcurrency: 0,
		},
		DeepDocGPUQuota: utility.DeepDocGPUQuota{
			Count:          0,
			GPUNum:         0,
			CPUNum:         0,
			WorkerNum:      0,
			MaxConcurrency: 0,
		},
		DataQuota: utility.DataQuota{
			TotalGB: 0,
		},
		TenantQuota: utility.TenantQuota{
			Count: 0,
		},
		Type: utility.TRIAL,
	}

	licenseV2.Features = make(map[string]bool)

	licenseV2.Features["single_tenant"] = true
	licenseV2.Features["rbac"] = true
	licenseV2.Features["group_and_share"] = true
	licenseV2.Features["deepdoc_pro"] = true
	licenseV2.Features["deepdoc_gpu"] = true
	licenseV2.Features["wps"] = true
	licenseV2.Features["cad"] = true
	licenseV2.Features["evaluation"] = true
	licenseV2.Features["multi_modal"] = true
	licenseV2.Features["stats"] = true
	licenseV2.Features["analytics_log"] = true

	licenseV2.ValidUntil = licenseV2.ValidFrom.Add(time.Duration(minutes) * time.Minute)
	licenseV2.LicenseID = generateLicenseID(string(licenseV2.Type))

	return licenseV2, nil
}

// generateLicenseID creates a unique license ID
func generateLicenseID(prefix string) string {
	b := make([]byte, 8)
	_, err := rand.Read(b)
	if err != nil {
		// If random fails, fallback to timestamp-based ID
		return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), time.Now().Unix())
	}
	return fmt.Sprintf("%s-%x-%d", prefix, b, time.Now().Unix())
}

func (s *Service) CheckLicense(ctx context.Context) (common.ErrorCode, error) {
	currentLicense := GlobalServerStatusStore.GetLicense()
	var licenseCheckResult string
	if currentLicense == nil {
		licenseCode := GlobalServerStatusStore.GetLicenseCode()
		if licenseCode != common.CodeLicenseValid {
			return licenseCode, errors.New(licenseCode.Message())
		}

		licenseCheckResult = "License is not ready"
		return licenseCode, errors.New(licenseCheckResult)
	}

	lastRecord, err := GetLastTimeRecord(ctx)
	if err != nil {
		common.Warn(fmt.Sprintf("Fail to get last time record: %v", err))
		return common.CodeLicenseUnexpectedError, err
	}

	if lastRecord.Nonce != currentLicense.Digest {
		return common.CodeLicenseDigestError, fmt.Errorf("time record digest is mismatched")
	}

	nowTS := time.Now().Unix()
	now := time.Now()
	if currentLicense.ValidFrom.After(now) {
		licenseCheckResult = "License not valid yet"
		return common.CodeLicenseNotValidYet, errors.New(licenseCheckResult)
	}

	if currentLicense.ValidUntil.Before(now) {
		licenseCheckResult = "License expired"
		return common.CodeLicenseExpiredError, errors.New(licenseCheckResult)
	}
	// 2. check time rollback
	lastTimestamp := lastRecord.Timestamp
	if nowTS < lastTimestamp {
		// convert following timestamp to time.Time
		return common.CodeLicenseTimeRollback, fmt.Errorf("time rollback detected")
	}

	licenseCheckResult = "license valid"

	return common.CodeLicenseValid, errors.New(licenseCheckResult)
}

func extractLicenseV2(licenseStr string) (*utility.LicenseV2, map[string]interface{}, error) {

	licenseV2, err := utility.DecodeLicenseV2(licenseStr)
	if err != nil {
		common.Error("Extract license: error parsing license, ", err)
		return nil, nil, err
	}

	common.Debug(fmt.Sprintf("License ID:     %s\n", licenseV2.LicenseID))
	common.Debug(fmt.Sprintf("Version:     %d\n", licenseV2.Version))
	common.Debug(fmt.Sprintf("License to:     %s\n", licenseV2.CustomerName))
	common.Debug(fmt.Sprintf("Issued at:     %s\n", formatLicenseTime(licenseV2.IssuedAt)))
	common.Debug(fmt.Sprintf("Valid from:     %s\n", formatLicenseTime(licenseV2.ValidFrom)))
	common.Debug(fmt.Sprintf("Valid until:     %s\n", formatLicenseTime(licenseV2.ValidUntil)))

	result := map[string]interface{}{}
	result["ID"] = licenseV2.LicenseID
	result["Version"] = licenseV2.Version
	result["CustomerName"] = licenseV2.CustomerName
	result["IssuedAt"] = formatLicenseTime(licenseV2.IssuedAt)
	result["ValidFrom"] = formatLicenseTime(licenseV2.ValidFrom)
	result["ValidUntil"] = formatLicenseTime(licenseV2.ValidUntil)
	if licenseV2.APIQuota.Count == 0 {
		result["API Server count"] = "Unlimited"
	} else {
		result["API Server count"] = licenseV2.APIQuota.Count
	}

	if licenseV2.APIQuota.MaxConcurrency == 0 {
		result["Concurrency per API Server "] = "Unlimited"
	} else {
		result["Concurrency per API Server "] = licenseV2.APIQuota.MaxConcurrency
	}

	totalConcurrency := licenseV2.APIQuota.MaxConcurrency * licenseV2.APIQuota.Count
	if totalConcurrency == 0 {
		result["Total concurrency of API Server"] = "Unlimited"
	} else {
		result["Total concurrency of API Server"] = totalConcurrency
	}

	if licenseV2.IngestorQuota.Count == 0 {
		result["Ingestor count"] = "Unlimited"
	} else {
		result["Ingestor count"] = licenseV2.IngestorQuota.Count
	}

	if licenseV2.SyncerQuota.Count == 0 {
		result["Syncer count"] = "Unlimited"
	} else {
		result["Syncer count"] = licenseV2.SyncerQuota.Count
	}

	result["DeepDocGPU count"] = licenseV2.DeepDocGPUQuota.Count
	result["DeepDocCPU count"] = licenseV2.DeepDocCPUQuota.Count

	result["Features"] = licenseV2.GetFeatures()

	return licenseV2, result, nil
}

func ValidateLicenseDigest(licenseDigest string) error {
	// Get fingerprint and generate digest
	collector := utility.NewFingerprintCollector(true)
	fingerprint, err := collector.CollectV2()
	if err != nil {
		common.Fatal(fmt.Sprintf("Failed to collect hardware info: %v", err))
		return err
	}
	fingerprintJson, err := json.Marshal(fingerprint)
	if err != nil {
		common.Fatal(fmt.Sprintf("Failed to marshal fingerprint: %v", err))
		return err
	}

	hash := md5.Sum(fingerprintJson)
	digest := hex.EncodeToString(hash[:])

	if licenseDigest != digest {
		return errors.New("license digest mismatch")
	}
	return nil
}

func CheckLicenseValidity(currentLicense *utility.LicenseV2, licenseCode common.ErrorCode) common.ErrorCode {

	if licenseCode != common.CodeLicenseValid {
		return licenseCode
	}

	if currentLicense == nil {
		return common.CodeLicenseNotFound
	}

	now := time.Now()
	// if current time is not in the valid time range, return error
	if currentLicense.ValidFrom.After(now) {
		return common.CodeLicenseInactiveError
	}

	if currentLicense.ValidUntil.Before(now) {
		return common.CodeLicenseExpiredError
	}

	return common.CodeLicenseValid
}

type TimeRecord struct {
	Timestamp  int64  `json:"ts"`
	Nonce      string `json:"n"`
	CheckCount int    `json:"c"`
}

func SaveTimeRecord(ctx context.Context, lastData *TimeRecord) error {
	var lastCheckCount = 0

	if lastData != nil {
		lastCheckCount = lastData.CheckCount
	}

	digest := GlobalServerStatusStore.GetLicense().Digest

	now := time.Now().Unix()

	// 3. create new data
	newData := TimeRecord{
		Timestamp:  now,
		Nonce:      digest,
		CheckCount: lastCheckCount + 1,
	}

	// 4. encryption
	encrypted, err := EncryptTimeRecord(&newData)
	if err != nil {
		return fmt.Errorf("fail to encrypt: %v", err)
	}

	// 5. insert new record
	var timeRecordDao *dao.TimeRecordDAO
	timeRecordDao = dao.NewTimeRecordDAO()
	err = timeRecordDao.Create(ctx, dao.DB, &entity.TimeRecord{
		Data:      encrypted,
		CreatedAt: time.Now(),
	})
	if err != nil {
		return err
	}
	return err
}

// GetLastTimeRecord get last time record
func GetLastTimeRecord(ctx context.Context) (*TimeRecord, error) {
	var timeRecordDao *dao.TimeRecordDAO
	timeRecordDao = dao.NewTimeRecordDAO()
	timeRecords, err := timeRecordDao.GetRecent(ctx, dao.DB, 1)
	if err != nil {
		return nil, err
	}

	if len(timeRecords) == 0 {
		return nil, sql.ErrNoRows
	}

	timeRecord := timeRecords[0]

	return DecryptTimeRecord(timeRecord.Data)
}

// GetAllTimeRecords get all records
func GetAllTimeRecords(ctx context.Context) ([]*TimeRecord, error) {
	var timeRecordDao *dao.TimeRecordDAO
	timeRecordDao = dao.NewTimeRecordDAO()
	timeRecords, err := timeRecordDao.GetAll(ctx, dao.DB)
	if err != nil {
		return nil, err
	}

	var results = make([]*TimeRecord, 0)
	for _, timeRecord := range timeRecords {
		data, err := DecryptTimeRecord(timeRecord.Data)
		if err != nil {
			common.Warn(fmt.Sprintf("error decrypting time record: %v", err))
			continue // skip error
		}
		results = append(results, data)
	}

	return results, nil
}

// EncryptTimeRecord encrypt time record
func EncryptTimeRecord(data *TimeRecord) (string, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	hash := sha256.New()
	ciphertext, err := rsa.EncryptOAEP(hash, rand.Reader, utility.TimeRecordPublicKey, jsonData, nil)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptTimeRecord encrypt time record
func DecryptTimeRecord(encrypted string) (*TimeRecord, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		return nil, err
	}

	hash := sha256.New()
	plaintext, err := rsa.DecryptOAEP(hash, rand.Reader, utility.TimeRecordPrivateKey, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	var data TimeRecord
	err = json.Unmarshal(plaintext, &data)
	if err != nil {
		return nil, err
	}

	return &data, nil
}

// CleanOldRecords time records one year ago
func CleanOldRecords(ctx context.Context) error {
	var timeRecordDao *dao.TimeRecordDAO
	timeRecordDao = dao.NewTimeRecordDAO()
	err := timeRecordDao.DeleteOldest(ctx, dao.DB, 365*24)
	if err != nil {
		return err
	}
	return nil
}

func InitFirstTimeRecord(ctx context.Context) error {
	_, err := GetLastTimeRecord(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		err = SaveTimeRecord(ctx, nil)
		if err != nil {
			return err
		}
	}
	return nil
}

func StartTimeRecordService(interval time.Duration) error {
	common.Debug("Try to update time record")
	// Get last time
	ctx := context.Background()
	lastData, err := GetLastTimeRecord(ctx)
	if err != nil {
		GlobalServerStatusStore.SetLicenseStatusCode(common.CodeLicenseUnexpectedError)
		return fmt.Errorf("fail to get last time record: %v", err)
	}

	currentLicense := GlobalServerStatusStore.GetLicense()
	if lastData.Nonce != currentLicense.Digest {
		GlobalServerStatusStore.SetLicenseStatusCode(common.CodeLicenseDigestError)
		return errors.New("digest error")
	}

	nowTS := time.Now().Unix()
	now := time.Now()
	if currentLicense.ValidFrom.After(now) {
		GlobalServerStatusStore.SetLicenseStatusCode(common.CodeLicenseNotValidYet)
		return errors.New("license not valid yet")
	}

	if currentLicense.ValidUntil.Before(now) {
		GlobalServerStatusStore.SetLicenseStatusCode(common.CodeLicenseExpiredError)
		return errors.New("license expired")
	}
	// 2. check time rollback
	lastTimestamp := lastData.Timestamp
	if nowTS < lastTimestamp {
		// convert following timestamp to time.Time
		lastTime := time.Unix(lastTimestamp, 0)
		currentTime := time.Unix(nowTS, 0)
		GlobalServerStatusStore.SetLicenseStatusCode(common.CodeLicenseTimeRollback)
		return fmt.Errorf("detect time rollback: last time=%s, current=%s", lastTime.Format("2006-01-02 15:04:05"), currentTime.Format("2006-01-02 15:04:05"))
	}

	// if time gap is not larger than interval
	timeGap := nowTS - lastTimestamp
	if timeGap < int64(interval.Seconds()) {
		// not error
		common.Debug(fmt.Sprintf("Time gap: %d seconds, not reach the time limit %d seconds", timeGap, int64(interval.Seconds())))
		return nil
	}

	err = SaveTimeRecord(ctx, lastData)
	return err
}
