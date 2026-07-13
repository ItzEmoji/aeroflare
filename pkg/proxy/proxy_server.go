package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/itzemoji/aeroflare/pkg/oci"
)

// ProxyServer bridges the Nix binary cache protocol to an OCI registry and
// upstream caches.
//
// It holds no cache of its own: every narinfo, NAR, and public-key lookup is
// resolved with a direct request to the OCI registry (the per-package manifest
// tagged with the store hash, or the "cache-config" manifest for the public
// key). There is no index blob, no TTL, and nothing to refresh.
//
// Registry credentials live in the puller's transport, not in this struct: it
// authenticates on first use and re-authenticates whenever the registry says
// the credential has expired, so nothing here tracks token lifetimes.
type ProxyServer struct {
	Port           int
	ListenAddr     string
	Registry       string
	Repository     string
	UpstreamCaches []string

	repo   name.Repository
	auth   authn.Authenticator
	puller *remote.Puller

	// upstream deliberately carries no registry credentials: the upstream
	// caches are unrelated hosts, and a credential must not be sent to one.
	upstream *http.Client
}

// NewProxyServer builds a server that serves repository on registry, falling
// back to upstreams for anything the registry cannot satisfy. A nil auth reads
// anonymously, which is all a public cache needs.
func NewProxyServer(registry, repository string, upstreams []string, auth authn.Authenticator) (*ProxyServer, error) {
	repo, err := oci.Repository(registry, repository)
	if err != nil {
		return nil, fmt.Errorf("invalid registry/repository: %w", err)
	}
	puller, err := oci.Puller(auth)
	if err != nil {
		return nil, fmt.Errorf("failed to create registry puller: %w", err)
	}

	return &ProxyServer{
		Registry:       registry,
		Repository:     repository,
		UpstreamCaches: upstreams,
		repo:           repo,
		auth:           auth,
		puller:         puller,
		upstream:       &http.Client{Timeout: 30 * time.Minute}, // high, for massive NARs
	}, nil
}

