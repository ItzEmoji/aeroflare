package ci

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// discoverSentinel is the `builds` entry that expands to every buildable output
// the flake in the working directory exposes for the running system.
const discoverSentinel = "all"

// discoverRef is the flake the sentinel discovers against. Discovery is
// deliberately limited to the checkout being built: a CI run caches its own
// repository, and pointing it at a remote flake is a different feature.
const discoverRef = "."

// toplevelAttr is the attribute path a NixOS configuration must be built
// through. `nixosConfigurations.<host>` is a module-system result rather than a
// derivation, so only `.config.system.build.toplevel` is buildable.
const toplevelAttr = ".config.system.build.toplevel"

// flakeOutputs is the discovery evaluation's result: the attribute names of
// each output class the flake exposes, for one system.
type flakeOutputs struct {
	System              string   `json:"system"`
	Packages            []string `json:"packages"`
	DevShells           []string `json:"devShells"`
	NixosConfigurations []string `json:"nixosConfigurations"`
}

// hasDiscoverSentinel reports whether any build entry asks for discovery.
func hasDiscoverSentinel(builds []string) bool {
	for _, b := range builds {
		if strings.TrimSpace(b) == discoverSentinel {
			return true
		}
	}
	return false
}

// escapeNixString renders s as the body of a Nix double-quoted string. Nix
// starts an interpolation at "${" even inside quotes, so that needs escaping
// alongside backslashes and quotes.
func escapeNixString(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, `${`, `\${`).Replace(s)
}

// discoverExpr renders the Nix expression that enumerates flakePath's outputs.
//
// One expression rather than one `nix eval` per output class, because `or {}`
// makes an absent class a total, first-class case. Asking for a missing class
// directly would instead mean telling "no such output" apart from a genuine
// evaluation error by matching on stderr. It also avoids `nix flake show`,
// which would evaluate `legacyPackages` — for a NUR repo, the whole package set
// including the non-derivation `lib`, `overlays` and `nixosModules` attributes.
//
// NixOS configurations are filtered to the running system: a host built for
// another platform cannot be built here at all.
func discoverExpr(flakePath string) string {
	return `let
  f = builtins.getFlake "` + escapeNixString(flakePath) + `";
  s = builtins.currentSystem;
in {
  system = s;
  packages = builtins.attrNames (f.packages.${s} or {});
  devShells = builtins.attrNames (f.devShells.${s} or {});
  nixosConfigurations = builtins.filter
    (n: (f.nixosConfigurations.${n}.pkgs.stdenv.hostPlatform.system or null) == s)
    (builtins.attrNames (f.nixosConfigurations or {}));
}`
}

// quoteAttrName quotes an attribute name that would otherwise be read as an
// attribute path separator, e.g. `.#packages.x86_64-linux."foo.bar"`.
func quoteAttrName(name string) string {
	if !strings.ContainsAny(name, `."\`) {
		return name
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(name) + `"`
}

// sorted returns a sorted copy, so a run's build order does not depend on the
// attribute order nix happened to emit.
func sorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// Installables renders the discovered outputs as flake installables for ref.
func (o flakeOutputs) Installables(ref string) []string {
	var out []string
	for _, n := range sorted(o.Packages) {
		out = append(out, ref+"#packages."+o.System+"."+quoteAttrName(n))
	}
	for _, n := range sorted(o.DevShells) {
		out = append(out, ref+"#devShells."+o.System+"."+quoteAttrName(n))
	}
	for _, n := range sorted(o.NixosConfigurations) {
		out = append(out, ref+"#nixosConfigurations."+quoteAttrName(n)+toplevelAttr)
	}
	return out
}

// Total is the number of discovered outputs across every class.
func (o flakeOutputs) Total() int {
	return len(o.Packages) + len(o.DevShells) + len(o.NixosConfigurations)
}

// discoverLine renders the one-line roll-up of what discovery found.
func discoverLine(o flakeOutputs) string {
	return fmt.Sprintf("discover %d packages, %d devShells, %d nixosConfigurations  (%s)",
		len(o.Packages), len(o.DevShells), len(o.NixosConfigurations), o.System)
}

// expandBuilds replaces every sentinel entry with found, leaving explicit
// entries in place. Duplicates are dropped, so listing a package explicitly
// alongside `all` builds it once.
func expandBuilds(builds, found []string) []string {
	seen := make(map[string]bool, len(builds)+len(found))
	var out []string
	for _, b := range builds {
		entries := []string{b}
		if strings.TrimSpace(b) == discoverSentinel {
			entries = found
		}
		for _, e := range entries {
			if !seen[e] {
				seen[e] = true
				out = append(out, e)
			}
		}
	}
	return out
}

// trimEvalNoise returns the evaluation error starting at nix's first `error:`
// line. Everything before it is `warning:` chatter from the user's nix.conf,
// which would otherwise bury the one line that says what actually broke. Output
// with no `error:` line at all is returned whole rather than dropped.
func trimEvalNoise(stderr string) string {
	lines := strings.Split(stderr, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "error:") {
			return strings.TrimSpace(strings.Join(lines[i:], "\n"))
		}
	}
	return strings.TrimSpace(stderr)
}

// discoverFlake evaluates dir's flake and returns the outputs it exposes for
// the running system.
//
// The evaluation is impure because `builtins.getFlake` on a local checkout is
// not a locked reference. That is scoped to discovery alone; the builds it
// feeds are unaffected. Evaluation output is captured rather than streamed and
// surfaces only on failure, since a successful discovery has nothing to say
// beyond its roll-up line.
func discoverFlake(dir string) (flakeOutputs, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return flakeOutputs{}, fmt.Errorf("resolving %s: %w", dir, err)
	}
	// Checked up front because the alternative is a twenty-line Nix stack trace
	// ending in "flake.nix does not exist" for what is almost always a job
	// running from the wrong directory.
	if _, err := os.Stat(filepath.Join(abs, "flake.nix")); err != nil {
		return flakeOutputs{}, fmt.Errorf("'all' needs a flake: no flake.nix in %s", abs)
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("nix", "eval", "--impure", "--json", "--expr", discoverExpr(abs))
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := trimEvalNoise(stderr.String())
		if msg == "" {
			return flakeOutputs{}, fmt.Errorf("nix eval: %w", err)
		}
		return flakeOutputs{}, fmt.Errorf("nix eval: %w\n%s", err, msg)
	}
	var out flakeOutputs
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return flakeOutputs{}, fmt.Errorf("parsing discovery output: %w", err)
	}
	return out, nil
}

// resolveDiscovery expands the `all` sentinel in spec.Builds in place. It never
// shells out when no entry asks for discovery.
func resolveDiscovery(spec *RunSpec, w io.Writer) error {
	if !hasDiscoverSentinel(spec.Builds) {
		return nil
	}
	out, err := discoverFlake(discoverRef)
	if err != nil {
		return err
	}
	if out.Total() == 0 {
		return fmt.Errorf("'all' found nothing to build: the flake exposes no packages, "+
			"devShells or nixosConfigurations for %s", out.System)
	}
	_, _ = fmt.Fprintf(w, "%s\n", discoverLine(out))
	spec.Builds = expandBuilds(spec.Builds, out.Installables(discoverRef))
	return nil
}
