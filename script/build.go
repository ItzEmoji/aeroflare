// Command build provides build tasks for aeroflare, mirroring the pattern
// used by github/cli's script/build.go: a small Go program that computes
// version/date metadata and invokes `go build` with the right ldflags, so
// the same logic runs locally and in CI.
//
// Usage: go run script/build.go <task>
//
// Known tasks:
//
//	bin/aeroflare:
//	  Builds the root aeroflare binary for the host GOOS/GOARCH.
//
//	bin/aeroflare-ci:
//	  Builds the aeroflare-ci binary for the host GOOS/GOARCH.
//
//	dist:
//	  Cross-builds both binaries for linux/amd64 and linux/arm64 and
//	  packages each into dist/<bin>-<label>.tar.zst.
//
//	clean:
//	  Removes bin/ and dist/.
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

// distBinary is one binary that `dist` builds and packages for every
// archTarget.
type distBinary struct {
	name string // output binary name, and dist/<name>-<label>.tar.zst
	pkg  string // package path passed to `go build`
}

var distBinaries = []distBinary{
	{name: "aeroflare", pkg: "."},
	{name: "aeroflare-ci", pkg: "./cmd/aeroflare-ci"},
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run script/build.go <task>")
		os.Exit(1)
	}

	var err error
	switch task := os.Args[1]; task {
	case "bin/aeroflare":
		err = buildHost("aeroflare", ".")
	case "bin/aeroflare-ci":
		err = buildHost("aeroflare-ci", "./cmd/aeroflare-ci")
	case "dist":
		err = dist()
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

func buildHost(name, pkg string) error {
	if err := os.MkdirAll("bin", 0o755); err != nil {
		return err
	}
	exe := "bin/" + name
	return run("go", "build", "-trimpath", "-ldflags", ldflags(), "-o", exe, pkg)
}

func dist() error {
	if err := os.MkdirAll("dist", 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll("stage", 0o755); err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll("stage") }()

	for _, target := range distTargets {
		for _, bin := range distBinaries {
			out := "stage/" + bin.name
			env := append(os.Environ(),
				"CGO_ENABLED=0",
				"GOOS=linux",
				"GOARCH="+target.goarch,
			)
			if err := runEnv(env, "go", "build", "-trimpath", "-ldflags", ldflags(), "-o", out, bin.pkg); err != nil {
				return err
			}
			archive := fmt.Sprintf("dist/%s-%s.tar.zst", bin.name, target.label)
			if err := run("tar", "--zstd", "-cf", archive, "-C", "stage", bin.name); err != nil {
				return err
			}
			if err := os.Remove(out); err != nil {
				return err
			}
		}
	}
	return nil
}

func clean() error {
	if err := os.RemoveAll("bin"); err != nil {
		return err
	}
	return os.RemoveAll("dist")
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
