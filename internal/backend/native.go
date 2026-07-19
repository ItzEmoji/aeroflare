package backend

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/itzemoji/aeroflare/pkg/oci"
	"github.com/itzemoji/aeroflare/pkg/prepare/narinfo"

	"github.com/google/go-containerregistry/pkg/v1/types"
	"golang.org/x/sync/errgroup"
)

// NativeBackend publishes each receipt as its own OCI image (one manifest
// per Nix package, tagged with the store hash).
type NativeBackend struct {
	cfg BackendConfig
}

// PushReceipts pushes each receipt concurrently (bounded by a worker limit)
// as an independent OCI image; a failure on one receipt does not stop the
// others already in flight but is still returned to the caller.
func (b *NativeBackend) PushReceipts(ctx context.Context, receipts []PushReceipt) error {
	// One shared pusher so all package image pushes reuse a single registry
	// auth handshake instead of each repeating the /v2/ 401 challenge.
	pusher, err := oci.NewImagePusher(b.cfg.Auth)
	if err != nil {
		return fmt.Errorf("failed to create registry pusher: %w", err)
	}

	eg, _ := errgroup.WithContext(ctx)
	eg.SetLimit(workerLimit(b.cfg.Workers, 5))

	for _, receipt := range receipts {
		receipt := receipt
		eg.Go(func() error {
			if err := ctx.Err(); err != nil {
				return err
			}

			narinfoData, err := os.ReadFile(receipt.NarinfoPath)
			if err != nil {
				return fmt.Errorf("failed to read narinfo (%s): %w", receipt.NarinfoPath, err)
			}
			ni, err := narinfo.Parse(string(narinfoData))
			if err != nil {
				return fmt.Errorf("failed to parse narinfo (%s): %w", receipt.NarinfoPath, err)
			}

			if receipt.NarPath == "" {
				return fmt.Errorf("could not find .nar file for %s (NarPath is empty)", receipt.NarinfoPath)
			}

			layer, _, err := oci.NewLayerFast(receipt.NarPath, types.MediaType("application/vnd.aeroflare.nar.v1+"+ni.Compression), ni)
			if err != nil {
				return fmt.Errorf("failed to create layer for (%s): %w", receipt.NarPath, err)
			}

			basename := filepath.Base(ni.StorePath)
			tag := strings.SplitN(basename, "-", 2)[0]

			err = oci.PushNarPackageWith(ctx, pusher, layer, ni, tag, b.cfg.Registry, b.cfg.Repository)
			if err != nil {
				return fmt.Errorf("failed to push native artifact (%s): %w", receipt.StorePath, err)
			}
			return nil
		})
	}
	return eg.Wait()
}
