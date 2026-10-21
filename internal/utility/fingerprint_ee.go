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

package utility

import (
	"bufio"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"ragflow/internal/common"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type ClusterInfo struct {
}

// EncryptedFingerprint contains the hybrid-encrypted hardware information
type EncryptedFingerprint struct {
	Version   string `json:"version"`   // Format version
	Timestamp int64  `json:"timestamp"` // Generation time

	// Encrypted AES key (using RSA public key)
	EncryptedKey string `json:"encrypted_key"` // Base64 encoded

	// AES encrypted data
	EncryptedData string `json:"encrypted_data"` // Base64 encoded

	// AES nonce/IV (needed for decryption)
	Nonce string `json:"nonce"` // Base64 encoded
}

// DiskInfo represents a physical disk's information
type DiskInfo struct {
	Path   string `json:"path"`
	Serial string `json:"serial"`
	Model  string `json:"model"`
}

// NetworkInfo represents a network interface
type NetworkInfo struct {
	Name string `json:"name"`
	MAC  string `json:"mac"`
}

// CPUInfo represents CPU information (NEW)
type CPUInfo struct {
	ModelName string `json:"model_name"` // e.g., "Intel(R) Core(TM) i7-10750H CPU @ 2.60GHz"
}

// MemoryInfo represents memory information (NEW)
type MemoryInfo struct {
	TotalGB float64 `json:"total_gb"` // Total RAM in GB
}

// ==================== Collector Implementation ====================

// FingerprintCollector configures the fingerprint collection process
type FingerprintCollector struct {
	UseStableOrder bool          // Sort devices for consistent ordering
	Timeout        time.Duration // Collection timeout
}

// NewFingerprintCollector creates a new fingerprint collector
func NewFingerprintCollector(useStableOrder bool) *FingerprintCollector {
	return &FingerprintCollector{
		UseStableOrder: useStableOrder,
		Timeout:        30 * time.Second,
	}
}

func (fc *FingerprintCollector) Collect() (*ClusterInfo, error) {
	return nil, nil
}

// getMachineID reads the machine-id from standard Linux locations
func (fc *FingerprintCollector) getMachineID() string {
	paths := []string{
		"/etc/machine-id",
		"/var/lib/dbus/machine-id",
	}

	if fc.isInK8s() {
		paths = append([]string{
			"/host/etc/machine-id",
			"/host/var/lib/dbus/machine-id",
		}, paths...)
	}

	for _, path := range paths {
		data, err := ioutil.ReadFile(path)
		if err != nil {
			continue
		}
		id := strings.TrimSpace(string(data))
		if id != "" && len(id) >= 32 {
			return id
		}
	}

	return ""
}

// getBoardSerial reads the motherboard serial number
func (fc *FingerprintCollector) getBoardSerial() string {
	dmiPaths := []string{
		"/sys/class/dmi/id/board_serial",
		"/sys/class/dmi/id/product_serial",
	}

	if fc.isInK8s() {
		dmiPaths = append([]string{
			"/host/sys/class/dmi/id/board_serial",
			"/host/sys/class/dmi/id/product_serial",
		}, dmiPaths...)
	}

	for _, path := range dmiPaths {
		data, err := ioutil.ReadFile(path)
		if err != nil {
			continue
		}
		serial := strings.TrimSpace(string(data))
		if fc.isValidBoardSerial(serial) {
			return serial
		}
	}

	return ""
}

// isValidBoardSerial checks if the board serial is valid
func (fc *FingerprintCollector) isValidBoardSerial(serial string) bool {
	invalid := []string{
		"",
		"To be filled by O.E.M.",
		"Default string",
		"None",
		"Not Available",
		"00000000",
		"123456789",
		"0",
	}
	for _, v := range invalid {
		if serial == v {
			return false
		}
	}
	return true
}

// getCPUInfo collects CPU information from /proc/cpuinfo (NEW)
func (fc *FingerprintCollector) getCPUInfo() CPUInfo {
	cpuInfo := CPUInfo{}

	data, err := ioutil.ReadFile("/proc/cpuinfo")
	if err != nil {
		return cpuInfo
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))

	for scanner.Scan() {
		line := scanner.Text()

		if strings.Contains(line, "model name") {
			parts := strings.Split(line, ":")
			if len(parts) == 2 {
				cpuInfo.ModelName = strings.TrimSpace(parts[1])
			}
		}
	}
	return cpuInfo
}

