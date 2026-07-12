// Package configure implements `aeroflare configure`, which interactively
// configures cache settings and saves them to the OCI config manifest.
package configure

import (
	"context"
	"fmt"

	setup "github.com/itzemoji/aeroflare/internal/init"
	"github.com/itzemoji/aeroflare/pkg/oci"
	"github.com/itzemoji/aeroflare/pkg/proxy"
	"github.com/itzemoji/aeroflare/pkg/cmd/auth/shared"
	"github.com/itzemoji/aeroflare/pkg/cmdutil"
	"github.com/itzemoji/aeroflare/pkg/iostreams"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

// Options holds the dependencies configureRun needs.
type Options struct {
	IO *iostreams.IOStreams
}

// NewCmdConfigure builds the `aeroflare configure` command.
func NewCmdConfigure(f *cmdutil.Factory) *cobra.Command {
	opts := &Options{
		IO: f.IOStreams,
	}

	cmd := &cobra.Command{
		Use:   "configure",
		Short: "Interactively configure cache settings",
		RunE: func(cmd *cobra.Command, args []string) error {
			return configureRun(f, opts)
		},
	}

	return cmd
}

func configureRun(f *cmdutil.Factory, opts *Options) error {
	registry, repository, err := cmdutil.RegistryAndRepository()
	if err != nil {
		return err
	}
	ociToken := cmdutil.RegistryToken(registry, repository, "")
	if ociToken == "" {
		return fmt.Errorf("authentication token missing (oci_token, GITHUB_TOKEN or GH_TOKEN)")
	}

	// Fetch existing config manifest so we can prefill the public key.
	var existingPublicKey = ""

	token, err := shared.TokenForRegistry(f, registry)
	if err != nil {
		return err
	}
	tokenMgr := proxy.NewTokenManager(registry, repository, token)
	remoteConf, existingAnnotations, _ := proxy.BootstrapConfigWithAnnotations(context.Background(), nil, registry, repository, tokenMgr)
	if existingAnnotations != nil {
		if pk := existingAnnotations["aeroflare.public-key"]; pk != "" {
			existingPublicKey = pk
		} else if pk := existingAnnotations["public-key"]; pk != "" {
			existingPublicKey = pk
		}
	} else if remoteConf != nil && remoteConf.PublicKey != "" {
		existingPublicKey = remoteConf.PublicKey
	}

	publicKey := existingPublicKey

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Public Key").
				Description("Enter your nix cache public key (optional)").
				Value(&publicKey),
		),
	).WithTheme(setup.AeroflareTheme())

	if err := form.Run(); err != nil {
		if err.Error() != "user aborted" {
			return fmt.Errorf("form error: %v", err)
		}
		return cmdutil.ErrCancel
	}

	annotations := map[string]string{
		"aeroflare.public-key": publicKey,
	}

	opts.IO.Info("Saving configuration to OCI manifest annotations...")

	if err := oci.PushConfigManifest(registry, repository, ociToken, annotations); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	opts.IO.Success("Configuration successfully saved to cache-config manifest!")

	return nil
}
