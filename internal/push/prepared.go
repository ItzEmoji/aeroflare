package push

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/itzemoji/aeroflare/internal/backend"
	"github.com/itzemoji/aeroflare/internal/oci"
	"github.com/itzemoji/aeroflare/internal/prepare/compress"
	"github.com/itzemoji/aeroflare/internal/prepare/narinfo"
	"github.com/itzemoji/aeroflare/internal/prepare/prepare"
	"github.com/itzemoji/aeroflare/internal/prepare/signing"
	"github.com/itzemoji/aeroflare/internal/proxy"

	"github.com/google/go-containerregistry/pkg/v1/types"
	"golang.org/x/sync/errgroup"
)

// PrepareConfig configures a one-time full-closure preparation.
type PrepareConfig struct {
	Compression string // zstd, xz, gzip, none
	Workers     int
	SigningKey  string   // path to a Nix signing key, or "" for unsigned
	CacheURLs   []string // empty = prepare the full closure (no upstream filtering)
	KeepFiles   bool
}

// preparedTask is one NAR to upload, flagged as a closure root or a dependency.
type preparedTask struct {
	r      *prepare.Result
	isRoot bool
}

// PreparedSet is a generated-once set of NAR/narinfo artifacts for a full closure,
// ready to be uploaded to one or more caches.
type PreparedSet struct {
	results    []*prepare.Result
	tasks      []preparedTask
	outputDir  string
	keep       bool
	signingKey string
	workers    int
}

// flattenTasks flattens results and their transitive MissingRefResults into a
// deduplicated task list, so a dependency shared by multiple roots appears once.
func flattenTasks(results []*prepare.Result) []preparedTask {
	seen := make(map[string]bool)
	var tasks []preparedTask
	var collect func(r *prepare.Result, isRoot bool)
	collect = func(r *prepare.Result, isRoot bool) {
		if seen[r.StorePath] {
			return
		}
		seen[r.StorePath] = true
		tasks = append(tasks, preparedTask{r: r, isRoot: isRoot})
		for _, mr := range r.MissingRefResults {
			collect(mr, false)
		}
	}
	for _, r := range results {
		collect(r, true)
	}
	return tasks
}

// Prepare walks the closure of paths and generates all NAR/narinfo artifacts once.
// With cfg.CacheURLs empty the entire closure is prepared (no upstream filtering).
func Prepare(paths []string, cfg PrepareConfig) (*PreparedSet, error) {
	compType, err := compress.ParseType(cfg.Compression)
	if err != nil {
		return nil, err
	}

	var signKey *signing.PrivateKey
	if cfg.SigningKey != "" {
		signKey, err = signing.LoadPrivateKey(cfg.SigningKey)
		if err != nil {
			return nil, fmt.Errorf("error loading key: %v", err)
		}
	}

	outputDir, err := os.MkdirTemp("", "aeroflare-push-*")
	if err != nil {
		return nil, fmt.Errorf("error creating temporary directory: %v", err)
	}

	pcfg := &prepare.Config{
		OutputDir:          outputDir,
		Compression:        compType,
		CacheURLs:          cfg.CacheURLs,
		Workers:            cfg.Workers,
		PrepareMissingRefs: true,
		SigningKey:         signKey,
	}

	ctx := context.Background()
	var results []*prepare.Result
	if len(paths) == 1 {
		res, prepErr := prepare.Prepare(ctx, paths[0], pcfg)
		if prepErr != nil {
			_ = os.RemoveAll(outputDir)
			return nil, fmt.Errorf("error during preparation: %v", prepErr)
		}
		results = []*prepare.Result{res}
	} else {
		res, prepErr := prepare.PrepareBatch(ctx, paths, pcfg)
		if prepErr != nil {
			_ = os.RemoveAll(outputDir)
			return nil, fmt.Errorf("error during batch preparation: %v", prepErr)
		}
		results = res
	}

	return &PreparedSet{
		results:    results,
		tasks:      flattenTasks(results),
		outputDir:  outputDir,
		keep:       cfg.KeepFiles,
		signingKey: cfg.SigningKey,
		workers:    cfg.Workers,
	}, nil
}

// PathCount reports how many store paths (roots + closure) are in the set.
func (ps *PreparedSet) PathCount() int { return len(ps.tasks) }

// Cleanup removes the temp directory unless KeepFiles was set.
func (ps *PreparedSet) Cleanup() {
	if ps == nil || ps.keep || ps.outputDir == "" {
		return
	}
	_ = os.RemoveAll(ps.outputDir)
}

