// Package secretstest provides an in-memory secrets.Manager for tests.
package secretstest

import "github.com/itzemoji/aeroflare/internal/secrets"

// MockManager is an in-memory secrets.Manager. Set Err to make every write fail.
type MockManager struct {
	Data map[string]string
	Err  error
}

func NewMockManager(data map[string]string) *MockManager {
	if data == nil {
		data = map[string]string{}
	}
	return &MockManager{Data: data}
}

func (m *MockManager) Set(key, value string) error {
	if m.Err != nil {
		return m.Err
	}
	m.Data[key] = value
	return nil
}

func (m *MockManager) Get(key string) (string, error) {
	if val, ok := m.Data[key]; ok {
		return val, nil
	}
	return "", secrets.ErrNotFound
}

func (m *MockManager) List() ([]string, error) {
	var keys []string
	for k := range m.Data {
		keys = append(keys, k)
	}
	return keys, nil
}

func (m *MockManager) Delete(key string) error {
	delete(m.Data, key)
	return nil
}
