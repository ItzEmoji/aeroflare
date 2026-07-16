// Command build provides build tasks for aeroflare, mirroring the pattern
// used by github/cli's scripts/build.go: a small Go program that computes
// version/date metadata and invokes `go build` with the right ldflags, so
// the same logic runs locally and in CI.
//
// Usage: go run scripts/build.go <task> [--prefix=VALUE]
//
// Known tasks:
//
//	build:
//	  Builds the root aeroflare binary for the host GOOS/GOARCH into
//	  out/aeroflare.
//
//	build-ci:
//	  Builds the aeroflare-ci binary for the host GOOS/GOARCH into
//	  out/aeroflare-ci.
//
//	build-all:
//	  Runs build then build-ci.
//
//	dist:
//	  Cross-builds aeroflare for linux/amd64 and linux/arm64 and packages
//	  each into out/aeroflare-<label>.tar.zst, with the binary at
//	  bin/aeroflare inside the archive.
//
//	dist-ci:
//	  Same as dist, for aeroflare-ci (out/aeroflare-ci-<label>.tar.zst,
//	  binary at bin/aeroflare-ci inside the archive).
//
//	dist-all:
//	  Runs dist then dist-ci.
//
//	install:
//	  Builds aeroflare (like build) and copies it to <prefix>/bin/aeroflare.
//
//	install-ci:
//	  Builds aeroflare-ci (like build-ci) and copies it to
//	  <prefix>/bin/aeroflare-ci.
//
//	install-all:
//	  Runs install then install-ci.
//
//	install-release:
//	  Fetches the aeroflare release tarball from GitHub (no local build)
//	  and copies the extracted binary to <prefix>/bin/aeroflare.
//
//	install-release-ci:
//	  Same as install-release, for aeroflare-ci.
//
//	install-release-all:
//	  Runs install-release then install-release-ci.
//
//	clean:
//	  Removes out/.
//
// Every build/dist task skips any output that's already newer than all
// tracked Go source files, printing "<path>: up to date" instead of
// rebuilding it.
//
// The install* tasks resolve <prefix> as: --prefix=VALUE (or --prefix
// VALUE) if given, else the PREFIX environment variable, else
// /usr/local.
//
// Supported environment variables:
//   - AEROFLARE_VERSION: overrides the version baked into the binary
//     (build/dist tasks), or pins the release tag to fetch
//     (install-release tasks)
//   - AEROFLARE_REPO: GitHub repo to fetch releases from for
//     install-release tasks (default ItzEmoji/aeroflare)
//   - PREFIX: install prefix for install* tasks (default /usr/local)
//   - SOURCE_DATE_EPOCH: enables reproducible build dates
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const modulePath = "github.com/itzemoji/aeroflare"

// archTarget cross-compiles for a GOARCH, labelled the way release.yaml's
// asset filenames expect (e.g. "x86_64" for amd64).
type archTarget struct {
	label  string
	goarch string
}

var distTargets = []archTarget{
	{label: "x86_64", goarch: "amd64"},
	{label: "aarch64", goarch: "arm64"},
}

// distBinary is one binary that build/dist/install tasks build, package,
// or install.
type distBinary struct {
	name string // output binary name, and out/<name>-<label>.tar.zst
	pkg  string // package path passed to `go build`
}

var aeroflareBin = distBinary{name: "aeroflare", pkg: "./cmd/aeroflare"}
var aeroflareCIBin = distBinary{name: "aeroflare-ci", pkg: "./cmd/aeroflare-ci"}

func main() {
	task, prefixFlag, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, "usage: go run scripts/build.go <task> [--prefix=VALUE]")
		os.Exit(1)
	}

	switch task {
	case "build":
		err = buildOne(aeroflareBin)
	case "build-ci":
		err = buildOne(aeroflareCIBin)
	case "build-all":
		err = buildAll()
	case "dist":
		err = distOne(aeroflareBin)
	case "dist-ci":
		err = distOne(aeroflareCIBin)
	case "dist-all":
		err = distAll()
	case "install":
		err = installOne(aeroflareBin, prefixFlag)
	case "install-ci":
		err = installOne(aeroflareCIBin, prefixFlag)
	case "install-all":
		err = installAll(prefixFlag)
	case "install-release":
		err = installReleaseOne(aeroflareBin, prefixFlag)
	case "install-release-ci":
		err = installReleaseOne(aeroflareCIBin, prefixFlag)
	case "install-release-all":
		err = installReleaseAll(prefixFlag)
	case "clean":
		err = clean()
	default:
		fmt.Fprintf(os.Stderr, "don't know how to build task `%s`\n", task)
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// parseArgs splits args into a task name and an optional --prefix value,
// accepting both `--prefix VALUE` and `--prefix=VALUE`.
func parseArgs(args []string) (task string, prefixFlag string, err error) {
	if len(args) < 1 {
		return "", "", fmt.Errorf("no task given")
	}
	task = args[0]
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case a == "--prefix":
			if i+1 >= len(rest) {
				return "", "", fmt.Errorf("--prefix requires a value")
			}
			prefixFlag = rest[i+1]
			i++
		case strings.HasPrefix(a, "--prefix="):
			prefixFlag = strings.TrimPrefix(a, "--prefix=")
		default:
			return "", "", fmt.Errorf("unknown argument %q", a)
		}
	}
	return task, prefixFlag, nil
}