// uploadOutcome is the result of uploading a task set to one registry.
type uploadOutcome struct {
	receiptsByPath map[string]backend.PushReceipt
	failed         []string
	uploaded       int
}

// uploadTaskSet uploads every task concurrently to the target registry, reporting
// each success via reporter.Uploaded. It never aborts on individual failures.
func uploadTaskSet(ctx context.Context, tasks []preparedTask, registry, repository, token string, reporter Reporter, workers int) (uploadOutcome, error) {
	pusher, repo, err := oci.NewLayerPusher(registry, repository, token)
	if err != nil {
		return uploadOutcome{}, fmt.Errorf("failed to create registry pusher: %v", err)
	}

	var mu sync.Mutex
	receiptsByPath := make(map[string]backend.PushReceipt)
	var failed []string
	uploaded := 0

	eg, _ := errgroup.WithContext(ctx)
	eg.SetLimit(workers)
	for _, t := range tasks {
		t := t
		eg.Go(func() error {
			r := t.r
			fail := func(stage string, err error) {
				mu.Lock()
				fmt.Printf("ERROR: %s (%s): %v\n", stage, r.StorePath, err)
				failed = append(failed, r.StorePath)
				mu.Unlock()
			}

			narStat, err := os.Stat(r.NarPath)
			if err != nil {
				fail("failed to stat NAR file", err)
				return nil
			}
			narinfoData, err := os.ReadFile(r.NarinfoPath)
			if err != nil {
				fail("failed to read narinfo", err)
				return nil
			}
			ni, err := narinfo.Parse(string(narinfoData))
			if err != nil {
				fail("failed to parse narinfo", err)
				return nil
			}
			layer, narDigest, err := oci.NewLayerFast(r.NarPath, types.MediaType("application/vnd.aeroflare.nar.v1+"+ni.Compression), ni)
			if err != nil {
				fail("failed to create NAR layer", err)
				return nil
			}
			if err := pusher.Upload(ctx, repo, layer); err != nil {
				fail("failed to push NAR layer", err)
				return nil
			}

			mu.Lock()
			uploaded++
			reporter.Uploaded(r.StorePath)
			receiptsByPath[r.StorePath] = backend.PushReceipt{
				StorePath:   r.StorePath,
				NarinfoPath: r.NarinfoPath,
				NarDigest:   narDigest,
				NarSize:     narStat.Size(),
				NarPath:     r.NarPath,
				Compression: ni.Compression,
				IsRoot:      t.isRoot,
			}
			mu.Unlock()
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return uploadOutcome{}, err
	}
	return uploadOutcome{receiptsByPath: receiptsByPath, failed: failed, uploaded: uploaded}, nil
}

// PushTo uploads the prepared set to a single cache. The registry dedups any blob
// it already stores, so re-pushing an unchanged closure is cheap. Safe to call
// multiple times with different targets.
func (ps *PreparedSet) PushTo(target Target, reporter Reporter) (*PushResult, error) {
	ctx := context.Background()

	ociToken := oci.GetToken(target.Registry, target.Repository, target.Token)
	if ociToken == "" {
		return nil, errors.New("authentication token missing for registry")
	}

	tokenMgr := proxy.NewTokenManager(target.Registry, target.Repository, target.Token)
	_, configAnnotations, _ := proxy.BootstrapConfigWithAnnotations(ctx, nil, target.Registry, target.Repository, tokenMgr)

	reporter.Step(1, 2, fmt.Sprintf("Uploading %d packages to OCI registry", len(ps.tasks)))
	out, err := uploadTaskSet(ctx, ps.tasks, target.Registry, target.Repository, ociToken, reporter, ps.workers)
	if err != nil {
		return nil, err
	}

	receipts, _ := completeReceipts(ps.results, out.receiptsByPath)
	rootsUploaded := 0
	for _, r := range receipts {
		if r.IsRoot {
			rootsUploaded++
		}
	}

	if len(receipts) > 0 {
		b := backend.NewCacheBackend(backend.BackendConfig{
			Registry:          target.Registry,
			Repository:        target.Repository,
			Token:             ociToken,
			PubKeyPath:        ps.signingKey,
			ConfigAnnotations: configAnnotations,
			Workers:           ps.workers,
		})
		if err := b.PushReceipts(ctx, receipts); err != nil {
			return nil, fmt.Errorf("backend push failed: %v", err)
		}
	}
	reporter.Step(2, 2, "Cache backend updated")

	result := &PushResult{Uploaded: out.uploaded, Roots: rootsUploaded, Failed: out.failed}
	if len(out.failed) > 0 {
		return result, fmt.Errorf("%d upload(s) failed; re-run to retry", len(out.failed))
	}
	return result, nil
}
