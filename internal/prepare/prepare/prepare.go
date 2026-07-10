package prepare

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/itzemoji/aeroflare/internal/prepare/cache"
	"github.com/itzemoji/aeroflare/internal/prepare/compress"
	narhash "github.com/itzemoji/aeroflare/internal/prepare/hash"
	"github.com/itzemoji/aeroflare/internal/prepare/narinfo"
	"github.com/itzemoji/aeroflare/internal/prepare/signing"
	"github.com/itzemoji/aeroflare/internal/prepare/store"
)

// Config holds configuration for the prepare operation.
type Config struct {
	OutputDir          string              // directory to write .nar and .narinfo files
	Compression        compress.Type       // compression algorithm
	CacheURLs          []string            // upstream binary caches; empty disables reference filtering
	Workers            int                 // number of concurrent workers for cache checking
	PrepareMissingRefs bool                // if true, also generate NAR+narinfo for references not on the upstream cache, recursively
	SigningKey         *signing.PrivateKey // if non-nil, narinfo files are signed with this key
}

// Result holds the outcome of preparing a single store path.
type Result struct {
	StorePath         string    // the input store path
	Hash              string    // base32 hash extracted from the store path
	NarPath           string    // path to the generated .nar.<compression> file
	NarinfoPath       string    // path to the generated .narinfo file
	References        []string  // all references (full store paths)
	MissingRefs       []string  // references not found on upstream cache (full store paths)
	MissingRefResults []*Result // prepared results for missing refs (only populated when Config.PrepareMissingRefs is true)
	Signed            bool      // true if the narinfo was signed
}

// Prepare processes a single store path: creates a compressed NAR archive
// and a narinfo file, and checks which references exist on the upstream cache.
// If Config.PrepareMissingRefs is true, missing references are also prepared
// (NAR + narinfo generated) recursively — their sub-references are also
// checked against the upstream cache and prepared if missing, with cycle
// detection to handle self-referencing paths.
func Prepare(ctx context.Context, storePath string, cfg *Config) (*Result, error) {
	if cfg.Workers <= 0 {
		cfg.Workers = 50
	}

	pathHash, _, err := store.ParsePath(storePath)
	if err != nil {
		return nil, err
	}

	info, err := store.GetPathInfo(storePath)
	if err != nil {
		return nil, fmt.Errorf("get path info: %w", err)
	}

	missingRefs, err := checkReferences(ctx, info.References, cfg)
	if err != nil {
		return nil, fmt.Errorf("check references: %w", err)
	}

	narPath, narinfoPath, err := writeNarAndNarinfo(storePath, pathHash, info, cfg)
	if err != nil {
		return nil, err
	}

	result := &Result{
		StorePath:   storePath,
		Hash:        pathHash,
		NarPath:     narPath,
		NarinfoPath: narinfoPath,
		References:  info.References,
		MissingRefs: missingRefs,
		Signed:      cfg.SigningKey != nil,
	}

	if cfg.PrepareMissingRefs && len(missingRefs) > 0 {
		visited := map[string]bool{storePath: true}
		refResults, err := prepareRefsRecursive(ctx, missingRefs, visited, cfg)
		if err != nil {
			return nil, fmt.Errorf("prepare missing refs: %w", err)
		}
		result.MissingRefResults = refResults
	}

	return result, nil
}

