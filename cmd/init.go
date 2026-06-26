package cmd

import (
	"os"

	setup "aeroflare/src/init"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize Aeroflare infrastructure via interactive wizard",
	Long: `Run the Aeroflare setup wizard to provision all required infrastructure:

  • OCI repository for storing cache data
  • Cloudflare R2 bucket (if selected)
  • Cloudflare Worker deployment
  • Git repository and CI/CD integration (if selected)

The wizard asks all questions up front and shows a summary before making
any changes. No infrastructure is created until you confirm.`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := setup.RunWizard()
		if err != nil {
			PrintError(err.Error())
			os.Exit(1)
		}

		confirmed, err := setup.DisplaySummary(cfg)
		if err != nil || !confirmed {
			PrintInfo("Setup cancelled.")
			return
		}

		if err := setup.RunProvision(cfg); err != nil {
			PrintError(err.Error())
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
