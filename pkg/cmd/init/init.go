// Package initcmd implements `aeroflare init`, an interactive wizard that
// provisions Aeroflare infrastructure. It is named initcmd (not init)
// because a Go package cannot be named init; the cobra Use string is still
// "init".
package initcmd

import (
	"os"

	setup "github.com/itzemoji/aeroflare/internal/init"
	"github.com/itzemoji/aeroflare/pkg/cmd/auth/shared"
	"github.com/itzemoji/aeroflare/pkg/cmdutil"
	"github.com/itzemoji/aeroflare/pkg/iostreams"

	"github.com/spf13/cobra"
)

// Options holds the dependencies initRun needs.
type Options struct {
	IO *iostreams.IOStreams
}

// NewCmdInit builds the `aeroflare init` command.
func NewCmdInit(f *cmdutil.Factory) *cobra.Command {
	opts := &Options{
		IO: f.IOStreams,
	}

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize Aeroflare infrastructure via interactive wizard",
		Long: `Run the Aeroflare setup wizard to provision all required infrastructure:

  • OCI repository for storing cache data
  • Cloudflare Worker deployment
  • Git repository and CI/CD integration (if selected)

The wizard asks all questions up front and shows a summary before making
any changes. No infrastructure is created until you confirm.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return initRun(f, opts)
		},
	}

	return cmd
}

func initRun(f *cmdutil.Factory, opts *Options) error {
	cfg, err := setup.RunWizard()
	if err != nil {
		return err
	}

	confirmed, err := setup.DisplaySummary(cfg)
	if err != nil || !confirmed {
		opts.IO.Info("Setup cancelled.")
		return nil
	}

	// Cloudflare tokens are always needed to deploy the Worker.
	cfToken, cfID, err := shared.RequireCloudflareToken(f)
	if err != nil {
		return err
	}
	_ = os.Setenv("CLOUDFLARE_API_TOKEN", cfToken)
	_ = os.Setenv("CLOUDFLARE_ACCOUNT_ID", cfID)

	// Ensure Github token exists if they need GitHub Actions / Registry
	if cfg.Registry == "ghcr.io" || cfg.GitProvider == setup.GitGitHub {
		ghToken, err := shared.RequireGithubToken(f)
		if err != nil {
			return err
		}
		_ = os.Setenv("GITHUB_TOKEN", ghToken)
	}

	if err := setup.RunProvision(cfg); err != nil {
		return err
	}

	return nil
}