// prefix resolves the install prefix: flagValue (if non-empty), else the
// PREFIX environment variable, else /usr/local.
func prefix(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if v := os.Getenv("PREFIX"); v != "" {
		return v
	}
	return "/usr/local"
}

func buildOne(bin distBinary) error {
	if err := os.MkdirAll("out", 0o755); err != nil {
		return err
	}
	out := "out/" + bin.name
	if upToDate(out) {
		fmt.Printf("%s: up to date\n", out)
		return nil
	}
	return runEnv(crossEnv(), "go", "build", "-trimpath", "-ldflags", ldflags(), "-o", out, bin.pkg)
}

// crossEnv returns the environment buildOne compiles with: the ambient
// environment, plus a CGO_ENABLED=0/GOOS/GOARCH override when TARGETOS and
// TARGETARCH are both set. Those two are deliberately not named GOOS/GOARCH:
// this same command also builds and runs this file as `go run
// scripts/build.go`, which would try to cross-execute itself if the ambient
// GOOS/GOARCH didn't match the host. Docker's buildx sets TARGETOS/TARGETARCH
// automatically for a multi-platform build, so a builder stage that promotes
// them to ENV (see Dockerfile) cross-compiles the binary for each requested
// platform without needing to run under QEMU emulation.
func crossEnv() []string {
	goos, goarch := os.Getenv("TARGETOS"), os.Getenv("TARGETARCH")
	if goos == "" || goarch == "" {
		return os.Environ()
	}
	return append(os.Environ(), "CGO_ENABLED=0", "GOOS="+goos, "GOARCH="+goarch)
}

func buildAll() error {
	if err := buildOne(aeroflareBin); err != nil {
		return err
	}
	return buildOne(aeroflareCIBin)
}

func distOne(bin distBinary) error {
	if err := os.MkdirAll("out", 0o755); err != nil {
		return err
	}
	for _, target := range distTargets {
		archive := fmt.Sprintf("out/%s-%s.tar.zst", bin.name, target.label)
		if upToDate(archive) {
			fmt.Printf("%s: up to date\n", archive)
			continue
		}
		if err := packageTarball(archive, bin, target); err != nil {
			return err
		}
	}
	return nil
}

func distAll() error {
	if err := distOne(aeroflareBin); err != nil {
		return err
	}
	return distOne(aeroflareCIBin)
}

