package utility

import (
	"crypto"
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
	"ragflow/internal/common"
	"time"
)

// LicenseType defines the type of license
type LicenseType string

const (
	TRIAL     LicenseType = "trial"
	Standard  LicenseType = "standard"
	Perpetual LicenseType = "perpetual"
	Cluster   LicenseType = "Cluster"
)

// LicenseBinding defines how the license is bound to hardware
type LicenseBinding string

const (
	BindToMachine LicenseBinding = "machine"
	NoBinding     LicenseBinding = "none"
)

type MachineInfo struct {
	MachineID   string `json:"machine_id,omitempty"`
	BoardSerial string `json:"board_serial,omitempty"`
	DiskSerial  string `json:"disk_serial,omitempty"`
	MACAddress  string `json:"mac_address,omitempty"`
}

// License represents a software license
type License struct {
	Version      string         `json:"version"`
	LicenseID    string         `json:"license_id"`
	Type         LicenseType    `json:"type"`
	IssuedAt     time.Time      `json:"issued_at"`
	ValidFrom    time.Time      `json:"valid_from"`
	ValidUntil   time.Time      `json:"valid_until"`
	Binding      LicenseBinding `json:"binding"`
	Machines     []*MachineInfo `json:"machines"`
	NodeName     string         `json:"node_name,omitempty"`
	MaxNodes     int            `json:"max_nodes"`
	CustomerName string         `json:"customer_name"`
	Digest       string         `json:"digest"`
}

// LicensePackage represents the encrypted license package
type LicensePackage struct {
	V  string `json:"v"`  // version
	K  string `json:"k"`  // encrypted AES key
	D  string `json:"d"`  // IV + encrypted data
	Ts int64  `json:"ts"` // timestamp
	S  string `json:"s"`  // signature
}

type LicenseStatus struct {
	CurrentLicense *License
	Code           common.ErrorCode
}

// LoadMachineFingerprint Load machine fingerprint from JSON file
func LoadMachineFingerprint(jsonFile string) (ClusterInfo, error) {
	data, err := ioutil.ReadFile(jsonFile)
	var info ClusterInfo
	if err != nil {
		return info, fmt.Errorf("failed to read fingerprint file: %v", err)
	}

	if err := json.Unmarshal(data, &info); err != nil {
		return info, fmt.Errorf("failed to parse fingerprint JSON: %v", err)
	}

	return info, nil
}

// DecryptAESGCM decrypts data using AES-256-GCM
func DecryptAESGCM(key, ciphertext, nonce []byte) ([]byte, error) {
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

// decryptLicense decrypts the license using the private key
func DecryptLicense(pkg *LicensePackage, privateKey *rsa.PrivateKey) (*License, error) {
	// 1. Decode base64 fields
	encryptedKey, err := base64.StdEncoding.DecodeString(pkg.K)
	if err != nil {
		return nil, fmt.Errorf("failed to decode encrypted key: %v", err)
	}

	encryptedData, err := base64.StdEncoding.DecodeString(pkg.D)
	if err != nil {
		return nil, fmt.Errorf("failed to decode encrypted data: %v", err)
	}

	// 2. Decrypt AES key with RSA (PKCS1v15)
	aesKey, err := rsa.DecryptPKCS1v15(nil, privateKey, encryptedKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt AES key: %v", err)
	}

	// 3. Extract IV (first 12 bytes) and ciphertext
	if len(encryptedData) < 12 {
		return nil, fmt.Errorf("encrypted data too short")
	}
	iv := encryptedData[:12]
	ciphertext := encryptedData[12:]

	// 4. Decrypt data with AES-GCM
	jsonData, err := decryptAESGCM(aesKey, ciphertext, iv)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt license data: %v", err)
	}

	// 5. Unmarshal JSON
	var license License
	if err := json.Unmarshal(jsonData, &license); err != nil {
		return nil, fmt.Errorf("failed to unmarshal license: %v", err)
	}

	return &license, nil
}