// getMemoryInfo collects memory information from /proc/meminfo (NEW)
func (fc *FingerprintCollector) getMemoryInfo() MemoryInfo {
	memInfo := MemoryInfo{}

	data, err := ioutil.ReadFile("/proc/meminfo")
	if err != nil {
		return memInfo
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				// Value is in KB
				kb, _ := strconv.ParseUint(fields[1], 10, 64)
				memInfo.TotalGB = float64(kb) / (1024 * 1024)
				// Round to 2 decimal places
				memInfo.TotalGB = float64(int(memInfo.TotalGB*100)) / 100
			}
			break
		}
	}

	return memInfo
}

// getDiskInfo collects information about first physical disks
func (fc *FingerprintCollector) getDiskInfo() DiskInfo {
	var disks []DiskInfo

	blockDevices, err := ioutil.ReadDir("/sys/block")
	if err != nil {
		return DiskInfo{}
	}

	for _, device := range blockDevices {
		devName := device.Name()

		if strings.HasPrefix(devName, "loop") ||
			strings.HasPrefix(devName, "ram") ||
			strings.HasPrefix(devName, "sr") {
			continue
		}

		disk := DiskInfo{
			Path: "/dev/" + devName,
		}

		modelPath := fmt.Sprintf("/sys/block/%s/device/model", devName)
		if modelData, err := ioutil.ReadFile(modelPath); err == nil {
			disk.Model = strings.TrimSpace(string(modelData))
		}

		serialPath := fmt.Sprintf("/sys/block/%s/device/serial", devName)
		if serialData, err := ioutil.ReadFile(serialPath); err == nil {
			disk.Serial = strings.TrimSpace(string(serialData))
		}

		disks = append(disks, disk)
	}

	if fc.UseStableOrder {
		sort.Slice(disks, func(i, j int) bool {
			return disks[i].Path < disks[j].Path
		})
	}

	if len(disks) == 0 {
		return DiskInfo{}
	}

	return disks[0]
}

// getNetworkInfo collects network interface information
func (fc *FingerprintCollector) getNetworkInfo() NetworkInfo {

	var networks []NetworkInfo
	interfaces, err := net.Interfaces()
	if err != nil {
		return NetworkInfo{}
	}

	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		isPhysical := fc.isPhysicalInterface(iface.Name)
		if !isPhysical {
			continue
		}

		netInfo := NetworkInfo{
			Name: iface.Name,
			MAC:  iface.HardwareAddr.String(),
		}

		networks = append(networks, netInfo)
	}

	if fc.UseStableOrder {
		sort.Slice(networks, func(i, j int) bool {
			return networks[i].Name < networks[j].Name
		})
	}

	if len(networks) == 0 {
		return NetworkInfo{}
	}

	return networks[0]
}

// isPhysicalInterface checks if a network interface is physical
func (fc *FingerprintCollector) isPhysicalInterface(name string) bool {
	virtualPrefixes := []string{
		"docker", "veth", "br-", "virbr", "lo",
		"tun", "tap", "vnet", "macvtap", "vlan",
		"bond", "team", "dummy", "gre", "gretap",
		"erspan", "ip6tnl", "ip6gre", "ip6gretap",
		"vti", "vti6", "nlmon", "ipoib", "can",
	}

	for _, prefix := range virtualPrefixes {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}

	devPath := fmt.Sprintf("/sys/class/net/%s/device", name)
	if _, err := os.Stat(devPath); os.IsNotExist(err) {
		return false
	}

	return true
}