// packageTarball cross-builds bin into a per-invocation temp directory
// (at <tmp>/bin/<name>) and archives it into archive with the member path
// bin/<name>, so the release asset follows a small FHS-style convention.
func packageTarball(archive string, bin distBinary, target archTarget) error {
	tmp, err := os.MkdirTemp("out", ".aeroflare-dist-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	out := filepath.Join(binDir, bin.name)

	env := append(os.Environ(),
		"CGO_ENABLED=0",
		"GOOS=linux",
		"GOARCH="+target.goarch,
	)
	if err := runEnv(env, "go", "build", "-trimpath", "-ldflags", ldflags(), "-o", out, bin.pkg); err != nil {
		return err
	}

	tmpArchive := filepath.Join(tmp, filepath.Base(archive))
	if err := run("tar", "--zstd", "-cf", tmpArchive, "-C", tmp, "bin/"+bin.name); err != nil {
		return err
	}
	return os.Rename(tmpArchive, archive)
}

// installOne builds bin (reusing buildOne, so the skip-if-fresh check
// still applies) and copies the result to <prefix>/bin/<name>.
func installOne(bin distBinary, prefixFlag string) error {
	if err := buildOne(bin); err != nil {
		return err
	}
	return installBinary("out/"+bin.name, bin.name, prefixFlag)
}

func installAll(prefixFlag string) error {
	if err := installOne(aeroflareBin, prefixFlag); err != nil {
		return err
	}
	return installOne(aeroflareCIBin, prefixFlag)
}

// installBinary copies src to <prefix>/bin/<name> with executable
// permissions, creating <prefix>/bin if needed.
func installBinary(src, name, prefixFlag string) error {
	binDir := filepath.Join(prefix(prefixFlag), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	dest := filepath.Join(binDir, name)
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dest, data, 0o755); err != nil {
		return err
	}
	fmt.Printf("installed %s\n", dest)
	return nil
}

// installReleaseOne fetches bin's release tarball from GitHub (no local
// build) and installs the extracted binary to <prefix>/bin/<name>.
func installReleaseOne(bin distBinary, prefixFlag string) error {
	repoSlug := repo()
	tag, err := releaseVersion(repoSlug)
	if err != nil {
		return err
	}
	label, err := hostArchLabel()
	if err != nil {
		return err
	}

	archiveName := fmt.Sprintf("%s-%s.tar.zst", bin.name, label)
	url := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repoSlug, tag, archiveName)

	tmp, err := os.MkdirTemp("", "aeroflare-install-release-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	archivePath := filepath.Join(tmp, archiveName)
	fmt.Printf("downloading %s\n", url)
	if err := downloadFile(url, archivePath); err != nil {
		return err
	}

	if err := run("tar", "--zstd", "-xf", archivePath, "-C", tmp, "bin/"+bin.name); err != nil {
		return err
	}

	return installBinary(filepath.Join(tmp, "bin", bin.name), bin.name, prefixFlag)
}

func installReleaseAll(prefixFlag string) error {
	if err := installReleaseOne(aeroflareBin, prefixFlag); err != nil {
		return err
	}
	return installReleaseOne(aeroflareCIBin, prefixFlag)
}

// repo resolves the GitHub repo install-release fetches from: the
// AEROFLARE_REPO environment variable, or ItzEmoji/aeroflare by default.
func repo() string {
	if v := os.Getenv("AEROFLARE_REPO"); v != "" {
		return v
	}
	return "ItzEmoji/aeroflare"
}

// releaseVersion resolves the release tag install-release fetches: the
// AEROFLARE_VERSION environment variable, or repoSlug's latest GitHub
// release tag_name if unset.
func releaseVersion(repoSlug string) (string, error) {
	if v := os.Getenv("AEROFLARE_VERSION"); v != "" {
		return v, nil
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repoSlug)
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetching latest release for %s: unexpected status %s", repoSlug, resp.Status)
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if payload.TagName == "" {
		return "", fmt.Errorf("no tag_name in latest release response for %s", repoSlug)
	}
	return payload.TagName, nil
}

// hostArchLabel maps the host's GOOS/GOARCH to the release asset label
// (x86_64/aarch64), matching distTargets. Only linux/amd64 and
// linux/arm64 releases are published.
func hostArchLabel() (string, error) {
	if runtime.GOOS != "linux" {
		return "", fmt.Errorf("install-release only supports linux (GOOS=%s)", runtime.GOOS)
	}
	for _, target := range distTargets {
		if target.goarch == runtime.GOARCH {
			return target.label, nil
		}
	}
	return "", fmt.Errorf("install-release doesn't support GOARCH=%s", runtime.GOARCH)
}

// downloadFile GETs url and writes the response body to dest.
func downloadFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading %s: unexpected status %s", url, resp.Status)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(f, resp.Body)
	return err
}

func clean() error {
	return os.RemoveAll("out")
}

// upToDate reports whether output exists and is newer than every tracked
// Go source file, meaning it doesn't need rebuilding.
func upToDate(output string) bool {
	info, err := os.Stat(output)
	if err != nil {
		return false
	}
	return !sourceFilesLaterThan(info.ModTime())
}

// sourceFilesLaterThan walks the repo (skipping dotfiles/dot-dirs, vendor,
// node_modules, and out/) and reports whether any go.mod, go.sum, or
// non-test .go file has a modification time after t.
func sourceFilesLaterThan(t time.Time) bool {
	foundLater := false
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if foundLater {
			return filepath.SkipDir
		}
		name := filepath.Base(path)
		if len(name) > 1 && (name[0] == '.' || name[0] == '_') {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			if name == "vendor" || name == "node_modules" || name == "out" {
				return filepath.SkipDir
			}
			return nil
		}
		if path == "go.mod" || path == "go.sum" || (strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go")) {
			if info.ModTime().After(t) {
				foundLater = true
			}
		}
		return nil
	})
	if err != nil {
		panic(err)
	}
	return foundLater
}

func ldflags() string {
	return fmt.Sprintf(
		"-s -w -X %s/internal/build.Version=%s -X %s/internal/build.Date=%s",
		modulePath, version(), modulePath, date(),
	)
}

func version() string {
	if v := os.Getenv("AEROFLARE_VERSION"); v != "" {
		return v
	}
	if desc, err := cmdOutput("git", "describe", "--tags", "--always", "--dirty"); err == nil {
		return desc
	}
	return "dev"
}

func date() string {
	t := time.Now()
	if sourceDate := os.Getenv("SOURCE_DATE_EPOCH"); sourceDate != "" {
		if sec, err := strconv.ParseInt(sourceDate, 10, 64); err == nil {
			t = time.Unix(sec, 0)
		}
	}
	return t.Format("2006-01-02")
}

func run(args ...string) error {
	return runEnv(os.Environ(), args...)
}

func runEnv(env []string, args ...string) error {
	fmt.Println(strings.Join(args, " "))
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func cmdOutput(args ...string) (string, error) {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stderr = io.Discard
	out, err := cmd.Output()
	return strings.TrimSuffix(string(out), "\n"), err
}
