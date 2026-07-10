package ci

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/itzemoji/aeroflare/internal/prepare/cache"
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

// nothingToPushLine renders the all-roots-already-upstream outcome.
func nothingToPushLine(paths int) string {
	return fmt.Sprintf("all %d build outputs are already upstream, nothing to push", paths)
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

	// One group, consulted twice: once for the build outputs below, and again
	// inside prepare for their transitive references. A path skipped as a root
	// is therefore also skipped as somebody's dependency.
	var checker upstreamChecker
	if len(upstreams) > 0 {
		group := cache.NewGroup(upstreams, cache.WithMaxConns(spec.Workers))
		group.SetWarnWriter(w)
		checker = group
	}

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
		up = strings.Join(upstreams, ", ")
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

	roots, skipped, err := filterRoots(context.Background(), union, checker, spec.Workers)
	if err != nil {
		fmt.Fprintf(w, "✗ upstream check: %v\n", err)
		fmt.Fprintf(w, "\n%s\n", summaryLine(buildsTotal, buildsOK, len(spec.Caches), 0, 0))
		return false
	}
	if skipped > 0 && len(roots) > 0 {
		fmt.Fprintf(w, "skip     %d build outputs already upstream\n", skipped)
	}
	if len(roots) == 0 {
		fmt.Fprintf(w, "%s\n", nothingToPushLine(len(union)))
		fmt.Fprintf(w, "\n%s\n", summaryLine(buildsTotal, buildsOK, 0, 0, 0))
		return buildsOK == buildsTotal
	}

	prepared, err := push.Prepare(roots, push.PrepareConfig{
		Compression: spec.Compression,
		Workers:     spec.Workers,
		SigningKey:  keyPath,
		CacheURLs:   upstreams,
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
	for _, c := range spec.Caches {
		token := ResolveToken(c.Registry)
		if token == "" {
			fmt.Fprintf(w, "✗ push    → %s   auth: no token (set %s)\n", c.Raw, TokenEnvVar(c.Registry))
			continue
		}
		reporter := NewPlainReporter(w, "  ")
		target := push.Target{Registry: c.Registry, Repository: c.Repository, Token: token}
		res, err := prepared.PushTo(target, reporter)
		if err != nil {
			fmt.Fprintf(w, "✗ push    → %s   %v\n", c.Raw, err)
			continue
		}
		pushesOK++
		fmt.Fprintf(w, "✓ push    → %s   (%d pushed)\n", c.Raw, res.Uploaded)
	}

	fmt.Fprintf(w, "\n%s\n", summaryLine(buildsTotal, buildsOK, pushesTotal, pushesOK, pathCount))
	return buildsOK == buildsTotal && pushesOK == pushesTotal
}