// PrepareBatch processes multiple store paths concurrently.
// It first collects all unique references across all paths, checks them
// against the upstream cache in a single batch, then creates NAR archives
// and narinfo files in parallel.
// If Config.PrepareMissingRefs is true, missing references (not on the
// upstream cache) are also prepared recursively — deduplicated across all
// input paths, prepared concurrently, and their results attached to each
// referencing Result. Sub-references of missing refs are also checked
// against the cache and prepared if missing, with cycle detection.
func PrepareBatch(ctx context.Context, storePaths []string, cfg *Config) ([]*Result, error) {
	if cfg.Workers <= 0 {
		cfg.Workers = 50
	}

	// Phase 1: Get path info for all paths concurrently
	infos, err := gatherPathInfos(ctx, storePaths, cfg.Workers)
	if err != nil {
		return nil, err
	}

	// Phase 2: Collect all unique reference hashes and check against cache
	allRefHashes, refHashToPath := collectReferenceHashes(infos)
	existsMap, err := checkRefBatch(ctx, allRefHashes, cfg)
	if err != nil {
		return nil, fmt.Errorf("check references batch: %w", err)
	}

	// Phase 3: Create NAR archives and narinfo files concurrently
	results := make([]*Result, len(storePaths))
	resultCh := make(chan batchResult, len(storePaths))

	type batchJob struct {
		idx  int
		path string
	}
	jobs := make(chan batchJob, len(storePaths))

	narWorkers := cfg.Workers
	if narWorkers > len(storePaths) {
		narWorkers = len(storePaths)
	}

	var wg sync.WaitGroup
	for i := 0; i < narWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				info := infos[job.path]
				missingRefs := computeMissingRefs(info.References, existsMap, refHashToPath)
				narPath, narinfoPath, err := writeNarAndNarinfo(job.path, info.Hash, info, cfg)
				resultCh <- batchResult{
					idx: job.idx,
					result: &Result{
						StorePath:   job.path,
						Hash:        info.Hash,
						NarPath:     narPath,
						NarinfoPath: narinfoPath,
						References:  info.References,
						MissingRefs: missingRefs,
						Signed:      cfg.SigningKey != nil,
					},
					err: err,
				}
			}
		}()
	}

	for i, p := range storePaths {
		jobs <- batchJob{idx: i, path: p}
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	for r := range resultCh {
		if r.err != nil {
			return nil, fmt.Errorf("prepare %s: %w", storePaths[r.idx], r.err)
		}
		results[r.idx] = r.result
	}

	// Phase 4: Prepare missing refs recursively
	if cfg.PrepareMissingRefs {
		seen := make(map[string]bool)
		var allMissing []string
		for _, r := range results {
			for _, ref := range r.MissingRefs {
				if !seen[ref] {
					seen[ref] = true
					allMissing = append(allMissing, ref)
				}
			}
		}

		if len(allMissing) > 0 {
			visited := make(map[string]bool)
			for _, r := range results {
				visited[r.StorePath] = true
			}
			refResults, err := prepareRefsRecursive(ctx, allMissing, visited, cfg)
			if err != nil {
				return nil, fmt.Errorf("prepare missing refs: %w", err)
			}

			refResultMap := make(map[string]*Result, len(refResults))
			for _, rr := range refResults {
				refResultMap[rr.StorePath] = rr
			}

			for _, r := range results {
				r.MissingRefResults = make([]*Result, 0, len(r.MissingRefs))
				for _, ref := range r.MissingRefs {
					if rr, ok := refResultMap[ref]; ok {
						r.MissingRefResults = append(r.MissingRefResults, rr)
					}
				}
			}
		}
	}

	return results, nil
}

// batchResult carries the outcome of preparing one store path back from a
// worker goroutine, tagged with its original index so results can be placed
// in input order.
type batchResult struct {
	idx    int
	result *Result
	err    error
}

// gatherPathInfos fetches store.PathInfo for each path concurrently using the
// given number of workers, returning a map keyed by store path.
func gatherPathInfos(ctx context.Context, paths []string, workers int) (map[string]*store.PathInfo, error) {
	type infoResult struct {
		path string
		info *store.PathInfo
		err  error
	}

	if workers > len(paths) {
		workers = len(paths)
	}

	jobs := make(chan string, len(paths))
	results := make(chan infoResult, len(paths))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				info, err := store.GetPathInfo(p)
				results <- infoResult{path: p, info: info, err: err}
			}
		}()
	}

	for _, p := range paths {
		jobs <- p
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	infos := make(map[string]*store.PathInfo, len(paths))
	for r := range results {
		if r.err != nil {
			return nil, fmt.Errorf("get info for %s: %w", r.path, r.err)
		}
		infos[r.path] = r.info
	}
	return infos, nil
}

// collectReferenceHashes gathers the deduplicated set of hash parts across
// all references found in infos, along with a map back from hash to the
// full reference store path.
func collectReferenceHashes(infos map[string]*store.PathInfo) ([]string, map[string]string) {
	seen := make(map[string]bool)
	var hashes []string
	hashToPath := make(map[string]string)

	for _, info := range infos {
		for _, ref := range info.References {
			refHash, _, err := store.ParsePath(ref)
			if err != nil {
				continue
			}
			if !seen[refHash] {
				seen[refHash] = true
				hashes = append(hashes, refHash)
				hashToPath[refHash] = ref
			}
		}
	}
	return hashes, hashToPath
}

// checkRefBatch checks existence of the given hashes against the upstream caches
// configured in cfg. With no caches configured or no hashes to check it returns
// an empty map, meaning: treat everything as unknown, upload it.
func checkRefBatch(ctx context.Context, hashes []string, cfg *Config) (map[string]bool, error) {
	if len(cfg.CacheURLs) == 0 || len(hashes) == 0 {
		return make(map[string]bool), nil
	}

	g := cache.NewGroup(cfg.CacheURLs, cache.WithMaxConns(cfg.Workers))
	return g.ExistsBatch(ctx, hashes, cfg.Workers)
}

