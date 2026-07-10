package ci

import (
	"context"
	"fmt"
	"io"

	"github.com/itzemoji/aeroflare/internal/proxy"
	"github.com/itzemoji/aeroflare/internal/push"
)

// summaryLine renders the final one-line roll-up.
func summaryLine(buildsTotal, buildsOK, pushesTotal, pushesOK, paths int) string {
	status := "OK"
	if buildsOK != buildsTotal || pushesOK != pushesTotal {
		status = "FAILED"
	}
	return fmt.Sprintf("summary: builds %d/%d  pushes %d/%d  paths %d  —  %s",
		buildsOK, buildsTotal, pushesOK, pushesTotal, paths, status)
}

// Run executes the smart pipeline: start a proxy substituter at the primary cache,
// build every installable through it, prepare the full closure once, and upload it
// to every cache. Returns true iff every build and push succeeded.
func Run(spec RunSpec, w io.Writer) bool {
	fmt.Fprintf(w, "aeroflare-ci: %d builds, %d caches\n", len(spec.Builds), len(spec.Caches))

	keyPath, cleanup, err := ResolveSigningKey(spec.SigningKey)
	if err != nil {
		fmt.Fprintf(w, "✗ signing-key: %v\n", err)
		return false
	}
	defer cleanup()

	primary := spec.Caches[0]
	token0 := ResolveToken(primary.Registry)
	if token0 == "" {
		fmt.Fprintf(w, "✗ no token for primary cache %s (set %s)\n", primary.Raw, TokenEnvVar(primary.Registry))
		return false
	}

	upstreams := spec.UpstreamCaches

	// The proxy is a build-only substituter: it accelerates `nix build` by
	// serving already-cached paths from the primary cache. It has no role in
	// prepare (which reads local store paths) or push (which uploads directly to
	// the registry), so its context is scoped to the build loop and torn down
	// before prepare/upload. defer is a safety net for early returns; the happy
	// path stops it explicitly after builds.
	buildCtx, stopProxy := context.WithCancel(context.Background())
	defer stopProxy()

	port, err := proxy.StartProxy(buildCtx, 0, "127.0.0.1", primary.Registry, primary.Repository, upstreams, token0)
	if err != nil {
		fmt.Fprintf(w, "✗ proxy: %v\n", err)
		return false
	}
	up := "none"
	if len(upstreams) > 0 {
		up = upstreams[0]
	}
	fmt.Fprintf(w, "proxy 127.0.0.1:%d → %s  (upstream: %s)\n\n", port, primary.Raw, up)

	buildsTotal := len(spec.Builds)
	buildsOK := 0
	var union []string
	for _, installable := range spec.Builds {
		paths, err := BuildInstallable(installable, port)
		if err != nil {
			fmt.Fprintf(w, "✗ build   %s   %v\n", installable, err)
			continue
		}
		buildsOK++
		fmt.Fprintf(w, "✓ build   %s   (%d paths)\n", installable, len(paths))
		union = append(union, paths...)
	}
	// Builds are done — tear down the proxy so it isn't serving during
	// prepare/upload, which don't use it.
	stopProxy()
	union = dedupPaths(union)
	if len(union) == 0 {
		fmt.Fprintf(w, "\n%s\n", summaryLine(buildsTotal, buildsOK, 0, 0, 0))
		return false
	}

	prepared, err := push.Prepare(union, push.PrepareConfig{
		Compression: spec.Compression,
		Workers:     spec.Workers,
		SigningKey:  keyPath,
		CacheURLs:   nil,
	})
	if err != nil {
		fmt.Fprintf(w, "✗ prepare: %v\n", err)
		fmt.Fprintf(w, "\n%s\n", summaryLine(buildsTotal, buildsOK, len(spec.Caches), 0, 0))
		return false
	}
	defer prepared.Cleanup()
	pathCount := prepared.PathCount()
	fmt.Fprintf(w, "prepare  %d store paths (full closure)\n", pathCount)

	pushesTotal := len(spec.Caches)
	pushesOK := 0
	for _, cache := range spec.Caches {
		token := ResolveToken(cache.Registry)
		if token == "" {
			fmt.Fprintf(w, "✗ push    → %s   auth: no token (set %s)\n", cache.Raw, TokenEnvVar(cache.Registry))
			continue
		}
		reporter := NewPlainReporter(w, "  ")
		target := push.Target{Registry: cache.Registry, Repository: cache.Repository, Token: token}
		res, err := prepared.PushTo(target, reporter)
		if err != nil {
			fmt.Fprintf(w, "✗ push    → %s   %v\n", cache.Raw, err)
			continue
		}
		pushesOK++
		fmt.Fprintf(w, "✓ push    → %s   (%d pushed)\n", cache.Raw, res.Uploaded)
	}

	fmt.Fprintf(w, "\n%s\n", summaryLine(buildsTotal, buildsOK, pushesTotal, pushesOK, pathCount))
	return buildsOK == buildsTotal && pushesOK == pushesTotal
}
