package push_test

import (
	"fmt"
	"log"

	"github.com/itzemoji/aeroflare/pkg/push"
)

// silentReporter discards every progress event. Because push never writes to
// stdout itself, this is all it takes to embed the pipeline in a program that
// wants to render its own output (or none at all).
type silentReporter struct{}

func (silentReporter) Step(step, total int, msg string)          {}
func (silentReporter) Uploaded(storePath string)                 {}
func (silentReporter) SkippedUpstream(storePath string)          {}
func (silentReporter) Success(msg string)                        {}
func (silentReporter) Summary(title string, fields [][2]string)  {}
func (silentReporter) Failed(storePath, stage string, err error) {}
func (silentReporter) Warn(msg string)                           {}
func (silentReporter) Info(msg string)                           {}

// Pushing store paths to a registry: build a plan, name a target, supply a
// reporter.
func ExampleRunPushTo() {
	plan, err := push.Preflight(&push.PushConfig{
		TargetPaths: []string{"/nix/store/0nlp2xwzavr9dyrsdhcgnq2h4qxsi8bp-hello-2.12.1"},
		Compression: "zstd",
		Workers:     50,
		PrepareRefs: true,
		// Paths this upstream cache already serves are not worth uploading.
		CacheURL: "https://cache.nixos.org",
	})
	if err != nil {
		log.Fatal(err)
	}

	target := push.Target{
		Registry:   "ghcr.io",
		Repository: "itzemoji/aeroflare-cache",
		// A source rather than a string: registry bearer tokens expire, and the
		// pipeline calls this again before each chunk so a long push does not
		// die halfway through.
		TokenSource: func() string { return freshBearerToken() },
	}

	result, err := push.RunPushTo(plan, target, silentReporter{})
	if err != nil {
		log.Fatal(err)
	}

	// A push finishes even when individual paths fail; they are collected here
	// rather than aborting the run.
	fmt.Printf("uploaded %d, skipped %d, failed %d\n",
		result.Uploaded, result.SkippedUpstream, len(result.Failed))
}

// Preparing once and pushing to several registries: the NARs are generated a
// single time and reused for each target.
func ExamplePreparedSet_PushTo() {
	prepared, err := push.Prepare(
		[]string{"/nix/store/0nlp2xwzavr9dyrsdhcgnq2h4qxsi8bp-hello-2.12.1"},
		push.PrepareConfig{Compression: "zstd", Workers: 50},
	)
	if err != nil {
		log.Fatal(err)
	}
	defer prepared.Cleanup()

	for _, repo := range []string{"itzemoji/cache-eu", "itzemoji/cache-us"} {
		target := push.Target{
			Registry:    "ghcr.io",
			Repository:  repo,
			TokenSource: func() string { return freshBearerToken() },
		}
		if _, err := prepared.PushTo(target, silentReporter{}); err != nil {
			log.Printf("push to %s failed: %v", repo, err)
		}
	}
}

// freshBearerToken stands in for the caller's own credential lookup; the CLI
// uses cmdutil.RegistryToken.
func freshBearerToken() string { return "" }