// isInK8s detects if running inside Kubernetes
func (fc *FingerprintCollector) isInK8s() bool {
	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		return true
	}

	if _, err := os.Stat("/var/run/secrets/kubernetes.io"); err == nil {
		return true
	}

	hostname, _ := os.Hostname()
	if strings.Contains(hostname, "-deployment-") ||
		strings.Contains(hostname, "-statefulset-") {
		return true
	}

	return false
}

// generateAESKey creates a new random AES-256 key (32 bytes)
func generateAESKey() ([]byte, error) {
	key := make([]byte, 32) // AES-256
	_, err := rand.Read(key)
	if err != nil {
		return nil, fmt.Errorf("failed to generate AES key: %v", err)
	}
	return key, nil
}

// generateNonce creates a random nonce for GCM (12 bytes is recommended)
func generateNonce() ([]byte, error) {
	nonce := make([]byte, 12) // 96 bits is standard for GCM
	_, err := rand.Read(nonce)
	if err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %v", err)
	}
	return nonce, nil
}

// encryptAESGCM encrypts data using AES-256-GCM
func encryptAESGCM(key, plaintext, nonce []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %v", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %v", err)
	}

	// GCM includes authentication tag in the ciphertext
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	return ciphertext, nil
}

// decryptAESGCM decrypts data using AES-256-GCM
func decryptAESGCM(key, ciphertext, nonce []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %v", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %v", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt AES: %v", err)
	}

	return plaintext, nil
}

type Fingerprint struct {
	Type      string    `json:"type"`
	K8s       K8s       `json:"k8s"`
	BareMetal BareMetal `json:"baremetal"`
}

type K8s struct {
	Uuid string `json:"uuid"`
}

type BareMetal struct {
	CPUCount    int         `json:"cpu_count"`
	CPUInfo     CPUInfo     `json:"cpu0"`
	MemoryInfo  MemoryInfo  `json:"memory"`
	DiskInfo    DiskInfo    `json:"disk0"`
	NetworkInfo NetworkInfo `json:"network0"`
	MachineID   string      `json:"machine_id"`
	BoardSerial string      `json:"board_serial"`
}

func (f *Fingerprint) PrintAll() {
	fmt.Println("Type:", f.Type)
	if f.Type == "k8s" {
		fmt.Println("K8s UUID:", f.K8s.Uuid)
	} else {
		fmt.Println("BareMetal CPU Count:", f.BareMetal.CPUCount)
		fmt.Println("BareMetal CPU Info:", f.BareMetal.CPUInfo)
		fmt.Println("BareMetal Memory Info:", f.BareMetal.MemoryInfo)
		fmt.Println("BareMetal Disk Info:", f.BareMetal.DiskInfo)
		fmt.Println("BareMetal Network Info:", f.BareMetal.NetworkInfo)
		fmt.Println("BareMetal Machine ID:", f.BareMetal.MachineID)
		fmt.Println("BareMetal Board Serial:", f.BareMetal.BoardSerial)
	}
}

// CollectV2 gathers all hardware information from the system
func (fc *FingerprintCollector) CollectV2() (*Fingerprint, error) {
	// Check if current info is K8S

	fingerprint := Fingerprint{}
	var err error

	if fc.isInK8s() {
		fingerprint.Type = "k8s"
		fingerprint.K8s.Uuid, err = fc.getK8sUUID()
		if err != nil {
			return nil, err
		}
	} else {
		fingerprint.Type = "baremetal"
		fingerprint.BareMetal.CPUCount = runtime.NumCPU()
		fingerprint.BareMetal.CPUInfo = fc.getCPUInfo()
		fingerprint.BareMetal.MemoryInfo = fc.getMemoryInfo()
		fingerprint.BareMetal.DiskInfo = fc.getDiskInfo()
		fingerprint.BareMetal.NetworkInfo = fc.getNetworkInfo()
		fingerprint.BareMetal.MachineID = fc.getMachineID()
		fingerprint.BareMetal.BoardSerial = fc.getBoardSerial()
	}

	return &fingerprint, nil
}

