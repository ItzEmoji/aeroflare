package secrets

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"
)

type Manager interface {
	Set(key, value string) error
	Get(key string) (string, error)
}

type defaultManager struct {
	serviceName string
}

func NewManager() Manager {
	return &defaultManager{serviceName: "aeroflare"}
}

func getConfigDir() string {
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		homeDir, err := os.UserHomeDir()
		if err == nil && homeDir != "" {
			configDir = filepath.Join(homeDir, ".config")
		}
	}
	return filepath.Join(configDir, "aeroflare")
}

func getFallbackFile() string {
	return filepath.Join(getConfigDir(), "secrets.json")
}

func (m *defaultManager) Set(key, value string) error {
	err := keyring.Set(m.serviceName, key, value)
	if err == nil {
		return nil
	}
	
	// Fallback
	dir := getConfigDir()
	os.MkdirAll(dir, 0755)
	
	file := getFallbackFile()
	secrets := make(map[string]string)
	
	data, err := os.ReadFile(file)
	if err == nil {
		if len(data) > 0 {
			if err := json.Unmarshal(data, &secrets); err != nil {
				return err
			}
		}
	}
	
	secrets[key] = value
	
	out, err := json.MarshalIndent(secrets, "", "  ")
	if err != nil {
		return err
	}
	
	tmpFile := file + ".tmp"
	if err := os.WriteFile(tmpFile, out, 0600); err != nil {
		return err
	}
	return os.Rename(tmpFile, file)
}

func (m *defaultManager) Get(key string) (string, error) {
	val, err := keyring.Get(m.serviceName, key)
	if err == nil {
		return val, nil
	}
	
	// Fallback
	file := getFallbackFile()
	data, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	
	secrets := make(map[string]string)
	if err := json.Unmarshal(data, &secrets); err != nil {
		return "", err
	}
	
	if v, ok := secrets[key]; ok {
		return v, nil
	}
	
	return "", keyring.ErrNotFound
}
