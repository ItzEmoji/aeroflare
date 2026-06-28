package auth

import (
	"errors"
	"os"

	"aeroflare/src/secrets"
)

var ErrTokenNotFound = errors.New("token not found in flags, environment, or secrets manager")

type Resolver struct {
	secretKey      string
	flagValue      string
	envVars        []string
	secretsManager secrets.Manager
}

func NewResolver(secretKey string) *Resolver {
	return &Resolver{secretKey: secretKey}
}

func (r *Resolver) WithSecretsManager(manager secrets.Manager) *Resolver {
	r.secretsManager = manager
	return r
}

func (r *Resolver) WithFlag(val string) *Resolver {
	r.flagValue = val
	return r
}

func (r *Resolver) WithEnv(keys ...string) *Resolver {
	r.envVars = append(r.envVars, keys...)
	return r
}

func (r *Resolver) Resolve() (string, error) {
	if r.flagValue != "" {
		return r.flagValue, nil
	}

	for _, key := range r.envVars {
		if val := os.Getenv(key); val != "" {
			return val, nil
		}
	}

	manager := r.secretsManager
	if manager == nil {
		manager = secrets.NewManager()
	}
	val, err := manager.Get(r.secretKey)
	if err != nil && err != secrets.ErrNotFound {
		return "", err
	}
	if err == nil && val != "" {
		return val, nil
	}

	return "", ErrTokenNotFound
}
