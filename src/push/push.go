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

	network "aeroflare/src"
	"aeroflare/src/prepare/cache"
	"aeroflare/src/prepare/compress"
	"aeroflare/src/prepare/narinfo"
	"aeroflare/src/prepare/prepare"
	"aeroflare/src/prepare/signing"
	"aeroflare/src/proxy"
	"aeroflare/src/ui"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"golang.org/x/sync/errgroup"
	"strconv"
)

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

// DisplaySummary prints what is about to happen.
func DisplaySummary(plan *PushPlan) {
	fields := []ui.BoxField{
		{Label: "Total paths", Value: strconv.Itoa(len(plan.Config.TargetPaths))},
	}
	if plan.SkippedCount > 0 {
		fields = append(fields, ui.BoxField{Label: "Already cached", Value: strconv.Itoa(plan.SkippedCount)})
	}
	fields = append(fields, ui.BoxField{Label: "To be pushed", Value: strconv.Itoa(len(plan.FilteredPaths))})
	
	ui.PrintSummaryBox("Push Summary", fields)
}

func RunPush(plan *PushPlan) error {
	startTime := time.Now()
	var totalUploaded int

	// Fetch registry and token
	registry, repository := network.GetRegistryAndRepository()
	ociToken := network.GetToken(registry, repository, "")
	if ociToken == "" {
		return errors.New("authentication token missing (oci_token, GITHUB_TOKEN or GH_TOKEN)")
	}

	compType, err := compress.ParseType(plan.Config.Compression)
	if err != nil {
		return err
	}

	var signKey *signing.PrivateKey
	if plan.Config.SigningKey != "" {
		signKey, err = signing.LoadPrivateKey(plan.Config.SigningKey)
		if err != nil {
			return fmt.Errorf("error loading key: %v", err)
		}
	}

	// Create a temporary directory if files should not be kept
	outputDir, err := os.MkdirTemp("", "aeroflare-push-*")
	if err != nil {
		return fmt.Errorf("error creating temporary directory: %v", err)
	}

	if !plan.Config.KeepFiles {
		defer func() { _ = os.RemoveAll(outputDir) }()
	} else {
		fmt.Printf("Generated files will be kept in: %s\n", outputDir)
	}

	cfg := &prepare.Config{
		OutputDir:          outputDir,
		Compression:        compType,
		CacheURL:           plan.Config.CacheURL,
		Workers:            plan.Config.Workers,
		PrepareMissingRefs: plan.Config.PrepareRefs,
		SigningKey:         signKey,
	}

	ctx := context.Background()

	existingIndex, err := network.FetchCacheIndex(registry, repository, ociToken)
	if err != nil {
		fmt.Printf("WARNING: failed to fetch cache index: %v\n", err)
		existingIndex = &network.PushCacheIndex{Entries: make(map[string]network.PushCacheEntry)}
	}
	if existingIndex.Entries == nil {
		existingIndex.Entries = make(map[string]network.PushCacheEntry)
	}

	var filteredPaths []string
	if plan.Config.ForcePush {
		filteredPaths = plan.FilteredPaths
	} else {
		for _, p := range plan.FilteredPaths {
			basename := filepath.Base(p)
			parts := strings.SplitN(basename, "-", 2)
			if len(parts) >= 2 && existingIndex.Entries[parts[0]].NarDigest != "" {
				fmt.Printf("Skipping %s (already in cache index)\n", p)
				continue
			}
			filteredPaths = append(filteredPaths, p)
		}

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
				fmt.Printf("WARNING: upstream cache check failed: %v\n", err)
			} else {
				var trulyFiltered []string
				for _, p := range filteredPaths {
					basename := filepath.Base(p)
					parts := strings.SplitN(basename, "-", 2)
					if len(parts) >= 2 && existsMap[parts[0]] {
						fmt.Printf("Skipping %s (already in upstream cache)\n", p)
						continue
					}
					trulyFiltered = append(trulyFiltered, p)
				}
				filteredPaths = trulyFiltered
			}
		}
	}

	if len(filteredPaths) == 0 {
		fmt.Println("No new paths to push.")
		return nil
	}
	tokenMgr := proxy.NewTokenManager(registry, repository, "")
	_, configAnnotations, _ := proxy.BootstrapConfigWithAnnotations(ctx, nil, registry, repository, tokenMgr)

	isNative := false
	if configAnnotations != nil {
		if backend := configAnnotations["aeroflare.index-type"]; backend == "native" {
			isNative = true
		} else if backend := configAnnotations["aeroflare.backend"]; backend == "native" {
			isNative = true
		}
	}

	r2Cfg := network.GetR2Config(configAnnotations)
	var s3Client *s3.Client
	if r2Cfg != nil {
		var initErr error
		s3Client, initErr = r2Cfg.NewClient(ctx)
		if initErr != nil {
			return fmt.Errorf("failed to init R2 client: %v", initErr)
		}
		fmt.Printf("R2 Object Storage enabled (Bucket: %s)\n", r2Cfg.Bucket)
	}

	var receipts []network.PushReceipt

	chunkSize := 100
	for i := 0; i < len(filteredPaths); i += chunkSize {
		end := i + chunkSize
		if end > len(filteredPaths) {
			end = len(filteredPaths)
		}
		chunk := filteredPaths[i:end]

		numChunks := (len(filteredPaths) + chunkSize - 1) / chunkSize
		currentChunk := (i / chunkSize) + 1
		fmt.Printf("\n--- Processing chunk %d/%d ---\n\n", currentChunk, numChunks)

		var results []*prepare.Result
		printStep(1, 3, "Preparing (Generating NAR and narinfo files)")

		if len(chunk) == 1 {
			var res *prepare.Result
			var prepErr error
			res, prepErr = prepare.Prepare(ctx, chunk[0], cfg)
			if prepErr != nil {
				return fmt.Errorf("error during preparation: %v", prepErr)
			}
			results = append(results, res)
		} else {
			res, prepErr := prepare.PrepareBatch(ctx, chunk, cfg)
			if prepErr != nil {
				return fmt.Errorf("error during batch preparation: %v", prepErr)
			}
			results = res
		}
		
		if plan.Config.Verbosity < 1 {
			printSuccess(fmt.Sprintf("Prepared %d packages", len(results)))
		}

		pushedPaths := make(map[string]bool)

		type pushTask struct {
			r      *prepare.Result
			isRoot bool
		}
		var tasks []pushTask

		var collect func(r *prepare.Result, isRoot bool)
		collect = func(r *prepare.Result, isRoot bool) {
			if pushedPaths[r.StorePath] {
				return
			}
			pushedPaths[r.StorePath] = true

			tasks = append(tasks, pushTask{r: r, isRoot: isRoot})

			for _, missingRef := range r.MissingRefResults {
				collect(missingRef, false)
			}
		}

		for _, r := range results {
			collect(r, true)
		}

		printStep(2, 3, fmt.Sprintf("Uploading %d packages to OCI registry", len(tasks)))
		
		var mu sync.Mutex
		eg, egCtx := errgroup.WithContext(ctx)
		eg.SetLimit(plan.Config.Workers)

		for _, t := range tasks {
			t := t // capture loop variable
			eg.Go(func() error {
				r := t.r
				isRoot := t.isRoot

				narStat, err := os.Stat(r.NarPath)
				if err != nil {
					if isRoot {
						return fmt.Errorf("failed to stat NAR file (%s): %v", r.NarPath, err)
					}
					mu.Lock()
					fmt.Printf("ERROR: Failed to stat reference NAR file (%s): %v\n", r.NarPath, err)
					mu.Unlock()
					return nil
				}

				// Parse narinfo once to avoid disk reads for hashing
				narinfoData, err := os.ReadFile(r.NarinfoPath)
				if err != nil {
					if isRoot {
						return fmt.Errorf("failed to read narinfo (%s): %v", r.NarinfoPath, err)
					}
					mu.Lock()
					fmt.Printf("ERROR: Failed to read reference narinfo (%s): %v\n", r.NarinfoPath, err)
					mu.Unlock()
					return nil
				}
				ni, err := narinfo.Parse(string(narinfoData))
				if err != nil {
					if isRoot {
						return fmt.Errorf("failed to parse narinfo (%s): %v", r.NarinfoPath, err)
					}
					mu.Lock()
					fmt.Printf("ERROR: Failed to parse reference narinfo (%s): %v\n", r.NarinfoPath, err)
					mu.Unlock()
					return nil
				}

				// Create the layer ONCE for brutal speed without even hashing the file!
				layer, narDigest, err := network.NewLayerFast(r.NarPath, types.MediaType("application/vnd.aeroflare.nar.v1+"+ni.Compression), ni)
				if err != nil {
					if isRoot {
						return fmt.Errorf("failed to create NAR layer (%s): %v", r.NarPath, err)
					} else {
						mu.Lock()
						fmt.Printf("ERROR: Failed to create reference NAR layer: %v\n", err)
						mu.Unlock()
						return nil
					}
				}

				if isNative {
					// Dual-Strategy: Push natively to OCI
					basename := filepath.Base(ni.StorePath)
					tag := strings.SplitN(basename, "-", 2)[0]
					err = network.PushNarPackage(layer, ni, tag, registry, repository, ociToken)
					if err != nil {
						if isRoot {
							return fmt.Errorf("failed to push OCI Native artifact (%s): %v", r.StorePath, err)
						} else {
							mu.Lock()
							fmt.Printf("ERROR: Failed to push reference OCI Native artifact: %v\n", err)
							mu.Unlock()
							return nil
						}
					}
				}

				// Strategy 2 Legacy Fallback (the layer was already pushed by PushNarPackage, but we call PushLayer to be perfectly safe, which is a fast no-op if the layer exists)
				err = network.PushLayer(layer, registry, repository, ociToken)
				if err != nil {
					if isRoot {
						return fmt.Errorf("failed to push fallback NAR layer (%s): %v", r.NarPath, err)
					} else {
						mu.Lock()
						fmt.Printf("ERROR: Failed to push reference fallback NAR layer: %v\n", err)
						mu.Unlock()
						return nil
					}
				}

				if r2Cfg != nil && s3Client != nil {
					err = r2Cfg.UploadNarinfo(egCtx, s3Client, r.StorePath, r.NarinfoPath)
					if err != nil {
						if isRoot {
							return fmt.Errorf("failed to upload Narinfo file to R2 (%s): %v", r.NarinfoPath, err)
						} else {
							mu.Lock()
							fmt.Printf("ERROR: Failed to upload reference Narinfo to R2: %v\n", err)
							mu.Unlock()
							return nil
						}
					}
				} else {
					_, err = network.PushBlob(r.NarinfoPath, registry, repository, ociToken)
					if err != nil {
						if isRoot {
							return fmt.Errorf("failed to upload Narinfo file (%s): %v", r.NarinfoPath, err)
						} else {
							mu.Lock()
							fmt.Printf("ERROR: Failed to upload reference Narinfo: %v\n", err)
							mu.Unlock()
							return nil
						}
					}
				}

				pkgName := filepath.Base(r.StorePath)

				mu.Lock()
				totalUploaded++
				if plan.Config.Verbosity >= 1 {
					printSuccess(pkgName)
				}

				receipts = append(receipts, network.PushReceipt{
					StorePath:   r.StorePath,
					NarinfoPath: r.NarinfoPath,
					NarDigest:   narDigest,
					NarSize:     narStat.Size(),
					IsRoot:      isRoot,
				})
				mu.Unlock()

				return nil
			})
		}
		if err := eg.Wait(); err != nil {
			return err
		}

		if plan.Config.Verbosity < 1 {
			printSuccess(fmt.Sprintf("%d packages uploaded", len(tasks)))
		}
	}

	printStep(3, 3, "Updating cache index...")
	if err := network.UpdateCacheIndex(receipts, existingIndex, registry, repository, ociToken, plan.Config.SigningKey, r2Cfg, configAnnotations); err != nil {
		return fmt.Errorf("failed to update cache index: %v", err)
	}
	printSuccess("Cache index updated")

	duration := time.Since(startTime)

	rootsUploaded := 0
	for _, r := range receipts {
		if r.IsRoot {
			rootsUploaded++
		}
	}

	ui.PrintSummaryBox("Done", []ui.BoxField{
		{Label: "Packages uploaded", Value: strconv.Itoa(totalUploaded)},
		{Label: "GC roots", Value: strconv.Itoa(rootsUploaded)},
		{Label: "Duration", Value: duration.Round(time.Millisecond).String()},
	})

	return nil
}

func printStep(step, total int, msg string) {
	fmt.Printf("\n  [%d/%d] %s\n", step, total, msg)
}

func printSuccess(msg string) {
	checkMark := lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00")).Render("✓")
	fmt.Printf("  %s %s\n", checkMark, msg)
}
