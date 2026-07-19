// Package configure implements `aeroflare configure`, which interactively
// configures cache settings and saves them to the OCI config manifest.
package configure

import (
	"context"
	"errors"
	"fmt"

	"github.com/itzemoji/aeroflare/internal/ui"
	"github.com/itzemoji/aeroflare/pkg/cmd/auth/shared"
	"github.com/itzemoji/aeroflare/pkg/cmdutil"
	"github.com/itzemoji/aeroflare/pkg/iostreams"
	"github.com/itzemoji/aeroflare/pkg/oci"
	"github.com/itzemoji/aeroflare/pkg/proxy"

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
		Short: "Configure the remote cache (writes to the OCI manifest)",
		Long: `Configure the remote cache by writing settings (such as the Nix
signing public key) into the cache-config manifest stored in the OCI registry.

These settings live with the cache and are shared by everyone who uses it. To
change local, per-machine preferences (theme, registry logins, cache URL)
instead, use "aeroflare settings".`,
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
	// Fetch existing config manifest so we can prefill the public key.
	var existingPublicKey = ""

	// TokenForRegistry already fails when no credential can be found, so there
	// is no separate emptiness check to make here.
	token, err := shared.TokenForRegistry(f, registry)
	if err != nil {
		return err
	}
	auth := cmdutil.RegistryAuth(registry, token)
	remoteConf, existingAnnotations, _ := proxy.BootstrapConfigWithAnnotations(context.Background(), registry, repository, auth)
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
	).WithTheme(ui.AeroflareTheme())

	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return cmdutil.ErrCancel
		}
		return fmt.Errorf("form error: %w", err)
	}

	annotations := map[string]string{
		"aeroflare.public-key": publicKey,
	}

	opts.IO.Info("Saving configuration to OCI manifest annotations...")

	if err := oci.PushConfigManifest(registry, repository, auth, annotations); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	opts.IO.Success("Configuration successfully saved to cache-config manifest!")

	return nil
}
