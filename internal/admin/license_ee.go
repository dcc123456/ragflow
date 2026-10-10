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

	var license *utility.License = nil
	// Check if license can be read from database
	licenseDAO := dao.NewLicenseDAO()
	licenseRecord, err := licenseDAO.GetLatest(ctx, dao.DB)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			collector := utility.NewFingerprintCollector(true)
			clusterInfo, err2 := collector.Collect()
			if err2 != nil {
				common.Fatal(fmt.Sprintf("Failed to collect hardware info: %v", err))
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

			duration := time.Duration(trialLicenseLimitInt) * time.Minute
			license, err2 = generateTrialLicense(duration, clusterInfo)
			if err2 != nil {
				common.Fatal(fmt.Sprintf("Error generating trial license: %v", err))
				return err2
			}

			var encryptedData string
			encryptedData, err2 = utility.GenerateSimpleStringLicense(license)
			if err2 != nil {
				common.Fatal(fmt.Sprintf("Error generating license: %v", err))
				return err2
			}

			err2 = licenseDAO.Create(ctx, dao.DB, license.LicenseID, encryptedData)
			if err2 != nil {
				common.Fatal(fmt.Sprintf("Error storing license: %v", err))
				return err2
			}
		} else {
			return err
		}
	} else {
		license, err = parseLicenseFromStr(licenseRecord.License)
		if err != nil {
			common.Fatal(fmt.Sprintf("Error parsing license: %v", err))
			return err
		}
	}

	// Set to global status
	SetMemLicense(license)
	err = InitFirstTimeRecord(ctx)
	if err != nil {
		common.Fatal("Error initializing first time record: %v", zap.Error(err))
	}
	common.Info("License is valid")
	StartUpdateRecordTask()
	return nil
}

// generateLicenseID creates a unique license ID
func generateTrialLicenseID() string {
	return fmt.Sprintf("TRIAL-%d", time.Now().Unix())
}

// GenerateTrialLicense creates a trial license
func generateTrialLicense(duration time.Duration, clusterInfo *utility.ClusterInfo) (*utility.License, error) {
	// if duration is not between 1 minute to 30 days, return error
	if duration < time.Minute || duration > time.Duration(30)*24*time.Hour {
		return nil, errors.New("duration must be between 1 minute to 30 days")
	}

	now := time.Now()

	license := &utility.License{
		Version:      "1.0",
		LicenseID:    generateTrialLicenseID(),
		Type:         utility.TRIAL,
		IssuedAt:     now,
		ValidFrom:    now,
		ValidUntil:   now.Add(duration),
		MaxNodes:     1,
		CustomerName: "trial",
	}

	selectedInfos := clusterInfo.SelectClusterInfos()
	for _, info := range selectedInfos.SelectedSystemInfos {
		license.Machines = append(license.Machines, &utility.MachineInfo{
			MachineID:   info.MachineID,
			BoardSerial: info.BoardSerial,
			DiskSerial:  info.DiskSerial,
			MACAddress:  info.MACAddress,
		})
	}

	machineInfoJson, _ := json.Marshal(license.Machines)
	hash := md5.Sum(machineInfoJson)
	systemDigest := hex.EncodeToString(hash[:])

	license.NodeName = selectedInfos.NodeName
	license.MaxNodes = len(selectedInfos.SelectedSystemInfos)
	license.Digest = systemDigest

	return license, nil
}

// GetFingerprint to get fingerprint
func (s *Service) GetFingerprint() (string, error) {
	// Generate fingerprint
	clusterInfo := GlobalServerStatusStore.GetClusterInfo()
	if len(clusterInfo.SystemInfos) == 0 {
		return "", fmt.Errorf("fail to get cluster info, cluster isn't started fully")
	}

	encrypted, err := utility.EncryptHardwareInfo(&clusterInfo, utility.FingerprintPublicKey)
	if err != nil {
		common.Fatal("Failed to encrypt: ", zap.Error(err))
	}

	jsonData, err := json.MarshalIndent(encrypted, "", "  ")

	return string(jsonData), nil
}

