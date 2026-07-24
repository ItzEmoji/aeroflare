package ci

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestHasSentinel_Discover(t *testing.T) {
	cases := []struct {
		name   string
		builds []string
		want   bool
	}{
		{"absent", []string{".#default", ".#other"}, false},
		{"bare", []string{"all"}, true},
		{"mixed with explicit", []string{".#default", "all"}, true},
		{"surrounding whitespace", []string{"  all\t"}, true},
		{"substring is not the sentinel", []string{".#all", "allow"}, false},
		{"empty", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasSentinel(tc.builds, discoverSentinel); got != tc.want {
				t.Errorf("hasSentinel(%v, all) = %v, want %v", tc.builds, got, tc.want)
			}
		})
	}
}

func TestEscapeNixString(t *testing.T) {
	cases := map[string]string{
		"/home/me/repo":     "/home/me/repo",
		`/tmp/a"b`:          `/tmp/a\"b`,
		`/tmp/a\b`:          `/tmp/a\\b`,
		"/tmp/${HOME}/repo": `/tmp/\${HOME}/repo`,
	}
	for in, want := range cases {
		if got := escapeNixString(in); got != want {
			t.Errorf("escapeNixString(%q) = %q, want %q", in, got, want)
		}
	}
}

// The path is interpolated into a Nix string literal, so a path containing a
// quote must not be able to break out of it and inject an expression.
func TestDiscoverExpr_EscapesPath(t *testing.T) {
	expr := discoverExpr(`/tmp/a"; abort "pwned`)
	want := `  f = builtins.getFlake "/tmp/a\"; abort \"pwned";`
	if !strings.Contains(expr, want) {
		t.Errorf("path did not survive as one escaped literal, want line\n%s\ngot\n%s", want, expr)
	}
	// Every quote in the expression is either a literal delimiter or escaped;
	// an unescaped one from the path would end the string early.
	if strings.Contains(expr, `"/tmp/a"; abort`) {
		t.Errorf("path escaped its string literal:\n%s", expr)
	}
}

// The expression must ask for every class the design covers, and must do so
// with `or` fallbacks so an absent class yields nothing instead of failing.
func TestDiscoverExpr_CoversClasses(t *testing.T) {
	expr := discoverExpr("/repo")
	for _, want := range []string{
		"builtins.getFlake",
		"builtins.currentSystem",
		"f.packages.${s} or {}",
		"f.devShells.${s} or {}",
		"f.nixosConfigurations or {}",
	} {
		if !strings.Contains(expr, want) {
			t.Errorf("expression is missing %q:\n%s", want, expr)
		}
	}
}

