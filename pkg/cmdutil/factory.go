// Package cmdutil holds the Factory, the dependency bundle every aeroflare
// command is constructed with. Commands take a *Factory rather than reaching
// for globals, so each one can be built and tested in isolation.
package cmdutil

import (
	"github.com/itzemoji/aeroflare/internal/secrets"
	"github.com/itzemoji/aeroflare/pkg/iostreams"
	"github.com/spf13/viper"
)

// Overrides holds credential values supplied as root persistent flags. They
// take precedence over the secrets manager and the environment when a command
// resolves a token. Zero values mean "not supplied".
type Overrides struct {
	GithubToken string
	GitlabToken string
	CfToken     string
	CfUserID    string

	// Verbose is the -v count: 1 enables package-level logging, 2 enables
	// request logging.
	Verbose int
}

type Factory struct {
	AppVersion string
	IOStreams  *iostreams.IOStreams
	Overrides  *Overrides

	// Secrets and Config are functions rather than values so a command that
	// never needs them (e.g. `version`) does not pay to construct a keychain
	// client or read a config file, and so tests can substitute fakes without
	// touching global state.
	Secrets func() secrets.Manager
	Config  func() (*viper.Viper, error)

	// CacheURL resolves the effective OCI cache URL: an explicit --cache-url
	// flag wins, otherwise a shorthand --cache "org/repo" value is expanded to
	// a ghcr.io URL. Returns "" if neither is set.
	CacheURL func() string

	// IsNewConfig reports whether the config file was created fresh on this
	// run. `settings` uses it to choose between "Initial config has been saved
	// to" and "Config has been updated in".
	IsNewConfig func() bool
}