// checkReferences checks the given references against the upstream cache and
// returns the subset that are missing (not present on the cache).
func checkReferences(ctx context.Context, references []string, cfg *Config) ([]string, error) {
	if len(references) == 0 {
		return nil, nil
	}

	refHashes := make([]string, 0, len(references))
	hashToPath := make(map[string]string)
	for _, ref := range references {
		refHash, _, err := store.ParsePath(ref)
		if err != nil {
			continue
		}
		refHashes = append(refHashes, refHash)
		hashToPath[refHash] = ref
	}

	existsMap, err := checkRefBatch(ctx, refHashes, cfg)
	if err != nil {
		return nil, err
	}

	return computeMissingRefs(references, existsMap, hashToPath), nil
}

// computeMissingRefs filters references down to those not present in
// existsMap. If existsMap is empty (no cache was configured to check
// against), every reference is treated as missing.
func computeMissingRefs(references []string, existsMap map[string]bool, hashToPath map[string]string) []string {
	if len(existsMap) == 0 {
		// No cache configured: all references are "missing"
		var missing []string
		missing = append(missing, references...)
		return missing
	}

	var missing []string
	for _, ref := range references {
		refHash, _, err := store.ParsePath(ref)
		if err != nil {
			continue
		}
		if !existsMap[refHash] {
			missing = append(missing, ref)
		}
	}
	return missing
}

// prepareRefsRecursive generates NAR archives and narinfo files for the given
// store paths, checking their references against the upstream cache. Any
// references not found on the cache are recursively prepared. The visited map
// tracks paths already processed to prevent infinite cycles (e.g. a package
// that references itself).
func prepareRefsRecursive(ctx context.Context, refPaths []string, visited map[string]bool, cfg *Config) ([]*Result, error) {
	var newPaths []string
	for _, p := range refPaths {
		if !visited[p] {
			visited[p] = true
			newPaths = append(newPaths, p)
		}
	}
	if len(newPaths) == 0 {
		return nil, nil
	}

	infos, err := gatherPathInfos(ctx, newPaths, cfg.Workers)
	if err != nil {
		return nil, fmt.Errorf("gather path infos for refs: %w", err)
	}

	allRefHashes, refHashToPath := collectReferenceHashes(infos)
	existsMap, err := checkRefBatch(ctx, allRefHashes, cfg)
	if err != nil {
		return nil, fmt.Errorf("check references: %w", err)
	}

	results, err := writeNarsFromInfos(newPaths, infos, cfg)
	if err != nil {
		return nil, err
	}

	for i, r := range results {
		info := infos[newPaths[i]]
		r.MissingRefs = computeMissingRefs(info.References, existsMap, refHashToPath)
	}

	var allMissing []string
	seenMissing := make(map[string]bool)
	for _, r := range results {
		for _, ref := range r.MissingRefs {
			if !seenMissing[ref] {
				seenMissing[ref] = true
				allMissing = append(allMissing, ref)
			}
		}
	}

	if len(allMissing) > 0 {
		subResults, err := prepareRefsRecursive(ctx, allMissing, visited, cfg)
		if err != nil {
			return nil, fmt.Errorf("prepare missing sub-references: %w", err)
		}

		subResultMap := make(map[string]*Result, len(subResults))
		for _, sr := range subResults {
			subResultMap[sr.StorePath] = sr
		}

		for _, r := range results {
			for _, ref := range r.MissingRefs {
				if sr, ok := subResultMap[ref]; ok {
					r.MissingRefResults = append(r.MissingRefResults, sr)
				}
			}
		}
	}

	return results, nil
}

// writeNarsFromInfos writes NAR + narinfo for each path using the provided
// path infos. No reference checking is performed. Results are returned in the
// same order as paths. This is used both for missing-ref preparation and can
// be tested directly without invoking nix commands.
func writeNarsFromInfos(paths []string, infos map[string]*store.PathInfo, cfg *Config) ([]*Result, error) {
	results := make([]*Result, len(paths))
	resultCh := make(chan batchResult, len(paths))

	type writeJob struct {
		idx  int
		path string
	}
	jobs := make(chan writeJob, len(paths))

	workers := cfg.Workers
	if workers > len(paths) {
		workers = len(paths)
	}
	if workers < 1 {
		workers = 1
	}

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				info := infos[job.path]
				narPath, narinfoPath, err := writeNarAndNarinfo(job.path, info.Hash, info, cfg)
				resultCh <- batchResult{
					idx: job.idx,
					result: &Result{
						StorePath:   job.path,
						Hash:        info.Hash,
						NarPath:     narPath,
						NarinfoPath: narinfoPath,
						References:  info.References,
						Signed:      cfg.SigningKey != nil,
					},
					err: err,
				}
			}
		}()
	}

	for i, p := range paths {
		jobs <- writeJob{idx: i, path: p}
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	for r := range resultCh {
		if r.err != nil {
			return nil, fmt.Errorf("prepare ref %s: %w", paths[r.idx], r.err)
		}
		results[r.idx] = r.result
	}

	return results, nil
}

