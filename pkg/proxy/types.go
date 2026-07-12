package proxy

// RemoteConfig represents the optional dynamic configuration JSON loaded from GHCR.
// Used by the push/configure pipeline, not by the proxy.
type RemoteConfig struct {
	WorkerURL      string   `json:"worker_url"`
	PublicKey      string   `json:"public_key"`
	UpstreamCaches []string `json:"upstream_caches"`
}