func GenerateSimpleStringLicense(license *License) (string, error) {
	// 1. Convert license to JSON
	licenseJSON, err := json.Marshal(license)
	if err != nil {
		return "", err
	}

	// 2. Generate AES key
	aesKey, err := generateAESKey()
	if err != nil {
		return "", err
	}

	// 3. Generate random IV
	iv := make([]byte, 12)
	_, err = rand.Read(iv)
	if err != nil {
		return "", err
	}

	// 4. Encrypt with AES
	block, _ := aes.NewCipher(aesKey)
	gcm, _ := cipher.NewGCM(block)
	encrypted := gcm.Seal(nil, iv, licenseJSON, nil)

	// 5. Combine IV + encrypted data
	combined := append(iv, encrypted...)

	// 6. Encrypt the AES key with RSA (optional, if you want end-to-end encryption)
	// For simplicity, we'll just include the AES key encrypted
	encryptedKey, err := rsa.EncryptPKCS1v15(rand.Reader, LicensePublicKey, aesKey)
	if err != nil {
		return "", err
	}

	// 7. Create final package
	packageData := map[string]interface{}{
		"v":  "1",                                             // version
		"k":  base64.StdEncoding.EncodeToString(encryptedKey), // encrypted AES key
		"d":  base64.StdEncoding.EncodeToString(combined),     // IV + encrypted data
		"ts": time.Now().Unix(),                               // timestamp
	}

	// 8. Sign the package
	packageJSON, _ := json.Marshal(packageData)
	hash := sha256.Sum256(packageJSON)
	sig, _ := rsa.SignPKCS1v15(rand.Reader, LicensePrivateKey, crypto.SHA256, hash[:])
	packageData["s"] = base64.StdEncoding.EncodeToString(sig)

	// 9. Final JSON and base64
	finalJSON, _ := json.Marshal(packageData)
	licenseString := base64.StdEncoding.EncodeToString(finalJSON)

	return licenseString, nil
}