func TestQuoteAttrName(t *testing.T) {
	cases := map[string]string{
		"hello":     "hello",
		"my-pkg_2":  "my-pkg_2",
		"foo.bar":   `"foo.bar"`,
		`weird"one`: `"weird\"one"`,
	}
	for in, want := range cases {
		if got := quoteAttrName(in); got != want {
			t.Errorf("quoteAttrName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInstallables(t *testing.T) {
	o := flakeOutputs{
		System:              "x86_64-linux",
		Packages:            []string{"zed", "aeroflare"},
		DevShells:           []string{"default"},
		NixosConfigurations: []string{"laptop"},
	}
	got := o.Installables(".")
	want := []string{
		".#packages.x86_64-linux.aeroflare",
		".#packages.x86_64-linux.zed",
		".#devShells.x86_64-linux.default",
		".#nixosConfigurations.laptop.config.system.build.toplevel",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Installables() =\n%v\nwant\n%v", got, want)
	}
}

// A flake exposing nothing for this system yields no installables rather than
// a malformed one.
func TestInstallables_Empty(t *testing.T) {
	o := flakeOutputs{System: "aarch64-darwin"}
	if got := o.Installables("."); len(got) != 0 {
		t.Errorf("expected no installables, got %v", got)
	}
	if o.Total() != 0 {
		t.Errorf("Total() = %d, want 0", o.Total())
	}
}

func TestTotal(t *testing.T) {
	o := flakeOutputs{
		Packages:            []string{"a", "b"},
		DevShells:           []string{"c"},
		NixosConfigurations: []string{"d", "e", "f"},
	}
	if got := o.Total(); got != 6 {
		t.Errorf("Total() = %d, want 6", got)
	}
}

func TestExpandBuilds_SentinelOnly(t *testing.T) {
	got := expandBuilds([]string{"all"}, map[string][]string{
		discoverSentinel: {".#packages.x.a", ".#packages.x.b"},
	})
	want := []string{".#packages.x.a", ".#packages.x.b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// Explicit entries keep their position; the sentinel expands in place.
func TestExpandBuilds_MixedKeepsOrder(t *testing.T) {
	got := expandBuilds(
		[]string{"github:other/flake#tool", "all", ".#extra"},
		map[string][]string{discoverSentinel: {".#packages.x.a"}},
	)
	want := []string{"github:other/flake#tool", ".#packages.x.a", ".#extra"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// Naming a package explicitly alongside `all` must not build it twice.
func TestExpandBuilds_Deduplicates(t *testing.T) {
	got := expandBuilds(
		[]string{".#packages.x.a", "all"},
		map[string][]string{discoverSentinel: {".#packages.x.a", ".#packages.x.b"}},
	)
	want := []string{".#packages.x.a", ".#packages.x.b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExpandBuilds_NoSentinelIsUnchanged(t *testing.T) {
	in := []string{".#default", ".#other"}
	got := expandBuilds(in, map[string][]string{discoverSentinel: {".#packages.x.a"}})
	if !reflect.DeepEqual(got, in) {
		t.Errorf("got %v, want %v", got, in)
	}
}

func TestDiscoverLine(t *testing.T) {
	got := discoverLine(flakeOutputs{
		System:              "x86_64-linux",
		Packages:            []string{"a", "b"},
		DevShells:           []string{"default"},
		NixosConfigurations: []string{"laptop"},
	})
	for _, want := range []string{"2 packages", "1 devShells", "1 nixosConfigurations", "x86_64-linux"} {
		if !strings.Contains(got, want) {
			t.Errorf("discoverLine missing %q: %q", want, got)
		}
	}
}

// The Go struct must match the JSON the expression produces, including the
// camelCase keys nix emits for the attribute names we chose.
func TestFlakeOutputs_UnmarshalsEvalJSON(t *testing.T) {
	raw := `{"devShells":["default"],"nixosConfigurations":[],"packages":["aeroflare","default"],"system":"x86_64-linux"}`
	var o flakeOutputs
	if err := json.Unmarshal([]byte(raw), &o); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if o.System != "x86_64-linux" {
		t.Errorf("System = %q", o.System)
	}
	if !reflect.DeepEqual(o.Packages, []string{"aeroflare", "default"}) {
		t.Errorf("Packages = %v", o.Packages)
	}
	if !reflect.DeepEqual(o.DevShells, []string{"default"}) {
		t.Errorf("DevShells = %v", o.DevShells)
	}
	if o.Total() != 3 {
		t.Errorf("Total() = %d, want 3", o.Total())
	}
}

// nix.conf warnings must not bury the evaluation error underneath them.
func TestTrimEvalNoise(t *testing.T) {
	stderr := "warning: ignoring untrusted substituter 'http://x'\n" +
		"Run `man nix.conf` for more information\n" +
		"warning: ignoring the client-specified setting 'netrc-file'\n" +
		"error:\n       … while evaluating attribute 'devShells'\n" +
		"       error: path '/nix/store/x/flake.nix' does not exist\n"
	got := trimEvalNoise(stderr)
	if strings.Contains(got, "warning:") || strings.Contains(got, "man nix.conf") {
		t.Errorf("warnings survived:\n%s", got)
	}
	if !strings.HasPrefix(got, "error:") {
		t.Errorf("expected the error to lead, got:\n%s", got)
	}
	if !strings.Contains(got, "does not exist") {
		t.Errorf("dropped the actual cause:\n%s", got)
	}
}

// Output nix produced without an `error:` line is kept rather than swallowed.
func TestTrimEvalNoise_NoErrorLine(t *testing.T) {
	if got := trimEvalNoise("  killed by signal 9\n"); got != "killed by signal 9" {
		t.Errorf("got %q", got)
	}
	if got := trimEvalNoise("   \n  \n"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// A directory with no flake.nix is reported plainly instead of as a Nix trace.
func TestDiscoverFlake_NoFlake(t *testing.T) {
	_, err := discoverFlake(t.TempDir())
	if err == nil {
		t.Fatal("expected an error for a directory with no flake.nix")
	}
	if !strings.Contains(err.Error(), "no flake.nix in") {
		t.Errorf("got %v", err)
	}
}
