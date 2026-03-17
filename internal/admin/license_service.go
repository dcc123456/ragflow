package admin

import (
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
	"ragflow/internal/logger"
	"ragflow/internal/model"
	"ragflow/internal/utility"
	"strconv"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

func InitLicense() error {

	var license *utility.License = nil
	// Check if license can be read from database
	licenseDAO := dao.NewLicenseDAO()
	licenseRecord, err := licenseDAO.GetLatest()
	if err != nil && errors.Is(err, gorm.ErrRecordNotFound) {
		collector := utility.NewFingerprintCollector(true)
		clusterInfo, err2 := collector.Collect()
		if err2 != nil {
			logger.Fatal(fmt.Sprintf("Failed to collect hardware info: %v", err))
			return err2
		}

		trialLicenseLimit := os.Getenv("TRIAL_LICENSE_LIMIT")
		if trialLicenseLimit == "" {
			trialLicenseLimit = "10080" // 7 * 24 * 60 minutes
		}

		var trialLicenseLimitInt int
		trialLicenseLimitInt, err = strconv.Atoi(trialLicenseLimit)
		if err != nil {
			logger.Fatal(fmt.Sprintf("Error parsing trial license limit: %v", err))
			return err
		}

		if trialLicenseLimitInt <= 0 || trialLicenseLimitInt > 30*24*60 {
			logger.Fatal("Invalid trial license limit")
			return errors.New("Invalid trial license limit")
		}

		duration := time.Duration(trialLicenseLimitInt) * time.Minute
		license, err2 = generateTrialLicense(duration, clusterInfo)
		if err2 != nil {
			logger.Fatal(fmt.Sprintf("Error generating trial license: %v", err))
			return err2
		}

		var encryptedData string
		encryptedData, err2 = utility.GenerateSimpleStringLicense(license)
		if err2 != nil {
			logger.Fatal(fmt.Sprintf("Error generating license: %v", err))
			return err2
		}

		err2 = licenseDAO.Create(license.LicenseID, encryptedData)
		if err2 != nil {
			logger.Fatal(fmt.Sprintf("Error storing license: %v", err))
			return err2
		}
	} else {
		license, err = parseLicenseFromStr(licenseRecord.License)
		if err != nil {
			logger.Fatal(fmt.Sprintf("Error parsing license: %v", err))
			return err
		}
	}

	// Set to global status
	SetMemLicense(license)
	err = InitFirstTimeRecord()
	if err != nil {
		logger.Fatal("Error initializing first time record: %v", zap.Error(err))
	}
	logger.Info("License is valid")
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
		return nil, errors.New("Duration must be between 1 minute to 30 days")
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
	encrypted, err := utility.EncryptHardwareInfo(&clusterInfo, utility.FingerprintPublicKey)
	if err != nil {
		logger.Fatal("Failed to encrypt: ", zap.Error(err))
	}

	jsonData, err := json.MarshalIndent(encrypted, "", "  ")

	return string(jsonData), nil
}

// SetLicense to set system license config
func (s *Service) SetLicenseConfig(timeRecordSaveInterval int64, timeRecordTaskDuration int64) error {
	if !(timeRecordSaveInterval >= 3 && timeRecordSaveInterval <= 86400) {
		return errors.New("Value1 value range:  [3, 86400]")
	}
	if !(timeRecordTaskDuration > 0 && timeRecordTaskDuration <= 3600) {
		return errors.New("Value2 value range: (0, 3600]")
	}
	TimeRecordSaveInterval = timeRecordSaveInterval
	TimeRecordTaskDuration = timeRecordTaskDuration
	StopUpdateRecordTask()
	StartUpdateRecordTask()
	return nil
}

// SetLicense to set system license
func (s *Service) SetLicense(licenseStr string) (map[string]interface{}, error) {
	// Verify license
	license, result, err := extractLicense(licenseStr)
	if err != nil {
		logger.Error("Error parsing license: ", err)
		return nil, err
	}

	err = ValidateLicenseDigest(license.Digest)
	if err != nil {
		logger.Error("Error validating license digest: ", err)
		return nil, err
	}

	err = s.timeRecordDAO.DeleteAll()
	if err != nil {
		logger.Warn("Error deleting time records: ", zap.Error(err))
		return nil, err
	}

	// Store license to database
	err = s.licenseDAO.Create(license.LicenseID, licenseStr)
	if err != nil {
		logger.Warn("Error storing license: ", zap.Error(err))
		return nil, err
	}

	SetMemLicense(license)
	err = InitFirstTimeRecord()
	if err != nil {
		logger.Fatal("Error initializing first time record: %v", zap.Error(err))
	}
	logger.Info("License is valid")
	StartUpdateRecordTask()
	return result, nil
}

func (s *Service) ShowLicense() (map[string]interface{}, error) {
	licenseStatus := GetLicenseStatus()
	if licenseStatus.CurrentLicense == nil {
		return nil, fmt.Errorf("License is not ready")
	}
	// read license from database
	license, err := s.licenseDAO.GetLatest()
	if err != nil {
		logger.Warn("Error reading license: ", zap.Error(err))
		return nil, err
	}

	_, result, err := extractLicense(license.License)
	if err != nil {
		logger.Error("Error parsing license: ", err)
		return nil, err
	}

	return result, nil
}

func (s *Service) CheckLicense() error {
	licenseStatus := GetLicenseStatus()
	var licenseCheckResult string
	if licenseStatus.CurrentLicense == nil {
		if licenseStatus.Code != common.CodeLicenseValid {
			return errors.New(licenseStatus.Code.Message())
		}

		licenseCheckResult = "License is not ready"
		return fmt.Errorf(licenseCheckResult)
	}

	currentLicense := licenseStatus.CurrentLicense
	lastRecord, err := GetLastTimeRecord()
	if err != nil {
		logger.Warn(fmt.Sprintf("Fail to get last time record: %v", err))
		return err
	}

	if lastRecord.Nonce != currentLicense.Digest {
		return fmt.Errorf("Time record digest is mismatched")
	}

	nowTS := time.Now().Unix()
	now := time.Now()
	if currentLicense.ValidFrom.After(now) {
		licenseCheckResult = fmt.Sprintf("License not valid yet")
		return fmt.Errorf(licenseCheckResult)
	}

	if currentLicense.ValidUntil.Before(now) {
		licenseCheckResult = fmt.Sprintf("License expired")
		return fmt.Errorf(licenseCheckResult)
	}
	// 2. check time rollback
	lastTimestamp := lastRecord.Timestamp
	if nowTS < lastTimestamp {
		// convert following timestamp to time.Time
		return fmt.Errorf("Time rollback detected")
	}

	licenseCheckResult = "License valid"

	return fmt.Errorf(licenseCheckResult)
}

func extractLicense(licenseStr string) (*utility.License, map[string]interface{}, error) {
	license, err := parseLicenseFromStr(licenseStr)
	if err != nil {
		logger.Error("Error parsing license: ", err)
		return nil, nil, err
	}

	logger.Debug(fmt.Sprintf("License ID:     %s\n", license.LicenseID))
	logger.Debug(fmt.Sprintf("Version:     %s\n", license.Version))
	logger.Debug(fmt.Sprintf("License to:     %s\n", license.CustomerName))
	logger.Debug(fmt.Sprintf("Issued at:     %s\n", utility.FormatTime(license.IssuedAt)))
	logger.Debug(fmt.Sprintf("Valid from:     %s\n", utility.FormatTime(license.ValidFrom)))
	logger.Debug(fmt.Sprintf("Valid until:     %s\n", utility.FormatTime(license.ValidUntil)))

	result := map[string]interface{}{}
	result["ID"] = license.LicenseID
	result["Version"] = license.Version
	result["CustomerName"] = license.CustomerName
	result["IssuedAt"] = utility.FormatTime(license.IssuedAt)
	result["ValidFrom"] = utility.FormatTime(license.ValidFrom)
	result["ValidUntil"] = utility.FormatTime(license.ValidUntil)

	return license, result, nil
	//return license.LicenseID, result, license.Digest, nil
}

func parseLicenseFromStr(licenseStr string) (*utility.License, error) {
	// Decode base64
	jsonData, err := base64.StdEncoding.DecodeString(licenseStr)
	if err != nil {
		logger.Error("Error decoding base64: ", err)
		return nil, err
	}

	// Parse JSON package
	var pkg utility.LicensePackage
	err = json.Unmarshal(jsonData, &pkg)
	if err != nil {
		logger.Error("Error parsing license JSON: ", err)
		return nil, err
	}

	var license *utility.License
	license, err = utility.DecryptLicense(&pkg, utility.LicensePrivateKey)
	if err != nil {
		logger.Error("\"Error decrypting license: ", err)
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
	machineInfoJson, _ := json.Marshal(machineInfo)
	hash := md5.Sum(machineInfoJson)
	systemDigest := hex.EncodeToString(hash[:])
	if licenseDigest != systemDigest {
		return errors.New("License digest mismatch")
	}
	return nil
}

// LicenseStatus related function
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

func SaveTimeRecord(lastData *TimeRecord) error {
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
		return fmt.Errorf("Fail to encrypt: %v", err)
	}

	// 5. insert new record
	var timeRecordDao *dao.TimeRecordDAO
	timeRecordDao = dao.NewTimeRecordDAO()
	err = timeRecordDao.Create(&model.TimeRecord{
		Data:      encrypted,
		CreatedAt: time.Now(),
	})
	if err != nil {
		return err
	}
	return err
}

// GetLastTime get last time record
func GetLastTimeRecord() (*TimeRecord, error) {
	var timeRecordDao *dao.TimeRecordDAO
	timeRecordDao = dao.NewTimeRecordDAO()
	timeRecords, err := timeRecordDao.GetRecent(1)
	if err != nil {
		return nil, err
	}

	if len(timeRecords) == 0 {
		return nil, sql.ErrNoRows
	}

	timeRecord := timeRecords[0]

	return DecryptTimeRecord(timeRecord.Data)
}

// GetAllTimes Get all time records
func GetAllTimeRecords() ([]*TimeRecord, error) {
	var timeRecordDao *dao.TimeRecordDAO
	timeRecordDao = dao.NewTimeRecordDAO()
	timeRecords, err := timeRecordDao.GetAll()
	if err != nil {
		return nil, err
	}

	var results = make([]*TimeRecord, 0)
	for _, timeRecord := range timeRecords {
		data, err := DecryptTimeRecord(timeRecord.Data)
		if err != nil {
			logger.Warn(fmt.Sprintf("Error decrypting time record: ", err))
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
func CleanOldRecords() error {
	var timeRecordDao *dao.TimeRecordDAO
	timeRecordDao = dao.NewTimeRecordDAO()
	err := timeRecordDao.DeleteOldest(365 * 24)
	if err != nil {
		return err
	}
	return nil
}

func InitFirstTimeRecord() error {
	_, err := GetLastTimeRecord()
	if errors.Is(err, sql.ErrNoRows) {
		err = SaveTimeRecord(nil)
		if err != nil {
			return err
		}
	}
	return nil
}

func StartTimeRecordService(interval time.Duration) error {
	logger.Debug("Try to update time record")
	// Get last time
	lastData, err := GetLastTimeRecord()
	if err != nil {
		logger.Fatal(fmt.Sprintf("Fail to get last time record: %v", err))
		return err
	}

	currentLicense := GetLicenseStatus().CurrentLicense
	if lastData.Nonce != currentLicense.Digest {
		SetLicenseStatus(common.CodeLicenseExpiredError)
		//logger.Warn("Digest error: license expired")
		return errors.New("Digest error: license expired")
	}

	nowTS := time.Now().Unix()
	now := time.Now()
	if currentLicense.ValidFrom.After(now) {
		return errors.New("License not valid yet")
	}

	if currentLicense.ValidUntil.Before(now) {
		return errors.New("License expired")
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
		logger.Debug(fmt.Sprintf("Not reach the time limit %d hour", interval.Hours()))
		return nil
	}

	err = SaveTimeRecord(lastData)
	return err
}
