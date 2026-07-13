// Package login implements `aeroflare auth login`.
package login

import (
	"fmt"

	"github.com/itzemoji/aeroflare/pkg/cmd/auth/shared"
	"github.com/itzemoji/aeroflare/pkg/cmdutil"

	"github.com/spf13/cobra"
)

// Options holds the dependencies loginRun needs, so it can be exercised in
// tests without going through cobra.
type Options struct {
	F *cmdutil.Factory
}

func NewCmdLogin(f *cmdutil.Factory) *cobra.Command {
	opts := &Options{F: f}

	return &cobra.Command{
		Use:   "login",
		Short: "Authenticate interactively",
		RunE: func(cmd *cobra.Command, args []string) error {
			return loginRun(opts)
		},
	}
}

func loginRun(opts *Options) error {
	f := opts.F
	manager := f.Secrets()

	tokens := []struct {
		key string
		val string
	}{
		{"github-token", f.Overrides.GithubToken},
		{"gitlab-token", f.Overrides.GitlabToken},
		{"cf-token", f.Overrides.CfToken},
		{"cf-user-id", f.Overrides.CfUserID},
	}

	savedAny := false
	for _, t := range tokens {
		if t.val != "" {
			if err := manager.Set(t.key, t.val); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(f.IOStreams.Out, "Saved %s\n", t.key)
			savedAny = true
		}
	}

	if !savedAny {
		return shared.RunInteractiveAuth(f)
	}
	return nil
}
