// Package auth implements `aeroflare auth` and its subcommands.
package auth

import (
	"github.com/itzemoji/aeroflare/v2/pkg/cmd/auth/get"
	"github.com/itzemoji/aeroflare/v2/pkg/cmd/auth/importcmd"
	"github.com/itzemoji/aeroflare/v2/pkg/cmd/auth/login"
	"github.com/itzemoji/aeroflare/v2/pkg/cmd/auth/remove"
	"github.com/itzemoji/aeroflare/v2/pkg/cmd/auth/set"
	"github.com/itzemoji/aeroflare/v2/pkg/cmd/auth/status"
	"github.com/itzemoji/aeroflare/v2/pkg/cmdutil"

	"github.com/spf13/cobra"
)

func NewCmdAuth(f *cmdutil.Factory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage Aeroflare authentication secrets",
	}

	cmd.AddCommand(login.NewCmdLogin(f))
	cmd.AddCommand(status.NewCmdStatus(f))
	cmd.AddCommand(set.NewCmdSet(f))
	cmd.AddCommand(get.NewCmdGet(f))
	cmd.AddCommand(remove.NewCmdRemove(f))
	cmd.AddCommand(importcmd.NewCmdImport(f))

	return cmd
}