// EncryptFingerprint encrypts fingerprint using hybrid encryption (AES + RSA)
func EncryptFingerprint(info *Fingerprint, pubKey *rsa.PublicKey) (*EncryptedFingerprint, error) {
	// 1. Convert fingerprint to JSON
	jsonData, err := json.Marshal(info)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal fingerprint: %v", err)
	}

	fmt.Printf("JSON data size: %d bytes\n", len(jsonData))

	// 2. Generate AES key and nonce
	aesKey, err := generateAESKey()
	if err != nil {
		return nil, err
	}

	nonce, err := generateNonce()
	if err != nil {
		return nil, err
	}

	// 3. Encrypt the data with AES
	encryptedData, err := encryptAESGCM(aesKey, jsonData, nonce)
	if err != nil {
		return nil, fmt.Errorf("failed to AES encrypt: %v", err)
	}

	// 4. Encrypt the AES key with RSA
	encryptedKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, pubKey, aesKey, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to RSA encrypt AES key: %v", err)
	}

	// 5. Create encrypted fingerprint
	ef := &EncryptedFingerprint{
		Version:       "1.0",
		Timestamp:     time.Now().Unix(),
		EncryptedKey:  base64.StdEncoding.EncodeToString(encryptedKey),
		EncryptedData: base64.StdEncoding.EncodeToString(encryptedData),
		Nonce:         base64.StdEncoding.EncodeToString(nonce),
	}

	return ef, nil
}

// DecryptFingerprint decrypts an encrypted fingerprint using hybrid decryption
func DecryptFingerprint(ef *EncryptedFingerprint, privateKey *rsa.PrivateKey) (*Fingerprint, error) {
	// 1. Decode base64 fields
	encryptedKey, err := base64.StdEncoding.DecodeString(ef.EncryptedKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode encrypted key: %v", err)
	}

	encryptedData, err := base64.StdEncoding.DecodeString(ef.EncryptedData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode encrypted data: %v", err)
	}

	nonce, err := base64.StdEncoding.DecodeString(ef.Nonce)
	if err != nil {
		return nil, fmt.Errorf("failed to decode nonce: %v", err)
	}

	// 2. Decrypt the AES key with RSA
	aesKey, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, privateKey, encryptedKey, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to RSA decrypt AES key: %v", err)
	}

	// 3. Decrypt the data with AES
	jsonData, err := decryptAESGCM(aesKey, encryptedData, nonce)
	if err != nil {
		return nil, fmt.Errorf("failed to AES decrypt: %v", err)
	}

	// 4. Unmarshal JSON
	var fingerprint Fingerprint
	if err = json.Unmarshal(jsonData, &fingerprint); err != nil {
		return nil, fmt.Errorf("failed to unmarshal fingerprint: %v", err)
	}

	return &fingerprint, nil
}

func (fc *FingerprintCollector) getK8sUUID() (string, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		common.Fatal(fmt.Sprintf("can't create in-cluster config: %v", err))
		return "", err
	}

	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		common.Fatal(fmt.Sprintf("can't create client set: %v", err))
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	kubeSystem, err := clientSet.CoreV1().Namespaces().Get(ctx, "kube-system", metav1.GetOptions{})
	if err != nil {
		common.Fatal(fmt.Sprintf("can't get kube-system namespace: %v", err))
		return "", err
	}

	clusterUID := string(kubeSystem.UID)
	return clusterUID, nil
}

type CPUServerStatus struct {
	CPUModel string  `json:"cpu_model"`
	CoreNum  int     `json:"core_num"`
	Memory   float64 `json:"memory"`
}

