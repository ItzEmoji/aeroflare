package push

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/itzemoji/aeroflare/internal/backend"
	"github.com/itzemoji/aeroflare/pkg/oci"
	"github.com/itzemoji/aeroflare/pkg/prepare/cache"
	"github.com/itzemoji/aeroflare/pkg/prepare/compress"
	"github.com/itzemoji/aeroflare/pkg/prepare/narinfo"
	"github.com/itzemoji/aeroflare/pkg/prepare/prepare"
	"github.com/itzemoji/aeroflare/pkg/prepare/signing"
	"github.com/itzemoji/aeroflare/pkg/proxy"

	"strconv"

	"github.com/google/go-containerregistry/pkg/v1/types"
	"golang.org/x/sync/errgroup"
)

// PushConfig holds the resolved settings for a single push invocation:
// which store paths to push and how to prepare/upload them.
type PushConfig struct {
	TargetPaths []string
	Compression string
	CacheURL    string
	Workers     int
	PrepareRefs bool
	SigningKey  string
	KeepFiles   bool
	ForcePush   bool
	Verbosity   int
}

// Target is an explicit push destination. It carries everything the push
// pipeline needs to authenticate, so this package never consults viper, the
// environment, or the OS keyring itself.
type Target struct {
	Registry   string
	Repository string

	// Token is a ready-to-use registry bearer token. Ignored when TokenSource
	// is set.
	Token string

	// TokenSource, when non-nil, is called to obtain a bearer token, and is
	// called again before each upload chunk. Registry bearer tokens are
	// short-lived, so a long push must be able to refresh; a static Token
	// would expire partway through. Returning "" means no credential is
	// available.
	//
	// The CLI supplies cmdutil.RegistryToken here.
	TokenSource func() string

	// OverrideToken, when non-empty, is used verbatim as the registry bearer
	// token when reading cache metadata, skipping token exchange. The CLI
	// supplies cmdutil.RegistryOverrideToken here.
	OverrideToken string
}

// token returns a fresh bearer token for the target, preferring TokenSource so
// that long-running pushes pick up a refreshed credential.
func (t Target) token() string {
	if t.TokenSource != nil {
		return t.TokenSource()
	}
	return t.Token
}

// PushResult summarizes a completed RunPushTo call.
type PushResult struct {
	Uploaded        int
	SkippedUpstream int
	Roots           int
	Failed          []string
}

// ParseConfig gathers store paths from a positional storePath, an --input
// file (one path per line, "#" comments ignored), and piped stdin (skipped
// if stdin is an interactive terminal), then appends any trailing args.
// It returns an error only if no paths were found from any source.
func ParseConfig(args []string, storePath string, inputFile string, stdin io.Reader) (*PushConfig, error) {
	var targetPaths []string
	if storePath != "" {
		targetPaths = append(targetPaths, storePath)
	}
	if inputFile != "" {
		filePaths, err := prepare.ParseInputFile(inputFile)
		if err != nil {
			return nil, err
		}
		targetPaths = append(targetPaths, filePaths...)
	}

	if stdin != nil {
		shouldRead := true
		if f, ok := stdin.(*os.File); ok {
			stat, err := f.Stat()
			if err != nil || (stat.Mode()&os.ModeCharDevice) != 0 {
				shouldRead = false
			}
		}

		if shouldRead {
			scanner := bufio.NewScanner(stdin)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line != "" && !strings.HasPrefix(line, "#") {
					targetPaths = append(targetPaths, line)
				}
			}
			if err := scanner.Err(); err != nil {
				return nil, err
			}
		}
	}

	if len(targetPaths) == 0 && len(args) == 0 {
		return nil, errors.New("no store paths found: provide --store-path, --input, or pipe paths via stdin")
	}
	targetPaths = append(targetPaths, args...)

	return &PushConfig{
		TargetPaths: targetPaths,
	}, nil
}

// PushPlan is the result of preflighting a PushConfig: the paths that still
// need to be pushed, plus how many were filtered out as already cached.
type PushPlan struct {
	Config        *PushConfig
	FilteredPaths []string
	SkippedCount  int
}