// writeNarAndNarinfo dumps storePath as a NAR, compresses and hashes it, then
// writes both the compressed NAR file and its accompanying .narinfo file to
// cfg.OutputDir. It returns the paths to the two written files.
func writeNarAndNarinfo(storePath, pathHash string, info *store.PathInfo, cfg *Config) (string, string, error) {
	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		return "", "", fmt.Errorf("create output dir: %w", err)
	}

	narFileName := fmt.Sprintf("%s.%s", pathHash, cfg.Compression.Extension())
	narFilePath := filepath.Join(cfg.OutputDir, narFileName)

	narFile, err := os.Create(narFilePath)
	if err != nil {
		return "", "", fmt.Errorf("create nar file: %w", err)
	}
	defer func() { _ = narFile.Close() }()

	compWriter, err := compress.NewWriter(narFile, cfg.Compression)
	if err != nil {
		return "", "", fmt.Errorf("create compression writer: %w", err)
	}

	b := &store.LegacyStoreBackend{}
	narStream, err := b.Dump(storePath)
	if err != nil {
		_ = compWriter.Close()
		return "", "", fmt.Errorf("dump nar: %w", err)
	}

	narHasher := sha256.New()
	var narSize int64

	buf := make([]byte, 32*1024)
	for {
		n, readErr := narStream.Read(buf)
		if n > 0 {
			narSize += int64(n)
			if _, werr := narHasher.Write(buf[:n]); werr != nil {
				_ = narStream.Close()
				_ = compWriter.Close()
				return "", "", fmt.Errorf("hash write error: %w", werr)
			}
			if _, werr := compWriter.Write(buf[:n]); werr != nil {
				_ = narStream.Close()
				_ = compWriter.Close()
				return "", "", fmt.Errorf("compress write error: %w", werr)
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			_ = narStream.Close()
			_ = compWriter.Close()
			return "", "", fmt.Errorf("read nar stream: %w", readErr)
		}
	}
	if err := narStream.Close(); err != nil {
		_ = compWriter.Close()
		return "", "", fmt.Errorf("close nar stream: %w", err)
	}

	if err := compWriter.Close(); err != nil {
		return "", "", fmt.Errorf("close compression writer: %w", err)
	}

	narHashStr := fmt.Sprintf("sha256:%s", narhash.EncodeBase32(narHasher.Sum(nil)))
	fileHashStr := fmt.Sprintf("sha256:%s", narhash.EncodeBase32(compWriter.Hash()))
	fileSize := compWriter.Size()

	references := make([]string, 0, len(info.References))
	for _, ref := range info.References {
		references = append(references, filepath.Base(ref))
	}

	deriver := ""
	if info.Deriver != "" && info.Deriver != "unknown-deriver" {
		deriver = filepath.Base(info.Deriver)
	}

	ni := &narinfo.Narinfo{
		StorePath:   storePath,
		URL:         fmt.Sprintf("nar/%s", narFileName),
		Compression: string(cfg.Compression),
		FileHash:    fileHashStr,
		FileSize:    fileSize,
		NarHash:     narHashStr,
		NarSize:     narSize,
		References:  references,
		Deriver:     deriver,
		System:      info.System,
	}

	if cfg.SigningKey != nil {
		ni.Sig = cfg.SigningKey.SignNarinfo(storePath, narHashStr, narSize, references)
	}

	narinfoPath := filepath.Join(cfg.OutputDir, fmt.Sprintf("%s.narinfo", pathHash))
	if err := os.WriteFile(narinfoPath, []byte(ni.String()), 0o644); err != nil {
		return "", "", fmt.Errorf("write narinfo: %w", err)
	}

	return narFilePath, narinfoPath, nil
}

// ParseInputFile reads a file containing one store path per line.
// Empty lines and lines starting with # are ignored.
func ParseInputFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read input file: %w", err)
	}

	var paths []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		paths = append(paths, line)
	}
	return paths, nil
}