// SetLicenseConfig set system license config
func (s *Service) SetLicenseConfig(timeRecordSaveInterval int64, timeRecordTaskDuration int64) error {
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

// SetLicense to set system license
func (s *Service) SetLicense(ctx context.Context, licenseStr string) (map[string]interface{}, error) {
	// Verify license
	license, result, err := extractLicense(licenseStr)
	if err != nil {
		common.Error("Error parsing license: ", err)
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

	SetMemLicense(license)
	err = InitFirstTimeRecord(ctx)
	if err != nil {
		common.Fatal("Error initializing first time record: %v", zap.Error(err))
	}
	common.Info("License is valid")
	StartUpdateRecordTask()
	return result, nil
}

func (s *Service) ShowLicense(ctx context.Context) (map[string]interface{}, error) {
	licenseStatus := GetLicenseStatus()
	if licenseStatus.CurrentLicense == nil {
		return nil, fmt.Errorf("license is not ready")
	}
	// read license from database
	license, err := s.licenseDAO.GetLatest(ctx, dao.DB)
	if err != nil {
		common.Warn("Error reading license: ", zap.Error(err))
		return nil, err
	}

	_, result, err := extractLicense(license.License)
	if err != nil {
		common.Error("Error parsing license: ", err)
		return nil, err
	}

	return result, nil
}

func (s *Service) CheckLicense(ctx context.Context) (common.ErrorCode, error) {
	licenseStatus := GetLicenseStatus()
	var licenseCheckResult string
	if licenseStatus.CurrentLicense == nil {
		if licenseStatus.Code != common.CodeLicenseValid {
			return licenseStatus.Code, errors.New(licenseStatus.Code.Message())
		}

		licenseCheckResult = "License is not ready"
		return licenseStatus.Code, errors.New(licenseCheckResult)
	}

	currentLicense := licenseStatus.CurrentLicense
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

	licenseCheckResult = "License valid"

	return common.CodeLicenseValid, errors.New(licenseCheckResult)
}

func extractLicense(licenseStr string) (*utility.License, map[string]interface{}, error) {
	license, err := parseLicenseFromStr(licenseStr)
	if err != nil {
		common.Error("Error parsing license: ", err)
		return nil, nil, err
	}

	common.Debug(fmt.Sprintf("License ID:     %s\n", license.LicenseID))
	common.Debug(fmt.Sprintf("Version:     %s\n", license.Version))
	common.Debug(fmt.Sprintf("License to:     %s\n", license.CustomerName))
	common.Debug(fmt.Sprintf("Issued at:     %s\n", formatLicenseTime(license.IssuedAt)))
	common.Debug(fmt.Sprintf("Valid from:     %s\n", formatLicenseTime(license.ValidFrom)))
	common.Debug(fmt.Sprintf("Valid until:     %s\n", formatLicenseTime(license.ValidUntil)))

	result := map[string]interface{}{}
	result["ID"] = license.LicenseID
	result["Version"] = license.Version
	result["CustomerName"] = license.CustomerName
	result["IssuedAt"] = formatLicenseTime(license.IssuedAt)
	result["ValidFrom"] = formatLicenseTime(license.ValidFrom)
	result["ValidUntil"] = formatLicenseTime(license.ValidUntil)

	return license, result, nil
	//return license.LicenseID, result, license.Digest, nil
}

func parseLicenseFromStr(licenseStr string) (*utility.License, error) {
	// Decode base64
	jsonData, err := base64.StdEncoding.DecodeString(licenseStr)
	if err != nil {
		common.Error("Error decoding base64: ", err)
		return nil, err
	}

	// Parse JSON package
	var pkg utility.LicensePackage
	err = json.Unmarshal(jsonData, &pkg)
	if err != nil {
		common.Error("Error parsing license JSON: ", err)
		return nil, err
	}

	var license *utility.License
	license, err = utility.DecryptLicense(&pkg, utility.LicensePrivateKey)
	if err != nil {
		common.Error("\"Error decrypting license: ", err)
		return nil, err
	}

	return license, nil
}

func ValidateLicenseDigest(licenseDigest string) error {
	// Get system info and generate digest
	clusterInfo := GlobalServerStatusStore.GetClusterInfo()
	selectedClusterInfo := clusterInfo.SelectClusterInfos()
	machineInfo := []*utility.MachineInfo{}
	for _, selected := range selectedClusterInfo.SelectedSystemInfos {
		machineInfo = append(machineInfo, &utility.MachineInfo{
			BoardSerial: selected.BoardSerial,
			DiskSerial:  selected.DiskSerial,
			MACAddress:  selected.MACAddress,
			MachineID:   selected.MachineID,
		})
	}
	if len(machineInfo) == 0 {
		return errors.New("no system info found, check if all RAGFlow components are fully started")
	}
	machineInfoJson, _ := json.Marshal(machineInfo)
	hash := md5.Sum(machineInfoJson)
	systemDigest := hex.EncodeToString(hash[:])
	if licenseDigest != systemDigest {
		return errors.New("license digest mismatch")
	}
	return nil
}

// SetMemLicense LicenseStatus related function
func SetMemLicense(license *utility.License) utility.LicenseStatus {
	licenseStatus := utility.LicenseStatus{
		CurrentLicense: license,
		Code:           common.CodeLicenseValid,
	}
	GlobalServerStatusStore.SetLicenseStatus(licenseStatus)
	return licenseStatus
}

func GetLicenseStatus() utility.LicenseStatus {
	return GlobalServerStatusStore.GetLicenseStatus()
}

func SetLicenseStatus(errorCode common.ErrorCode) {
	licenseStatus := GetLicenseStatus()
	GlobalServerStatusStore.SetLicenseStatus(utility.LicenseStatus{
		CurrentLicense: licenseStatus.CurrentLicense,
		Code:           errorCode,
	})
}

func CheckLicenseValidity(licenseStatus *utility.LicenseStatus) common.ErrorCode {

	if licenseStatus.Code != common.CodeLicenseValid {
		return licenseStatus.Code
	}

	if licenseStatus.CurrentLicense == nil {
		return common.CodeLicenseNotFound
	}

	now := time.Now()
	// if current time is not in the valid time range, return error
	if licenseStatus.CurrentLicense.ValidFrom.After(now) {
		return common.CodeLicenseInactiveError
	}

	if licenseStatus.CurrentLicense.ValidUntil.Before(now) {
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
	var lastCheckCount int = 0

	if lastData != nil {
		lastCheckCount = lastData.CheckCount
	}

	digest := GlobalServerStatusStore.GetLicenseStatus().CurrentLicense.Digest

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
		common.Fatal(fmt.Sprintf("Fail to get last time record: %v", err))
		return err
	}

	currentLicense := GetLicenseStatus().CurrentLicense
	if lastData.Nonce != currentLicense.Digest {
		SetLicenseStatus(common.CodeLicenseExpiredError)
		//common.Warn("Digest error: license expired")
		return errors.New("digest error: license expired")
	}

	nowTS := time.Now().Unix()
	now := time.Now()
	if currentLicense.ValidFrom.After(now) {
		return errors.New("license not valid yet")
	}

	if currentLicense.ValidUntil.Before(now) {
		return errors.New("license expired")
	}
	// 2. check time rollback
	lastTimestamp := lastData.Timestamp
	if nowTS < lastTimestamp {
		// convert following timestamp to time.Time
		lastTime := time.Unix(lastTimestamp, 0)
		currentTime := time.Unix(nowTS, 0)
		panic(fmt.Sprintf("Detected time recall: last time=%s, current=%s", lastTime.Format("2006-01-02 15:04:05"), currentTime.Format("2006-01-02 15:04:05")))
	}

	// if time gap is not larger than interval
	if nowTS-lastTimestamp < int64(interval.Seconds()) {
		common.Debug(fmt.Sprintf("Not reach the time limit %g hour", interval.Hours()))
		return nil
	}

	err = SaveTimeRecord(ctx, lastData)
	return err
}
