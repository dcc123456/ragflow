package utility

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

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

type SystemInfo struct {
	Nodes       []string    `json:"nodes"`
	MachineID   string      `json:"machine_id"`
	BoardSerial string      `json:"board_serial"`
	CPU         CPUInfo     `json:"cpu"`
	Memory      MemoryInfo  `json:"memory"`
	Disk        DiskInfo    `json:"disks"`
	Network     NetworkInfo `json:"networks"`
}

// ClusterInfo contains all collected hardware information
type ClusterInfo struct {
	SystemInfos []*SystemInfo `json:"system_infos"`

	// K8s environment detection
	InK8s    bool   `json:"in_k8s"`
	NodeName string `json:"node_name,omitempty"`
	PodName  string `json:"pod_name,omitempty"`
}

// SelectedSystemInfo contains the primary identifiers used for fingerprinting
type SelectedSystemInfo struct {
	MachineID   string `json:"machine_id"`
	BoardSerial string `json:"board_serial"`
	DiskSerial  string `json:"disk_serial"`
	MACAddress  string `json:"mac_address"`
	NodeName    string `json:"node_name,omitempty"`
}

type SelectedClusterInfo struct {
	Version     string    `json:"version"`
	CollectedAt time.Time `json:"collected_at"`

	SelectedSystemInfos []*SelectedSystemInfo `json:"selected_system_infos"`

	// K8s environment detection
	InK8s    bool   `json:"in_k8s"`
	NodeName string `json:"node_name,omitempty"`
	PodName  string `json:"pod_name,omitempty"`
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

// Collect gathers all hardware information from the system
func (fc *FingerprintCollector) Collect() (*ClusterInfo, error) {
	clusterInfo := &ClusterInfo{}

	systemInfo := &SystemInfo{}
	// Collect all hardware information
	systemInfo.MachineID = fc.getMachineID()
	systemInfo.BoardSerial = fc.getBoardSerial()
	systemInfo.CPU = fc.getCPUInfo()
	systemInfo.Memory = fc.getMemoryInfo()
	systemInfo.Disk = fc.getDiskInfo()
	systemInfo.Network = fc.getNetworkInfo()

	clusterInfo.SystemInfos = append(clusterInfo.SystemInfos, systemInfo)

	// Detect K8s environment
	clusterInfo.InK8s = fc.isInK8s()
	if clusterInfo.InK8s {
		clusterInfo.NodeName = os.Getenv("NODE_NAME")
		clusterInfo.PodName = os.Getenv("POD_NAME")
	}

	return clusterInfo, nil
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

// SortSystemInfos sorts the SystemInfo array by MachineID, BoardSerial, CPU, Disk, MAC, and MemoryInfo
func SortSystemInfos(systemInfos []*SystemInfo) {
	sort.Slice(systemInfos, func(i, j int) bool {
		// Compare MachineID
		if systemInfos[i].MachineID != systemInfos[j].MachineID {
			return systemInfos[i].MachineID < systemInfos[j].MachineID
		}
		// Compare BoardSerial
		if systemInfos[i].BoardSerial != systemInfos[j].BoardSerial {
			return systemInfos[i].BoardSerial < systemInfos[j].BoardSerial
		}
		// Compare CPU ModelName
		if systemInfos[i].CPU.ModelName != systemInfos[j].CPU.ModelName {
			return systemInfos[i].CPU.ModelName < systemInfos[j].CPU.ModelName
		}
		// Compare Disk Serial
		if systemInfos[i].Disk.Serial != systemInfos[j].Disk.Serial {
			return systemInfos[i].Disk.Serial < systemInfos[j].Disk.Serial
		}
		// Compare Network MAC
		if systemInfos[i].Network.MAC != systemInfos[j].Network.MAC {
			return systemInfos[i].Network.MAC < systemInfos[j].Network.MAC
		}
		// Compare Memory TotalGB
		return systemInfos[i].Memory.TotalGB < systemInfos[j].Memory.TotalGB
	})
}

// SelectPrimaryIdentifiers chooses the most stable identifiers for fingerprinting
func (hi *SystemInfo) SelectPrimaryIdentifiers() *SelectedSystemInfo {
	selected := &SelectedSystemInfo{
		MachineID:   hi.MachineID,
		BoardSerial: hi.BoardSerial,
		DiskSerial:  hi.Disk.Serial,
		MACAddress:  hi.Network.MAC,
	}

	return selected
}

// SelectClusterInfos chooses the most stable identifiers for fingerprinting
func (hi *ClusterInfo) SelectClusterInfos() SelectedClusterInfo {
	selected := SelectedClusterInfo{}

	for _, system := range hi.SystemInfos {
		selectedPrimaryIdentifier := system.SelectPrimaryIdentifiers()
		selected.SelectedSystemInfos = append(selected.SelectedSystemInfos, selectedPrimaryIdentifier)
	}
	return selected
}

// ==================== Hybrid Encryption Functions ====================

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

// EncryptHardwareInfo encrypts hardware info using hybrid encryption (AES + RSA)
func EncryptHardwareInfo(info *ClusterInfo, pubKey *rsa.PublicKey) (*EncryptedFingerprint, error) {
	// 1. Convert hardware info to JSON
	jsonData, err := json.Marshal(info)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal hardware info: %v", err)
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
func DecryptFingerprint(ef *EncryptedFingerprint, privKey *rsa.PrivateKey) (*ClusterInfo, error) {
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
	aesKey, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, privKey, encryptedKey, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to RSA decrypt AES key: %v", err)
	}

	// 3. Decrypt the data with AES
	jsonData, err := decryptAESGCM(aesKey, encryptedData, nonce)
	if err != nil {
		return nil, fmt.Errorf("failed to AES decrypt: %v", err)
	}

	// 4. Unmarshal JSON
	var info ClusterInfo
	if err := json.Unmarshal(jsonData, &info); err != nil {
		return nil, fmt.Errorf("failed to unmarshal hardware info: %v", err)
	}

	return &info, nil
}

// ==================== Key Management ====================

// LoadPublicKey loads an RSA public key from a PEM file
func LoadPublicKey(filename string) (*rsa.PublicKey, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %v", err)
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err == nil {
		rsaPub, ok := pub.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("key is not RSA public key")
		}
		return rsaPub, nil
	}

	rsaPub, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err == nil {
		return rsaPub, nil
	}

	return nil, fmt.Errorf("failed to parse public key: %v", err)
}

// LoadPrivateKey loads an RSA private key from a PEM file
func LoadPrivateKey(path string) (*rsa.PrivateKey, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %v", err)
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	switch block.Type {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)

	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse PKCS#8 private key: %v", err)
		}
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("key is not RSA private key")
		}
		return rsaKey, nil

	default:
		return nil, fmt.Errorf("unsupported key type: %s", block.Type)
	}
}
