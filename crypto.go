package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"golang.org/x/crypto/pbkdf2"
)

// encryptionKey is derived lazily on first use so PBKDF2 (100k iterations)
// runs concurrently with Fyne startup rather than blocking package init.
// When ENCRYPT_CONNECTION_DETAILS=no the key is never computed at all.
var (
	encryptionKeyOnce sync.Once
	encryptionKey     []byte
)

func getEncryptionKey() []byte {
	encryptionKeyOnce.Do(func() {
		encryptionKey = deriveKeyFromMachine()
	})
	return encryptionKey
}

// encryptConnectionData encrypts the connection details using AES-256-GCM
func encryptConnectionData(data string) (string, error) {
	block, err := aes.NewCipher(getEncryptionKey())
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(data), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decryptConnectionData decrypts the connection details using AES-256-GCM
func decryptConnectionData(encryptedData string) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encryptedData)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(getEncryptionKey())
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// packConnectionData packs connection fields into key=value pairs
func packConnectionData(env, ips, port, user, pass string, promptUser, promptPass bool) string {
	parts := []string{
		fmt.Sprintf("env=%s", env),
		fmt.Sprintf("ips=%s", ips),
		fmt.Sprintf("port=%s", port),
		fmt.Sprintf("user=%s", user),
		fmt.Sprintf("pass=%s", pass),
		fmt.Sprintf("prompt_user=%v", promptUser),
		fmt.Sprintf("prompt_pass=%v", promptPass),
	}
	return strings.Join(parts, "|")
}

// unpackConnectionData unpacks key=value pairs into connection fields
func unpackConnectionData(data string) (env, ips, port, user, pass string, promptUser, promptPass bool, err error) {
	parts := strings.Split(data, "|")
	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key, value := kv[0], kv[1]
		switch key {
		case "env":
			env = value
		case "ips":
			ips = value
		case "port":
			port = value
		case "user":
			user = value
		case "pass":
			pass = value
		case "prompt_user":
			promptUser = (value == "true")
		case "prompt_pass":
			promptPass = (value == "true")
		}
	}
	return
}

// deriveKeyFromMachine derives a 32-byte encryption key from machine-specific identifiers
func deriveKeyFromMachine() []byte {
	machineID := getMachineID()
	salt := []byte("A-Smart-GUI-For-ApacheCassandra-By-Oramad-2026-Salt")

	// Use PBKDF2 to derive 32-byte key for AES-256
	key := pbkdf2.Key([]byte(machineID), salt, 100000, 32, sha256.New)
	return key
}

// getMachineID returns a unique identifier for the machine
func getMachineID() string {
	var identifiers []string

	// 1. Hostname
	if hostname, err := os.Hostname(); err == nil {
		identifiers = append(identifiers, hostname)
	}

	// 2. Machine ID from OS (Linux/Unix)
	if machineID := getOSMachineID(); machineID != "" {
		identifiers = append(identifiers, machineID)
	}

	// Combine all identifiers and hash them
	combined := strings.Join(identifiers, "|")
	hash := sha256.Sum256([]byte(combined))
	return hex.EncodeToString(hash[:])
}

// getOSMachineID gets system-specific machine identifier
func getOSMachineID() string {
	switch runtime.GOOS {
	case "linux":
		return getLinuxMachineID()
	case "darwin":
		return getMacOSMachineID()
	case "windows":
		return getWindowsMachineID()
	default:
		return ""
	}
}

// getLinuxMachineID reads /etc/machine-id
func getLinuxMachineID() string {
	// Try systemd machine-id
	if data, err := os.ReadFile("/etc/machine-id"); err == nil {
		return strings.TrimSpace(string(data))
	}

	// Try dbus machine-id (older systems)
	if data, err := os.ReadFile("/var/lib/dbus/machine-id"); err == nil {
		return strings.TrimSpace(string(data))
	}

	return ""
}

// getMacOSMachineID gets hardware UUID on macOS
func getMacOSMachineID() string {
	cmd := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	// Parse output for IOPlatformUUID
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.Contains(line, "IOPlatformUUID") {
			parts := strings.Split(line, "\"")
			if len(parts) >= 4 {
				return parts[3]
			}
		}
	}

	return ""
}

// getWindowsMachineID gets machine GUID on Windows
func getWindowsMachineID() string {
	cmd := exec.Command("wmic", "csproduct", "get", "UUID")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) >= 2 {
		return strings.TrimSpace(lines[1])
	}

	return ""
}
