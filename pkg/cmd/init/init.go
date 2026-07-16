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
	// The wizard authenticates every service it needs through the auth module,
	// so by the time it returns, cfg holds the credentials and they are already
	// saved. Re-resolving them here is what used to trigger a second login.
	cfg, err := setup.RunWizard(f)
	if err != nil {
		return err
	}

	confirmed, err := setup.DisplaySummary(cfg)
	if err != nil || !confirmed {
		opts.IO.Info("Setup cancelled.")
		return nil
	}

	// Export for the tooling provisioning shells out to.
	_ = os.Setenv("CLOUDFLARE_API_TOKEN", cfg.CloudflareToken)
	_ = os.Setenv("CLOUDFLARE_ACCOUNT_ID", cfg.CloudflareAccountID)

	// ghcr.io authenticates with the GitHub token, which the wizard only
	// collects when GitHub is also the git provider.
	if cfg.Registry == "ghcr.io" {
		ghToken := cfg.GitToken
		if ghToken == "" {
			if ghToken, err = shared.RequireGithubToken(f); err != nil {
				return err
			}
		}
		_ = os.Setenv("GITHUB_TOKEN", ghToken)
	}

	return setup.RunProvision(cfg)
}
