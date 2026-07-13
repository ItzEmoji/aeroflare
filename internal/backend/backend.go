package backend

import (
	"context"

	"github.com/google/go-containerregistry/pkg/authn"
)

// PushReceipt records the result of preparing and uploading a single store
// path's NAR, so the backend can publish its narinfo/nar pair.
type PushReceipt struct {
	StorePath   string
	NarinfoPath string
	NarDigest   string
	NarSize     int64
	NarPath     string
	Compression string // narinfo Compression value (e.g. "xz", "zstd")
	IsRoot      bool
}

// CacheBackend publishes completed pushes (nar + narinfo pairs) to the OCI
// registry. The only implementation is NativeBackend, which publishes one OCI
// image per package.
type CacheBackend interface {
	PushReceipts(ctx context.Context, receipts []PushReceipt) error
}

// BackendConfig holds the destination and credentials for the CacheBackend.
type BackendConfig struct {
	Registry          string
	Repository        string
	Auth              authn.Authenticator
	PubKeyPath        string
	ConfigAnnotations map[string]string
	Workers           int
}

// NewCacheBackend returns the native OCI-tag backend.
func NewCacheBackend(cfg BackendConfig) CacheBackend {
	return &NativeBackend{cfg: cfg}
}

// workerLimit returns workers if positive, otherwise def. Used to size push
// concurrency instead of hardcoding a limit.
func workerLimit(workers, def int) int {
	if workers > 0 {
		return workers
	}
	return def
}