func (c *CPUServerStatus) PrintAll() string {
	return fmt.Sprintf("CPUModel: %s, CoreNum: %d, Memory: %.2f GB", c.CPUModel, c.CoreNum, c.Memory)
}

func (fc *FingerprintCollector) NewCPUServerStatus() (*CPUServerStatus, error) {
	coreNum := runtime.NumCPU()
	cpuInfo := fc.getCPUInfo()
	memoryInfo := fc.getMemoryInfo()

	return &CPUServerStatus{
		CPUModel: cpuInfo.ModelName,
		CoreNum:  coreNum,
		Memory:   memoryInfo.TotalGB,
	}, nil
}

type GPUServerStatus struct {
	CPUModel string  `json:"cpu_model"`
	CoreNum  int     `json:"core_num"`
	GPUModel string  `json:"gpu_model"`
	GPUNum   int     `json:"gpu_num"`
	Memory   float64 `json:"memory"`
}

func (g *GPUServerStatus) PrintAll() string {
	return fmt.Sprintf("CPUModel: %s, CoreNum: %d, GPUModel: %s, GPUNum: %d, Memory: %.2f GB", g.CPUModel, g.CoreNum, g.GPUModel, g.GPUNum, g.Memory)
}

func (fc *FingerprintCollector) NewGPUServerStatus() (*GPUServerStatus, error) {
	coreNum := runtime.NumCPU()
	cpuInfo := fc.getCPUInfo()
	memoryInfo := fc.getMemoryInfo()

	return &GPUServerStatus{
		CPUModel: cpuInfo.ModelName,
		CoreNum:  coreNum,
		GPUModel: "unknown",
		GPUNum:   0,
		Memory:   memoryInfo.TotalGB,
	}, nil
}

type APIServerHeartbeatResponse struct {
	CPUNum         int      `json:"cpu_num"`
	MaxConcurrency int      `json:"max_concurrency"`
	Features       []string `json:"features"`
}

func (a *APIServerHeartbeatResponse) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"cpu_num":         a.CPUNum,
		"max_concurrency": a.MaxConcurrency,
		"features":        a.Features,
	}
}

type IngestorHeartbeatResponse struct {
	CPUNum    int      `json:"cpu_num"`
	WorkerNum int      `json:"worker_num"`
	Features  []string `json:"features"`
}

func (i *IngestorHeartbeatResponse) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"cpu_num":    i.CPUNum,
		"worker_num": i.WorkerNum,
		"features":   i.Features,
	}
}

type FileSyncerHeartbeatResponse struct {
	CPUNum    int      `json:"cpu_num"`
	WorkerNum int      `json:"worker_num"`
	Features  []string `json:"features"`
}

func (f *FileSyncerHeartbeatResponse) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"cpu_num":    f.CPUNum,
		"worker_num": f.WorkerNum,
		"features":   f.Features,
	}
}

type DeepDocCPUHeartbeatResponse struct {
	CPUNum         int      `json:"cpu_num"`
	MaxConcurrency int      `json:"max_concurrency"`
	WorkerNum      int      `json:"worker_num"`
	Features       []string `json:"features"`
}

func (d *DeepDocCPUHeartbeatResponse) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"cpu_num":         d.CPUNum,
		"max_concurrency": d.MaxConcurrency,
		"worker_num":      d.WorkerNum,
		"features":        d.Features,
	}
}

type DeepDocGPUHeartbeatResponse struct {
	CPUNum         int      `json:"cpu_num"`
	GPUNum         int      `json:"gpu_num"`
	MaxConcurrency int      `json:"max_concurrency"`
	WorkerNum      int      `json:"worker_num"`
	Features       []string `json:"features"`
}

func (d *DeepDocGPUHeartbeatResponse) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"cpu_num":         d.CPUNum,
		"gpu_num":         d.GPUNum,
		"max_concurrency": d.MaxConcurrency,
		"worker_num":      d.WorkerNum,
	}
}
