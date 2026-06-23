package proxy

// IndexManifest represents the OCI image manifest for the cache index or configuration.
type IndexManifest struct {
	Layers      []IndexLayer      `json:"layers"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// IndexLayer represents a layer in the OCI manifest.
type IndexLayer struct {
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

// IndexEntry represents a single cached store path entry.
type IndexEntry struct {
	NarInfo   string `json:"narinfo"`
	NarDigest string `json:"nar_digest"`
}

// CacheIndexData represents the parsed cache-index JSON payload.
type CacheIndexData struct {
	Entries   map[string]IndexEntry `json:"entries"`
	PublicKey string                `json:"public_key"`
	Generated string                `json:"generated"`
}

// RemoteConfig represents the optional dynamic configuration JSON loaded from GHCR.
// Used by the push/configure pipeline, not by the proxy.
type RemoteConfig struct {
	WorkerURL      string   `json:"worker_url"`
	PublicKey      string   `json:"public_key"`
	UpstreamCaches []string `json:"upstream_caches"`
}
