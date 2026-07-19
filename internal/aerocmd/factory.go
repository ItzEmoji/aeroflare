package aerocmd

import (
	"sync"

	"github.com/itzemoji/aeroflare/internal/secrets"
	"github.com/itzemoji/aeroflare/pkg/cmd/root"
	"github.com/itzemoji/aeroflare/pkg/cmdutil"
	"github.com/itzemoji/aeroflare/pkg/iostreams"
	"github.com/spf13/viper"
)

// NewFactory wires the production Factory: real process streams, the OS
// keychain, and the on-disk config file. Secrets and Config are resolved
// lazily and memoized, so `aeroflare version` never touches the keychain.
func NewFactory(appVersion string) *cmdutil.Factory {
	var (
		secretsOnce sync.Once
		secretsMgr  secrets.Manager

		configOnce sync.Once
		configV    *viper.Viper
		configNew  bool
		configErr  error
	)

	f := &cmdutil.Factory{
		AppVersion: appVersion,
		IOStreams:  iostreams.System(),
		Overrides:  &cmdutil.Overrides{},
	}

	f.Secrets = func() cmdutil.SecretsManager {
		secretsOnce.Do(func() { secretsMgr = secrets.NewManager() })
		return secretsMgr
	}

	loadConfig := func() {
		configOnce.Do(func() { configV, configNew, configErr = root.InitConfig() })
	}

	f.Config = func() (*viper.Viper, error) {
		loadConfig()
		return configV, configErr
	}

	// IsNewConfig reports whether the config file was created on this run. It
	// forces the config load, since that is what decides the answer.
	f.IsNewConfig = func() bool {
		loadConfig()
		return configNew
	}

	f.CacheURL = func() string {
		v, err := f.Config()
		if err != nil {
			return ""
		}
		return root.ResolveCacheURL(v)
	}

	return f
}