// Preflight checks which paths actually need to be pushed.
// For now, it simply copies the paths, but will be integrated with cache index logic.
func Preflight(cfg *PushConfig) (*PushPlan, error) {
	// Note: To keep tasks small, we just stub this out first.
	// The actual cache checking logic from cmd/push.go will be moved here in Task 4.
	return &PushPlan{
		Config:        cfg,
		FilteredPaths: cfg.TargetPaths,
		SkippedCount:  0,
	}, nil
}

// SummaryFields describes what a push is about to do, as label/value pairs
// ready to hand to Reporter.Summary. It renders nothing itself.
func SummaryFields(plan *PushPlan) [][2]string {
	fields := [][2]string{
		{"Total paths", strconv.Itoa(len(plan.Config.TargetPaths))},
	}
	if plan.SkippedCount > 0 {
		fields = append(fields, [2]string{"Already cached", strconv.Itoa(plan.SkippedCount)})
	}
	return append(fields, [2]string{"To be pushed", strconv.Itoa(len(plan.FilteredPaths))})
}

// RunPushTo executes a PushPlan end to end against an explicit target: it
// authenticates against the registry, filters out paths already present in the
// cache index or upstream cache (unless ForcePush is set), then prepares and
// uploads the remaining paths in fixed-size chunks. Receipts are flushed to the
// cache backend after each chunk so an interrupted push keeps whatever it
// already uploaded, and per-path upload failures are reported at the end rather
// than aborting the whole run (a chunk only aborts outright if every upload in
// it fails).
//
// Progress is reported through reporter; nothing is written to stdout. The
// caller supplies the target, so this package resolves nothing from viper or
// the environment.
func RunPushTo(plan *PushPlan, target Target, reporter Reporter) (*PushResult, error) {
	startTime := time.Now()
	var totalUploaded int
	var skippedUpstream int

	registry, repository := target.Registry, target.Repository
	ociToken := target.token()
	if ociToken == "" {
		return nil, errors.New("authentication token missing for registry")
	}

	compType, err := compress.ParseType(plan.Config.Compression)
	if err != nil {
		return nil, err
	}

	var signKey *signing.PrivateKey
	if plan.Config.SigningKey != "" {
		signKey, err = signing.LoadPrivateKey(plan.Config.SigningKey)
		if err != nil {
			return nil, fmt.Errorf("error loading key: %v", err)
		}
	}

	// Create a temporary directory if files should not be kept
	outputDir, err := os.MkdirTemp("", "aeroflare-push-*")
	if err != nil {
		return nil, fmt.Errorf("error creating temporary directory: %v", err)
	}

	if !plan.Config.KeepFiles {
		defer func() { _ = os.RemoveAll(outputDir) }()
	} else {
		reporter.Info(fmt.Sprintf("Generated files will be kept in: %s", outputDir))
	}

	var upstreamURLs []string
	if plan.Config.CacheURL != "" {
		upstreamURLs = []string{plan.Config.CacheURL}
	}

	cfg := &prepare.Config{
		OutputDir:          outputDir,
		Compression:        compType,
		CacheURLs:          upstreamURLs,
		Workers:            plan.Config.Workers,
		PrepareMissingRefs: plan.Config.PrepareRefs,
		SigningKey:         signKey,
	}

	ctx := context.Background()

	var filteredPaths []string
	if plan.Config.ForcePush {
		filteredPaths = plan.FilteredPaths
	} else {
		filteredPaths = plan.FilteredPaths

		if len(filteredPaths) > 0 && plan.Config.CacheURL != "" {
			c := cache.New(plan.Config.CacheURL, cache.WithMaxConns(plan.Config.Workers))

			var hashes []string
			for _, p := range filteredPaths {
				basename := filepath.Base(p)
				parts := strings.SplitN(basename, "-", 2)
				if len(parts) >= 2 {
					hashes = append(hashes, parts[0])
				}
			}

			existsMap, err := c.ExistsBatch(ctx, hashes, plan.Config.Workers)
			if err != nil {
				reporter.Warn(fmt.Sprintf("upstream cache check failed: %v", err))
			} else {
				var trulyFiltered []string
				for _, p := range filteredPaths {
					basename := filepath.Base(p)
					parts := strings.SplitN(basename, "-", 2)
					if len(parts) >= 2 && existsMap[parts[0]] {
						reporter.SkippedUpstream(p)
						skippedUpstream++
						continue
					}
					trulyFiltered = append(trulyFiltered, p)
				}
				filteredPaths = trulyFiltered
			}
		}
	}

	if len(filteredPaths) == 0 {
		reporter.Info("No new paths to push.")
		return &PushResult{}, nil
	}
	tokenMgr := proxy.NewTokenManager(registry, repository, target.Token)
	tokenMgr.SetOverrideToken(target.OverrideToken)
	_, configAnnotations, _ := proxy.BootstrapConfigWithAnnotations(ctx, nil, registry, repository, tokenMgr)

	var totalReceipts []backend.PushReceipt
	var failedPaths []string
	var excludedPaths []string

	chunkSize := 100
	for i := 0; i < len(filteredPaths); i += chunkSize {
		end := i + chunkSize
		if end > len(filteredPaths) {
			end = len(filteredPaths)
		}
		chunk := filteredPaths[i:end]

		numChunks := (len(filteredPaths) + chunkSize - 1) / chunkSize
		currentChunk := (i / chunkSize) + 1
		reporter.Info(fmt.Sprintf("\n--- Processing chunk %d/%d ---\n", currentChunk, numChunks))

		var results []*prepare.Result
		reporter.Step(1, 3, "Preparing (Generating NAR and narinfo files)")

		if len(chunk) == 1 {
			var res *prepare.Result
			var prepErr error
			res, prepErr = prepare.Prepare(ctx, chunk[0], cfg)
			if prepErr != nil {
				return nil, fmt.Errorf("error during preparation: %v", prepErr)
			}
			results = append(results, res)
		} else {
			res, prepErr := prepare.PrepareBatch(ctx, chunk, cfg)
			if prepErr != nil {
				return nil, fmt.Errorf("error during batch preparation: %v", prepErr)
			}
			results = res
		}

		if plan.Config.Verbosity < 1 {
			reporter.Success(fmt.Sprintf("Prepared %d packages", len(results)))
		}

		seenPaths := make(map[string]bool)

		type pushTask struct {
			r      *prepare.Result
			isRoot bool
		}
		var tasks []pushTask

		// collect flattens each prepared result and its transitive
		// MissingRefResults into a deduplicated task list, so a dependency
		// shared by multiple roots is only uploaded once.
		var collect func(r *prepare.Result, isRoot bool)
		collect = func(r *prepare.Result, isRoot bool) {
			if seenPaths[r.StorePath] {
				return
			}
			seenPaths[r.StorePath] = true

			tasks = append(tasks, pushTask{r: r, isRoot: isRoot})

			for _, missingRef := range r.MissingRefResults {
				collect(missingRef, false)
			}
		}

		for _, r := range results {
			collect(r, true)
		}

		reporter.Step(2, 3, fmt.Sprintf("Uploading %d packages to OCI registry", len(tasks)))

		// Registry bearer tokens are short-lived; refresh per chunk so long
		// pushes don't fail partway with auth errors.
		if t := target.token(); t != "" {
			ociToken = t
		}

		// Build a single authenticated pusher for the whole chunk so all
		// concurrent layer uploads share one registry auth handshake instead
		// of each repeating the /v2/ 401 challenge and token exchange.
		pusher, repo, err := oci.NewLayerPusher(registry, repository, ociToken)
		if err != nil {
			return nil, fmt.Errorf("failed to create registry pusher: %v", err)
		}

		var mu sync.Mutex
		receiptsByPath := make(map[string]backend.PushReceipt)
		var chunkFailed []string
		eg, _ := errgroup.WithContext(ctx)
		eg.SetLimit(plan.Config.Workers)

		for _, t := range tasks {
			t := t // capture loop variable
			eg.Go(func() error {
				r := t.r
				isRoot := t.isRoot

				fail := func(stage string, err error) {
					mu.Lock()
					reporter.Failed(r.StorePath, stage, err)
					chunkFailed = append(chunkFailed, r.StorePath)
					mu.Unlock()
				}

				narStat, err := os.Stat(r.NarPath)
				if err != nil {
					fail("failed to stat NAR file", err)
					return nil
				}

				// Parse narinfo once to avoid disk reads for hashing
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

				// Create the layer ONCE for brutal speed without even hashing the file!
				layer, narDigest, err := oci.NewLayerFast(r.NarPath, types.MediaType("application/vnd.aeroflare.nar.v1+"+ni.Compression), ni)
				if err != nil {
					fail("failed to create NAR layer", err)
					return nil
				}

				// Simply push the layer and collect receipt.
				if err := pusher.Upload(ctx, repo, layer); err != nil {
					fail("failed to push NAR layer", err)
					return nil
				}

				mu.Lock()
				totalUploaded++
				if plan.Config.Verbosity >= 1 {
					reporter.Uploaded(r.StorePath)
				}

				receiptsByPath[r.StorePath] = backend.PushReceipt{
					StorePath:   r.StorePath,
					NarinfoPath: r.NarinfoPath,
					NarDigest:   narDigest,
					NarSize:     narStat.Size(),
					NarPath:     r.NarPath,
					Compression: ni.Compression,
					IsRoot:      isRoot,
				}
				mu.Unlock()

				return nil
			})
		}
		if err := eg.Wait(); err != nil {
			return nil, err
		}

		failedPaths = append(failedPaths, chunkFailed...)
		if len(chunkFailed) == len(tasks) && len(tasks) > 0 {
			return nil, fmt.Errorf("all %d uploads in chunk %d/%d failed (first: %s); aborting push", len(tasks), currentChunk, numChunks, chunkFailed[0])
		}

		// Only index store paths whose full closure was uploaded; a narinfo
		// referencing missing paths would break substitution for consumers.
		chunkReceipts, chunkExcluded := completeReceipts(results, receiptsByPath)
		excludedPaths = append(excludedPaths, chunkExcluded...)

		if plan.Config.Verbosity < 1 {
			reporter.Success(fmt.Sprintf("%d packages uploaded", len(chunkReceipts)))
		}

		// Flush receipts per chunk so an interrupted push keeps what it
		// already uploaded instead of orphaning every blob.
		if len(chunkReceipts) > 0 {
			backend := backend.NewCacheBackend(backend.BackendConfig{
				Registry:          registry,
				Repository:        repository,
				Token:             ociToken,
				PubKeyPath:        plan.Config.SigningKey,
				ConfigAnnotations: configAnnotations,
				Workers:           plan.Config.Workers,
			})
			if err := backend.PushReceipts(ctx, chunkReceipts); err != nil {
				return nil, fmt.Errorf("backend push failed: %v", err)
			}
			totalReceipts = append(totalReceipts, chunkReceipts...)
		}
	}

	reporter.Step(3, 3, "Cache backend updated")

	duration := time.Since(startTime)

	rootsUploaded := 0
	for _, r := range totalReceipts {
		if r.IsRoot {
			rootsUploaded++
		}
	}

	reporter.Summary("Done", [][2]string{
		{"Packages uploaded", strconv.Itoa(totalUploaded)},
		{"GC roots", strconv.Itoa(rootsUploaded)},
		{"Duration", duration.Round(time.Millisecond).String()},
	})

	result := &PushResult{
		Uploaded:        totalUploaded,
		SkippedUpstream: skippedUpstream,
		Roots:           rootsUploaded,
		Failed:          failedPaths,
	}
	if len(failedPaths) > 0 {
		return result, fmt.Errorf("%d upload(s) failed (%d dependent path(s) left unindexed to keep closures complete); re-run push to retry", len(failedPaths), len(excludedPaths))
	}

	return result, nil
}