// Handler handles all incoming HTTP requests for the Nix binary cache proxy.
func (ps *ProxyServer) Handler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")

	switch r.Method {
	case http.MethodGet, http.MethodHead:
		switch {
		case path == "/nix-cache-info":
			ps.serveNixCacheInfo(w)
		case path == "/public-key":
			ps.servePublicKey(w, r)
		case path == "/api/public-key":
			ps.serveApiPublicKey(w, r)
		case path == "/_status":
			ps.serveStatus(w, r)
		case strings.HasSuffix(path, ".narinfo"):
			ps.serveNarInfo(w, r, path)
		case strings.HasPrefix(path, "/nar/"):
			ps.serveNar(w, r, path, r.Method)
		default:
			http.Error(w, "Not Found", http.StatusNotFound)
		}
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

// pkg is one package's OCI manifest, reduced to what the proxy serves from it:
// the descriptor of the NAR layer, and the aeroflare.* narinfo fields. A NAR
// request needs only the former, so the fields are read lazily: recovering them
// can cost a second request, and there is no reason to pay it for a blob fetch.
type pkg struct {
	desc   *remote.Descriptor
	labels map[string]string // annotations as found on the manifest; may carry no narinfo
	nar    v1.Descriptor
}

// fetchPackage fetches the manifest tagged with the store hash.
func (ps *ProxyServer) fetchPackage(ctx context.Context, tag string) (*pkg, error) {
	desc, err := ps.puller.Get(ctx, ps.repo.Tag(tag))
	if err != nil {
		return nil, err
	}

	var mf struct {
		Annotations map[string]string `json:"annotations"`
		Labels      map[string]string `json:"labels"`
		Layers      []v1.Descriptor   `json:"layers"`
	}
	if err := json.Unmarshal(desc.Manifest, &mf); err != nil {
		return nil, err
	}
	if len(mf.Layers) == 0 {
		return nil, fmt.Errorf("no layers in manifest for %s", tag)
	}

	labels := mf.Annotations
	if labels["aeroflare.storepath"] == "" {
		labels = mf.Labels
	}

	return &pkg{desc: desc, labels: labels, nar: mf.Layers[0]}, nil
}

// narinfoLabels returns the aeroflare.* fields describing the package. Aeroflare
// writes them as manifest annotations; the image config's labels are a fallback
// for images written by other tools, which put their metadata there instead.
func (p *pkg) narinfoLabels() (map[string]string, error) {
	if p.labels["aeroflare.storepath"] != "" {
		return p.labels, nil
	}

	labels, err := configLabels(p.desc)
	if err != nil {
		return nil, err
	}
	if labels["aeroflare.storepath"] == "" {
		return nil, errors.New("no narinfo metadata in manifest annotations or image config")
	}
	return labels, nil
}

// configLabels reads the labels from an image's config blob, checking both the
// standard "config.Labels" location and the top-level one, since different
// tools populate different places.
func configLabels(desc *remote.Descriptor) (map[string]string, error) {
	img, err := desc.Image()
	if err != nil {
		return nil, err
	}
	raw, err := img.RawConfigFile()
	if err != nil {
		return nil, err
	}

	var cfg struct {
		Config struct {
			Labels      map[string]string `json:"Labels"`
			LabelsLower map[string]string `json:"labels"`
		} `json:"config"`
		Labels      map[string]string `json:"Labels"`
		LabelsLower map[string]string `json:"labels"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}

	for _, m := range []map[string]string{cfg.Config.Labels, cfg.Labels, cfg.Config.LabelsLower, cfg.LabelsLower} {
		if len(m) > 0 {
			return m, nil
		}
	}
	return nil, nil
}

// fetchPublicKey fetches the cache's public key straight from the
// "cache-config" OCI manifest annotations. There is no caching: each call is a
// fresh registry request. Returns "" if the manifest can't be fetched or has no
// key configured.
func (ps *ProxyServer) fetchPublicKey(ctx context.Context) string {
	anns, err := oci.FetchAeroflareAnnotations(ctx, ps.Registry, ps.Repository, "cache-config", ps.auth)
	if err != nil {
		return ""
	}
	if pk := anns["aeroflare.public-key"]; pk != "" {
		return pk
	}
	return anns["public-key"]
}

func (ps *ProxyServer) serveNixCacheInfo(w http.ResponseWriter) {
	data := []byte("StoreDir: /nix/store\nWantMassQuery: 1\nPriority: 40\n")
	w.Header().Set("Content-Type", "text/x-nix-cache-info")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (ps *ProxyServer) servePublicKey(w http.ResponseWriter, r *http.Request) {
	pubKey := ps.fetchPublicKey(r.Context())

	if pubKey != "" {
		pubKey = strings.TrimSpace(pubKey) + "\n"
		data := []byte(pubKey)
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	} else {
		http.Error(w, "No public key configured", http.StatusNotFound)
	}
}

// serveApiPublicKey is an alias of servePublicKey kept for API compatibility.
func (ps *ProxyServer) serveApiPublicKey(w http.ResponseWriter, r *http.Request) {
	ps.servePublicKey(w, r)
}

func (ps *ProxyServer) serveStatus(w http.ResponseWriter, r *http.Request) {
	status := map[string]interface{}{
		"repo":     ps.Repository,
		"upstream": ps.UpstreamCaches,
		"mode":     "native",
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

// serveNarInfo resolves a "<hash>.narinfo" request by reconstructing it from
// the native OCI manifest tagged with the store hash, falling back to the
// upstream caches if the manifest can't satisfy the request.
func (ps *ProxyServer) serveNarInfo(w http.ResponseWriter, r *http.Request, path string) {
	storeHash := strings.TrimPrefix(path, "/")
	storeHash = strings.TrimSuffix(storeHash, ".narinfo")

	if err := ps.serveNativeNarinfo(w, r, storeHash); err == nil {
		return
	}

	// Fallback to upstream cache.
	upstreamPath := fmt.Sprintf("/%s.narinfo", storeHash)
	if ps.proxyUpstream(w, r, upstreamPath) {
		return
	}

	http.Error(w, "Narinfo Not Found", http.StatusNotFound)
}

// serveNar resolves a "/nar/<basename>" request by deriving the NAR blob's
// digest from the OCI manifest tagged with the store hash, streaming it from
// the registry if found, and otherwise falling back to the upstream caches.
func (ps *ProxyServer) serveNar(w http.ResponseWriter, r *http.Request, path string, method string) {
	narBasename := strings.TrimPrefix(path, "/nar/")
	contentType := "application/x-nix-nar"
	if strings.HasSuffix(narBasename, ".xz") {
		contentType = "application/x-xz"
	}

	tag := narBasename
	if idx := strings.Index(tag, ".nar"); idx != -1 {
		tag = tag[:idx]
	}

	p, err := ps.fetchPackage(r.Context(), tag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[aeroflare proxy] Warning: no manifest for %s: %v. Trying upstream...\n", narBasename, err)
	} else if err := ps.streamNar(r.Context(), w, p.nar, contentType, method); err == nil {
		return
	} else {
		fmt.Fprintf(os.Stderr, "[aeroflare proxy] Warning: failed to stream blob %s: %v. Trying upstream...\n", p.nar.Digest, err)
	}

	if ps.proxyUpstream(w, r, path) {
		return
	}

	http.Error(w, "NAR Not Found", http.StatusNotFound)
}

// serveNativeNarinfo reconstructs a narinfo file from the OCI manifest tagged
// with storeHash.
func (ps *ProxyServer) serveNativeNarinfo(w http.ResponseWriter, r *http.Request, storeHash string) error {
	p, err := ps.fetchPackage(r.Context(), storeHash)
	if err != nil {
		return err
	}
	labels, err := p.narinfoLabels()
	if err != nil {
		return err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "StorePath: %s\n", labels["aeroflare.storepath"])
	fmt.Fprintf(&b, "URL: %s\n", labels["aeroflare.url"])
	fmt.Fprintf(&b, "Compression: %s\n", labels["aeroflare.compression"])
	fmt.Fprintf(&b, "FileHash: %s\n", labels["aeroflare.filehash"])
	fmt.Fprintf(&b, "FileSize: %s\n", labels["aeroflare.filesize"])
	fmt.Fprintf(&b, "NarHash: %s\n", labels["aeroflare.narhash"])
	fmt.Fprintf(&b, "NarSize: %s\n", labels["aeroflare.narsize"])

	if rStr, ok := labels["aeroflare.references"]; ok && rStr != "" {
		fmt.Fprintf(&b, "References: %s\n", rStr)
	} else {
		// The trailing space is required: Nix reads a field's value at
		// (colon + 2), so a bare "References:\n" makes it consume the following
		// line (e.g. "Deriver: ...drv") as the references value and fail with
		// "'Deriver:' is too short to be a valid store path".
		b.WriteString("References: \n")
	}

	// Only emit Deriver when the annotation actually carries one. Nix parses the
	// value as a store-path basename, so a bare "Deriver:" (empty value) fails
	// with "'Deriver:' is too short to be a valid store path". Omitting the line
	// is how "no known deriver" is represented (e.g. fetched sources).
	if deriver := labels["aeroflare.deriver"]; deriver != "" {
		fmt.Fprintf(&b, "Deriver: %s\n", deriver)
	}

	if system, ok := labels["aeroflare.system"]; ok && system != "" {
		fmt.Fprintf(&b, "System: %s\n", system)
	}

	if sig, ok := labels["aeroflare.sig"]; ok && sig != "" {
		fmt.Fprintf(&b, "Sig: %s\n", sig)
	}

	body := []byte(b.String())
	w.Header().Set("Content-Type", "text/x-nix-narinfo")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
	return nil
}

// streamNar copies the NAR blob described by nar straight to w. The blob is
// opened before any header is written, so a failure here can still fall through
// to the upstream caches. If the copy is interrupted partway through, it panics
// with http.ErrAbortHandler rather than returning normally, so the client sees
// a broken connection instead of what looks like a complete-but-truncated body.
func (ps *ProxyServer) streamNar(ctx context.Context, w http.ResponseWriter, nar v1.Descriptor, contentType string, method string) error {
	writeHeaders := func() {
		w.Header().Set("Content-Type", contentType)
		if nar.Size > 0 {
			w.Header().Set("Content-Length", strconv.FormatInt(nar.Size, 10))
		}
		w.WriteHeader(http.StatusOK)
	}

	// A HEAD is answered from the manifest: the layer descriptor already
	// carries the size, so there is no reason to open the blob at all.
	if method == http.MethodHead {
		writeHeaders()
		return nil
	}

	layer, err := ps.puller.Layer(ctx, ps.repo.Digest(nar.Digest.String()))
	if err != nil {
		return err
	}
	rc, err := layer.Compressed()
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	writeHeaders()

	if _, err := io.Copy(w, rc); err != nil {
		fmt.Fprintf(os.Stderr, "[aeroflare proxy] Warning: stream interrupted for blob %s: %v\n", nar.Digest, err)
		// Abort the connection so the client sees a transfer error; returning
		// normally would cleanly terminate a truncated body, making it look
		// like a complete download.
		panic(http.ErrAbortHandler)
	}
	return nil
}

// proxyUpstream tries each configured upstream cache in order for
// upstreamPath, streaming and returning true on the first 200 OK response.
// Non-200 responses and network errors move on to the next upstream; it
// returns false once all upstreams have been tried (or none are configured).
func (ps *ProxyServer) proxyUpstream(w http.ResponseWriter, r *http.Request, upstreamPath string) bool {
	if len(ps.UpstreamCaches) == 0 {
		return false
	}

	for _, cache := range ps.UpstreamCaches {
		upstreamURL := fmt.Sprintf("%s%s", strings.TrimSuffix(cache, "/"), upstreamPath)

		req, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, nil)
		if err != nil {
			continue // Try next upstream
		}
		req.Header.Set("User-Agent", "aeroflare/1.0")

		resp, err := ps.upstream.Do(req)
		if err != nil {
			continue // Try next upstream on network error
		}

		// If it's a 404, we might want to try the next cache.
		// If it's a 200 OK, serve it and return.
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			continue
		}

		defer func() { _ = resp.Body.Close() }()

		for k, vv := range resp.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, err = io.Copy(w, resp.Body)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				fmt.Fprintf(os.Stderr, "[aeroflare proxy] Warning: stream interrupted for upstream path %s: %v\n", upstreamPath, err)
			}
			// Abort instead of cleanly terminating a truncated body.
			panic(http.ErrAbortHandler)
		}
		return true
	}

	return false
}