var fingerprintPublicKeyBytes = []byte("-----BEGIN PUBLIC KEY-----\nMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEApvEySR0vYuAcc9jdhJJq\n71PONeo4RXuKM0TNaYiDkfjVOfxbvgVLZQjsvYZi2hViuUQUBfVFm4PWoDOIKJ4W\nOxmME2QFg5Rr1yVxgbGkYXzeA4BkUSNv9Xuxj0NbmOdC+M/RHA984r6kAGkaLHNr\n1PCfAL7hPkZhDA8dM/ocTRgFZueFYMAu/TyVIEmKCDsnsGwsZo0nkinDsH4/w177\nUy2Qr3EeYq5n8yQqMhuuHADMhYGZIgOlhTalB2/uiv4vy1FEC0JMMbj0wbBStTV0\nudh8lS2nQFYuILdBNyip6XrJS3M0q87W/t16+xYRJITncVj1Kg+jb9TpTZTzt36n\n2wIDAQAB\n-----END PUBLIC KEY-----\n")
var licensePublicKeyBytes = []byte("-----BEGIN PUBLIC KEY-----\nMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAk6PN/BEeehdR4QUoiq/l\nWZ6uSNl/XsvPd3XAhPiD7CsoeowWCLQzyU59FkXoUvmoIveF1Ns084R3CiJCWwZD\naTfiedFrDzCgLS3cSnF/KAYksU5fco8DZJNlY6dl0ikCkG3YXVgCYVoJ3OBGOGkT\nuBNuV8VeLGi24l3b1iBMammpKPGWBoXcGVDh/McK96c2nvALC0QvPzjHq8vxUjt0\nYwd9QKP4Hb/j06CjCoDEfi4kBjH0MWKDP+lZZzSl5r4B9/JE2eS7fdrB2vOJrqjX\nvHmcZItEqHeehXyIBc5wqGg5x95RfqGC2vYmUGopxqNo9Owd2iWQs39a3tFOipZ8\nNQIDAQAB\n-----END PUBLIC KEY-----\n")
var licensePrivateKeyBytes = []byte("-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQCTo838ER56F1Hh\nBSiKr+VZnq5I2X9ey893dcCE+IPsKyh6jBYItDPJTn0WRehS+agi94XU2zTzhHcK\nIkJbBkNpN+J50WsPMKAtLdxKcX8oBiSxTl9yjwNkk2Vjp2XSKQKQbdhdWAJhWgnc\n4EY4aRO4E25XxV4saLbiXdvWIExqaako8ZYGhdwZUOH8xwr3pzae8AsLRC8/OMer\ny/FSO3RjB31Ao/gdv+PToKMKgMR+LiQGMfQxYoM/6VlnNKXmvgH38kTZ5Lt92sHa\n84muqNe8eZxki0Sod56FfIgFznCoaDnH3lF+oYLa9iZQainGo2j07B3aJZCzf1re\n0U6Klnw1AgMBAAECggEABBJm+nZAaG6fN/0L7rNGOJALT4AMJsGpQaqyOhiejtNr\n5OFbNDdAHGO5SHV6IFu5WumHu+Sl07eKDvH98YO9NTgw21o+wSb+q/BRXI4/ywtM\nVMWMFV1DYzE7gSGbSc4Ov4wABScJvcSddAWNej3PqOqERxBu9fuXW6vqBcBTSzX/\nFge+jY3IhBwU/Vwz/EKralGaDd8kfEzdSlgCGzRdCD20mCt0x8VJKu+V86JvdqzA\nyaI+H9h5i6zDz/2hqSz/s1KO+DegXKEyYoH70wXDBmNe5RlkK+1/z4bgaIVbEY8Z\n7UChgNJ36MAcAXWgaA/XhYEhsy9uEoARuEVwnrWfuwKBgQDGR7E69eWcCVG9yn42\nq2UCzRFxzhxG+tM0NXuQvDilrRkW/SPfOieCzNBCWM/dJ3zGFY2K+U6U1vCSR8jM\no6r4EAO65n7BQkLFhfWEtk4+2DEfezPgxPBxQhM/0qQe5UObJNF14c6FfzNL6mx5\niaGLpVvjWijsnjb/ACwZJFwKMwKBgQC+nkiPAG6rSwsYYNqpUVXdSMRqJmVlVh8+\nK4+NYeUH/G4sx1pJ3JM32eF11j+DZs7nDk9TCodymjc9R0x0qOKeOFeWqJpewVtd\ndm+h06izWw5zEoQJaVid7Y5B10eNwWtd7ogu5h9wMOowyMK3t9aDZWyCHvNT28KO\nYlcvSbnH9wKBgEyxTD72/6HUBPb5DMqOjtp/gUDYrR1TRUALc8ju1KZYhrzamvZr\n4v53xBH1kikDbgKcMYxQk/GEFbcu5t8oayfZ4ed34g1UWMlX4Dg935P1QULg/5bv\n9eSI3zMvgWWl3flzS0ViWuRN6AR0HxL/himigyE0LWIgbDtD0MrEwoj7AoGAc2Tq\n0/cVCAlj7BwmAz5D1rQIg6I+27vpKf/A5XwP5GfCYsVEOVaYoMT7ohRTWr7QHjwh\ndUn4eT42lpglBrJ+jf3ZuFDVMuum3cunBLZXeEx1UOAyomftx51Z8y3aGUywLKsM\nMigJfCeAfovqpMFb0SuDJrqJ34g4HW4XDX96Qj0CgYEAoYL2U4pR3MZVdRgVrRCw\n8fJluUkSuro3XcuKK26wQlgHdmEEaebfJfef39CxGDBN6ZKH9NaWZ9nN1sEc/bak\n2h0ZstFJ+r0lN++BlOHVmTXj0S0kPUzOdw5UuuV6hkD4rbla1t2GtztaMoR1hIEj\nlXa43nUS5pdDka+OLTWndgE=\n-----END PRIVATE KEY-----\n")
var timeRecordPublicKeyBytes = []byte("-----BEGIN PUBLIC KEY-----\nMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAr3b2xr7Z/6xeMFzh9sIR\n+NhxCKHVViPlO8TQ2sAsrOwNlDfsbGYA8RjU3C4ig5llFCgGNjPmJTQOFzrgFoAo\nwe1sQI6ubNivju1YzWg5oPwXnh2/Hu3RT2nHXTrTXO1m06ymWoqJc81N5m6vBQ9o\nlP1V7TBT1KXFueB9zYyJx8X3e5CEGckdHoMelBL3wQUxVNIyjRijKwZPJCgO8Q4R\n8YypS9ClQZnWRkMYyNkl8g9gaP/QUfNC1c5Pej3ijEZ2HAvLraflCF82Jgl4rd9F\nPgdTIqVG6hc52dhLZ7QB2S6WcLPPfSGZU8ybW4JODNbOIQnE9+u4ZykzIWCpo5az\nIwIDAQAB\n-----END PUBLIC KEY-----\n")
var timeRecordPrivateKeyBytes = []byte("-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQCvdvbGvtn/rF4w\nXOH2whH42HEIodVWI+U7xNDawCys7A2UN+xsZgDxGNTcLiKDmWUUKAY2M+YlNA4X\nOuAWgCjB7WxAjq5s2K+O7VjNaDmg/BeeHb8e7dFPacddOtNc7WbTrKZaiolzzU3m\nbq8FD2iU/VXtMFPUpcW54H3NjInHxfd7kIQZyR0egx6UEvfBBTFU0jKNGKMrBk8k\nKA7xDhHxjKlL0KVBmdZGQxjI2SXyD2Bo/9BR80LVzk96PeKMRnYcC8utp+UIXzYm\nCXit30U+B1MipUbqFznZ2EtntAHZLpZws899IZlTzJtbgk4M1s4hCcT367hnKTMh\nYKmjlrMjAgMBAAECggEAIq9WiuURPNw65j6GIHwuh01p0rYC8Ps5hj4atxNEAY7M\nBF+lqavkRcSN11R7WB5Lf9eFmtNZjEMlAeyOfYQqCmO/gWdzDWssEQnUAw62TZ9n\nn8brj9adCKC1WzWUsIrxR6iaXc2C7FRKMOHyUQLBvqnxgWiLOb7nPh4lYCuG1Ol1\nirJeeBoqAD4iVkeCP6UU/aBzhs0vV86ADD4ngda5Npq482OXh6FK94iBKT2tbxNV\nrurd2Z1GWSqpDoZHPGUEkn2cz6p6Z6VbUshW5sVk5IjIleEYStWvh6EA457xWsM1\ni8a7eNOLF/ajlUD1HXmzdwXN7bhBeiTIOxlPpF14wQKBgQD2amkCdF6DzREChpej\nalRM5ATP1VknxXT+IHGdINrHU4jt9jz1npIw8DPv+fThDz8G5ZPvOylKnscz5Qvg\nED5mHbq4ctG1A+G159/97m502YVHTHQtuHlv2BVpGbQv4jDgvm6vNZdvfW668LAf\neuoGil2fV6D4TLBrLy9fnMTPIQKBgQC2ShYxWV1bcHemoFitsYtQjqbokmLcV8wq\nrw3FqHoNtVBQNZLWhYLIZSWnM+RQpgPFlGedk7GlF7oETQ/oTtp+s9/cK8X2brBi\nqIVVYM0BH7o0LMofOOOEdnUdiPLlrAwb9VUWfKLs2Iah1lQP0FU3V0FIMnBFqHyl\nkJVr+2BNwwKBgFILui0cDAA8fkZmBAVgOPNlFIkS012frEWVDd8wekfV84iv7Tom\n8ywiPljP1A4/ok+sjyYff68d0NvhjqOrJOuhSHNzn4ly4mtL6lPFWLfFWVAVD7XN\nb3mi6/YTneA3ouih336tDGAN1pmd3DaPGW7WETgl2C30cuUtT8u5CfqhAoGAK1aP\n0im53Uxu1emXS6xDP+K6Yd6zrEkfXCKENrLoWav6rScfgur4/eW2PvtCU740dVvk\nCn2bpXFvoygjGQruPWNMXI73oLAONVZ1ZKf/9T1yyoa/gw7GYK69B0mQ3fO6aUc5\ndIArR/3ufDl1gND6AY84EQ8UzCrTf5VRQPvhmHsCgYEAy6qKfF6M6K2wn7vmUThO\ni23C7zguhV7zA1jorErueegQAbkTYCBjHRPpIA6vX4tLzyR/i8eZcD3V1gq6smZd\n3RjmFJk1C8EM1OaaS5/ebHiSYdwmCdzqz9nIrZwwqFy7GybP/HLplIzYdxGvJ20q\n4KAfunvlLeBD/ay8sPF2y/E=\n-----END PRIVATE KEY-----\n")

