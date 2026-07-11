// Command build provides build tasks for aeroflare, mirroring the pattern
// used by github/cli's script/build.go: a small Go program that computes
// version/date metadata and invokes `go build` with the right ldflags, so
// the same logic runs locally and in CI.
//
// Usage: go run script/build.go <task>
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
//	clean:
//	  Removes out/.
//
// Every build/dist task skips any output that's already newer than all
// tracked Go source files, printing "<path>: up to date" instead of
// rebuilding it.
//
// Supported environment variables:
//   - AEROFLARE_VERSION: overrides the version baked into the binary
//   - SOURCE_DATE_EPOCH: enables reproducible build dates
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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

// distBinary is one binary that build/dist tasks build and package.
type distBinary struct {
	name string // output binary name, and out/<name>-<label>.tar.zst
	pkg  string // package path passed to `go build`
}

var aeroflareBin = distBinary{name: "aeroflare", pkg: "."}
var aeroflareCIBin = distBinary{name: "aeroflare-ci", pkg: "./cmd/aeroflare-ci"}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run script/build.go <task>")
		os.Exit(1)
	}

	var err error
	switch task := os.Args[1]; task {
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

func buildOne(bin distBinary) error {
	if err := os.MkdirAll("out", 0o755); err != nil {
		return err
	}
	out := "out/" + bin.name
	if upToDate(out) {
		fmt.Printf("%s: up to date\n", out)
		return nil
	}
	return run("go", "build", "-trimpath", "-ldflags", ldflags(), "-o", out, bin.pkg)
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
