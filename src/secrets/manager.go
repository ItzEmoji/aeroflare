package secrets

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zalando/go-keyring"
)

var ErrNotFound = keyring.ErrNotFound

type Manager interface {
	Set(key, value string) error
	Get(key string) (string, error)
	List() ([]string, error)
	Delete(key string) error
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

func (m *defaultManager) updateIndex(key string, remove bool) {
	if key == "_keys_index" {
		return
	}
	indexStr, _ := m.Get("_keys_index")
	keys := make(map[string]bool)
	if indexStr != "" {
		for _, k := range strings.Split(indexStr, ",") {
			if k != "" {
				keys[k] = true
			}
		}
	}
	
	changed := false
	if remove {
		if keys[key] {
			delete(keys, key)
			changed = true
		}
	} else {
		if !keys[key] {
			keys[key] = true
			changed = true
		}
	}
	
	if changed {
		var newKeys []string
		for k := range keys {
			newKeys = append(newKeys, k)
		}
		_ = m.Set("_keys_index", strings.Join(newKeys, ","))
	}
}

func (m *defaultManager) Set(key, value string) error {
	err := keyring.Set(m.serviceName, key, value)
	if err == nil {
		if key != "_keys_index" {
			fmt.Printf("🔒 Secret '%s' securely written to keychain.\n", key)
		}
		m.updateIndex(key, false)
		return nil
	}
	
	if key != "_keys_index" {
		fmt.Printf("⚠️ Warning: Keychain not available. Secret '%s' written in plain text to fallback file.\n", key)
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
	err = os.Rename(tmpFile, file)
	if err == nil {
		m.updateIndex(key, false)
	}
	return err
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

func (m *defaultManager) List() ([]string, error) {
	keySet := make(map[string]bool)

	indexStr, _ := m.Get("_keys_index")
	if indexStr != "" {
		for _, k := range strings.Split(indexStr, ",") {
			if k != "" {
				keySet[k] = true
			}
		}
	}

	// Merge keys from fallback JSON
	file := getFallbackFile()
	data, err := os.ReadFile(file)
	if err == nil {
		secrets := make(map[string]string)
		if json.Unmarshal(data, &secrets) == nil {
			for k := range secrets {
				keySet[k] = true
			}
		}
	}

	var keys []string
	for k := range keySet {
		if k != "_keys_index" {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (m *defaultManager) Delete(key string) error {
	err := keyring.Delete(m.serviceName, key)
	if err != nil && err != keyring.ErrNotFound {
		// If keyring fails for another reason, we might still want to try fallback.
		// But usually it's fine.
	}

	// Also try removing from fallback
	file := getFallbackFile()
	data, errFallback := os.ReadFile(file)
	if errFallback == nil {
		secrets := make(map[string]string)
		if json.Unmarshal(data, &secrets) == nil {
			if _, ok := secrets[key]; ok {
				delete(secrets, key)
				out, _ := json.MarshalIndent(secrets, "", "  ")
				tmpFile := file + ".tmp"
				_ = os.WriteFile(tmpFile, out, 0600)
				_ = os.Rename(tmpFile, file)
			}
		}
	}

	m.updateIndex(key, true)
	return nil
}