var FingerprintPublicKey *rsa.PublicKey
var LicensePublicKey *rsa.PublicKey
var LicensePrivateKey *rsa.PrivateKey
var TimeRecordPublicKey *rsa.PublicKey
var TimeRecordPrivateKey *rsa.PrivateKey

// Auto generate public key when import the package
func init() {
	var err error
	FingerprintPublicKey, err = LoadPublicKeyFromBytes(fingerprintPublicKeyBytes)
	if err != nil {
		panic(fmt.Sprintf("failed to load RSA public key1: %v", err))
	}

	LicensePublicKey, err = LoadPublicKeyFromBytes(licensePublicKeyBytes)
	if err != nil {
		panic(fmt.Sprintf("failed to load RSA public key2: %v", err))
	}

	LicensePrivateKey, err = LoadPrivateKeyFromBytes(licensePrivateKeyBytes)
	if err != nil {
		panic(fmt.Sprintf("failed to load RSA private key2: %v", err))
	}

	TimeRecordPublicKey, err = LoadPublicKeyFromBytes(timeRecordPublicKeyBytes)
	if err != nil {
		panic(fmt.Sprintf("failed to load RSA public key3: %v", err))
	}

	TimeRecordPrivateKey, err = LoadPrivateKeyFromBytes(timeRecordPrivateKeyBytes)
	if err != nil {
		panic(fmt.Sprintf("failed to load RSA private key3: %v", err))
	}
}

// LoadPublicKeyFromBytes loads a byte slice of RSA public key
func LoadPublicKeyFromBytes(data []byte) (*rsa.PublicKey, error) {
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

// LoadPrivateKeyFromBytes loads a byte slice of RSA private key
func LoadPrivateKeyFromBytes(data []byte) (*rsa.PrivateKey, error) {
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
