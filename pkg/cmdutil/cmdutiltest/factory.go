// Package cmdutiltest builds Factories for tests.
package cmdutiltest

import (
	"bytes"
	"testing"

	"github.com/itzemoji/aeroflare/internal/secrets/secretstest"
	"github.com/itzemoji/aeroflare/pkg/cmdutil"
	"github.com/itzemoji/aeroflare/pkg/iostreams"
	"github.com/spf13/viper"
)

// NewTestFactory returns a Factory backed by buffers and an in-memory secrets
// manager, plus the stdout and stderr buffers to assert on. Stdin is reported
// as non-interactive; call f.IOStreams.SetStdinTTY(true) to exercise the
// interactive branches.
func NewTestFactory(t *testing.T, stored map[string]string) (*cmdutil.Factory, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()

	io, _, out, errOut := iostreams.Test()
	io.SetStdinTTY(false)

	mgr := secretstest.NewMockManager(stored)
	v := viper.New()

	f := &cmdutil.Factory{
		IOStreams:   io,
		Overrides:   &cmdutil.Overrides{},
		Secrets:     func() cmdutil.SecretsManager { return mgr },
		Config:      func() (*viper.Viper, error) { return v, nil },
		CacheURL:    func() string { return "" },
		IsNewConfig: func() bool { return false },
	}

	return f, out, errOut
}
