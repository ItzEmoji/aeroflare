package ci

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestHasSentinel(t *testing.T) {
	cases := []struct {
		name     string
		builds   []string
		sentinel string
		want     bool
	}{
		{"absent", []string{".#default"}, changedSentinel, false},
		{"bare", []string{"changed"}, changedSentinel, true},
		{"mixed with explicit", []string{".#default", "changed"}, changedSentinel, true},
		{"surrounding whitespace", []string{"  changed\t"}, changedSentinel, true},
		{"substring is not the sentinel", []string{".#changed", "unchanged"}, changedSentinel, false},
		{"the other sentinel does not match", []string{"all"}, changedSentinel, false},
		{"empty", nil, changedSentinel, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasSentinel(tc.builds, tc.sentinel); got != tc.want {
				t.Errorf("hasSentinel(%v, %q) = %v, want %v", tc.builds, tc.sentinel, got, tc.want)
			}
		})
	}
}

// Both sentinels expand in one pass, each in place, sharing one dedup set.
func TestExpandBuilds_BothSentinels(t *testing.T) {
	got := expandBuilds(
		[]string{"changed", ".#extra", "all"},
		map[string][]string{
			"changed": {".#packages.x.a"},
			"all":     {".#packages.x.a", ".#packages.x.b"},
		},
	)
	want := []string{".#packages.x.a", ".#extra", ".#packages.x.b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// A sentinel that expanded to nothing drops out rather than reaching nix as a
// literal installable named "changed".
func TestExpandBuilds_EmptyExpansionDropsSentinel(t *testing.T) {
	got := expandBuilds([]string{"changed"}, map[string][]string{"changed": nil})
	if len(got) != 0 {
		t.Errorf("got %v, want none", got)
	}
}

// An output whose derivation is unchanged must not be rebuilt; everything else
// must be.
func TestChangedOutputs(t *testing.T) {
	base := flakeDrvs{
		System: "x86_64-linux",
		Packages: map[string]string{
			"same":    "/nix/store/aaa.drv",
			"bumped":  "/nix/store/bbb.drv",
			"removed": "/nix/store/ccc.drv",
			"broken":  "", // failed to evaluate at base
		},
		DevShells: map[string]string{"default": "/nix/store/ddd.drv"},
	}
	head := flakeDrvs{
		System: "x86_64-linux",
		Packages: map[string]string{
			"same":     "/nix/store/aaa.drv",
			"bumped":   "/nix/store/zzz.drv",
			"added":    "/nix/store/eee.drv",
			"broken":   "/nix/store/fff.drv",
			"unevaled": "", // failed to evaluate at head
		},
		DevShells: map[string]string{"default": "/nix/store/ddd.drv"},
	}
	got := changedOutputs(base, head)
	want := flakeOutputs{
		System:   "x86_64-linux",
		Packages: []string{"added", "broken", "bumped", "unevaled"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("changedOutputs() =\n%+v\nwant\n%+v", got, want)
	}
}

// A commit that touches nothing buildable yields an empty set, not everything.
func TestChangedOutputs_Identical(t *testing.T) {
	o := flakeDrvs{
		System:              "x86_64-linux",
		Packages:            map[string]string{"a": "/nix/store/a.drv"},
		NixosConfigurations: map[string]string{"laptop": "/nix/store/l.drv"},
	}
	if got := changedOutputs(o, o); got.Total() != 0 {
		t.Errorf("expected nothing changed, got %+v", got)
	}
}

// A flake.lock bump changes every derivation, so every class comes back.
func TestChangedOutputs_AllClasses(t *testing.T) {
	base := flakeDrvs{
		System:              "x86_64-linux",
		Packages:            map[string]string{"a": "/nix/store/1.drv"},
		DevShells:           map[string]string{"default": "/nix/store/2.drv"},
		NixosConfigurations: map[string]string{"laptop": "/nix/store/3.drv"},
	}
	head := flakeDrvs{
		System:              "x86_64-linux",
		Packages:            map[string]string{"a": "/nix/store/4.drv"},
		DevShells:           map[string]string{"default": "/nix/store/5.drv"},
		NixosConfigurations: map[string]string{"laptop": "/nix/store/6.drv"},
	}
	got := changedOutputs(base, head)
	want := flakeOutputs{
		System:              "x86_64-linux",
		Packages:            []string{"a"},
		DevShells:           []string{"default"},
		NixosConfigurations: []string{"laptop"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// The Go struct must match the JSON changedExpr produces, including a null for
// an output that failed to evaluate.
func TestFlakeDrvs_UnmarshalsEvalJSON(t *testing.T) {
	raw := `{"devShells":{"default":"/nix/store/d.drv"},"nixosConfigurations":{},` +
		`"packages":{"aeroflare":"/nix/store/a.drv","broken":null},"system":"x86_64-linux"}`
	var d flakeDrvs
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d.System != "x86_64-linux" {
		t.Errorf("System = %q", d.System)
	}
	if d.Packages["aeroflare"] != "/nix/store/a.drv" {
		t.Errorf("Packages[aeroflare] = %q", d.Packages["aeroflare"])
	}
	if got, ok := d.Packages["broken"]; !ok || got != "" {
		t.Errorf("a null drvPath must decode to the empty string, got %q (present=%v)", got, ok)
	}
	if d.Total() != 3 {
		t.Errorf("Total() = %d, want 3", d.Total())
	}
}

// The path is interpolated into a Nix string literal, so a path containing a
// quote must not be able to break out of it and inject an expression.
func TestChangedExpr_EscapesPath(t *testing.T) {
	expr := changedExpr(`/tmp/a"; abort "pwned`)
	want := `  f = builtins.getFlake "/tmp/a\"; abort \"pwned";`
	if !strings.Contains(expr, want) {
		t.Errorf("path did not survive as one escaped literal, want line\n%s\ngot\n%s", want, expr)
	}
}

// Every output must go through tryEval, or one broken package aborts the whole
// enumeration; and drvPath carries string context that --json cannot serialise.
func TestChangedExpr_TolerantAndSerialisable(t *testing.T) {
	expr := changedExpr("/repo")
	for _, want := range []string{
		"builtins.tryEval",
		"builtins.unsafeDiscardStringContext",
		".drvPath",
		"f.packages.${s} or {}",
		"f.devShells.${s} or {}",
		"f.nixosConfigurations or {}",
		toplevelAttr,
	} {
		if !strings.Contains(expr, want) {
			t.Errorf("expression is missing %q:\n%s", want, expr)
		}
	}
}

func TestParseMissingBasePolicy(t *testing.T) {
	cases := map[string]MissingBasePolicy{
		"":      MissingBaseAll,
		"all":   MissingBaseAll,
		"ALL":   MissingBaseAll,
		" all ": MissingBaseAll,
		"error": MissingBaseError,
		"none":  MissingBaseNone,
	}
	for in, want := range cases {
		got, err := ParseMissingBasePolicy(in)
		if err != nil {
			t.Errorf("ParseMissingBasePolicy(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseMissingBasePolicy(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseMissingBasePolicy_Unknown(t *testing.T) {
	_, err := ParseMissingBasePolicy("fallback")
	if err == nil {
		t.Fatal("expected an error for an unknown policy")
	}
	// The message has to name the valid values; the input alone does not tell
	// the user what to write instead.
	for _, want := range []string{"fallback", "all", "error", "none"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error is missing %q: %v", want, err)
		}
	}
}

func TestChangedLine(t *testing.T) {
	got := changedLine(
		flakeOutputs{System: "x86_64-linux", Packages: []string{"a", "b"}},
		14, "f3a9c21",
	)
	for _, want := range []string{"2 of 14", "f3a9c21", "x86_64-linux"} {
		if !strings.Contains(got, want) {
			t.Errorf("changedLine missing %q: %q", want, got)
		}
	}
}

func TestIsNullSHA(t *testing.T) {
	cases := map[string]bool{
		"0000000000000000000000000000000000000000": true,
		"0000000": true,
		"":        false,
		"f3a9c21": false,
		"0000000000000000000000000000000000000001": false,
	}
	for in, want := range cases {
		if got := isNullSHA(in); got != want {
			t.Errorf("isNullSHA(%q) = %v, want %v", in, got, want)
		}
	}
}

// A push payload's `before` is the commit the branch pointed at.
func TestBaseFromEvent_Push(t *testing.T) {
	path := writeEvent(t, `{"before":"f3a9c21aaaa","after":"bbb"}`)
	if got := baseFromEvent(path); got != "f3a9c21aaaa" {
		t.Errorf("got %q, want the before sha", got)
	}
}

// A pull_request payload has no `before`; the base is the target branch's tip.
func TestBaseFromEvent_PullRequest(t *testing.T) {
	path := writeEvent(t, `{"pull_request":{"base":{"sha":"deadbeef"},"head":{"sha":"cafe"}}}`)
	if got := baseFromEvent(path); got != "deadbeef" {
		t.Errorf("got %q, want the pull request base sha", got)
	}
}

// The first push to a branch reports an all-zero `before`, which is not a
// commit and must not be handed to git.
func TestBaseFromEvent_NullBefore(t *testing.T) {
	path := writeEvent(t, `{"before":"0000000000000000000000000000000000000000"}`)
	if got := baseFromEvent(path); got != "" {
		t.Errorf("got %q, want none", got)
	}
}

// Events that carry neither field — workflow_dispatch, schedule — and a missing
// or malformed file all yield nothing rather than an error.
func TestBaseFromEvent_Unusable(t *testing.T) {
	cases := map[string]string{
		"no relevant fields": `{"ref":"refs/heads/main"}`,
		"malformed":          `{not json`,
		"empty":              ``,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if got := baseFromEvent(writeEvent(t, body)); got != "" {
				t.Errorf("got %q, want none", got)
			}
		})
	}
	if got := baseFromEvent(filepath.Join(t.TempDir(), "absent.json")); got != "" {
		t.Errorf("missing file: got %q, want none", got)
	}
	if got := baseFromEvent(""); got != "" {
		t.Errorf("unset GITHUB_EVENT_PATH: got %q, want none", got)
	}
}

// An explicit base beats the event payload, which beats the local fallback.
func TestResolveBaseRef_Precedence(t *testing.T) {
	event := writeEvent(t, `{"before":"f3a9c21"}`)
	cases := []struct {
		name      string
		explicit  string
		eventPath string
		want      string
	}{
		{"explicit wins", "origin/main", event, "origin/main"},
		{"event when no explicit", "", event, "f3a9c21"},
		{"local fallback", "", "", "HEAD~1"},
		{"explicit is trimmed", "  origin/main\n", event, "origin/main"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveBaseRef(tc.explicit, tc.eventPath); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// verifyCommit has to resolve a real ref and reject one git cannot reach, since
// that is exactly what a shallow clone and a force-push look like.
func TestVerifyCommit(t *testing.T) {
	repo := initRepo(t)
	head := gitOut(t, repo, "rev-parse", "HEAD")

	got, err := verifyCommit(repo, "HEAD")
	if err != nil {
		t.Fatalf("verifyCommit(HEAD): %v", err)
	}
	if got != head {
		t.Errorf("got %q, want %q", got, head)
	}
	if _, err := verifyCommit(repo, "HEAD~1"); err != nil {
		t.Errorf("verifyCommit(HEAD~1): %v", err)
	}
	if _, err := verifyCommit(repo, "HEAD~99"); err == nil {
		t.Error("expected an error for a commit that is not in the repository")
	}
	if _, err := verifyCommit(repo, "0123456789012345678901234567890123456789"); err == nil {
		t.Error("expected an error for an unknown sha")
	}
}

// The worktree must expose the base commit's content, not the working tree's,
// and must be gone afterwards.
func TestWithBaseWorktree(t *testing.T) {
	repo := initRepo(t)
	base := gitOut(t, repo, "rev-parse", "HEAD~1")

	var seen, path string
	err := withBaseWorktree(repo, base, func(dir string) error {
		path = dir
		b, err := os.ReadFile(filepath.Join(dir, "version.txt"))
		seen = string(b)
		return err
	})
	if err != nil {
		t.Fatalf("withBaseWorktree: %v", err)
	}
	if seen != "1\n" {
		t.Errorf("worktree content = %q, want the base commit's %q", seen, "1\n")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("worktree %s outlived the call", path)
	}
}

// A failure inside the callback still tears the worktree down.
func TestWithBaseWorktree_CleansUpOnError(t *testing.T) {
	repo := initRepo(t)
	base := gitOut(t, repo, "rev-parse", "HEAD~1")

	var path string
	err := withBaseWorktree(repo, base, func(dir string) error {
		path = dir
		return errors.New("boom")
	})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected the callback's error, got %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("worktree %s outlived the call", path)
	}
}

// --- helpers ---------------------------------------------------------------

// writeEvent writes a webhook payload to a temp file and returns its path.
func writeEvent(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing event: %v", err)
	}
	return path
}

// initRepo builds a two-commit repository, so HEAD~1 exists and differs.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "--quiet", "-b", "main")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "user.name", "test")
	git(t, dir, "config", "commit.gpgsign", "false")
	for _, v := range []string{"1", "2"} {
		if err := os.WriteFile(filepath.Join(dir, "version.txt"), []byte(v+"\n"), 0o644); err != nil {
			t.Fatalf("writing version.txt: %v", err)
		}
		git(t, dir, "add", "version.txt")
		git(t, dir, "commit", "--quiet", "-m", "v"+v)
	}
	return dir
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// The whole point: an unchanged repository builds nothing at all.
func TestResolveBuilds_ChangedNothing(t *testing.T) {
	spec := RunSpec{Builds: []string{changedSentinel}}
	var sb strings.Builder
	err := resolveBuilds(&spec, &sb, fakeDiff(t, flakeOutputs{System: "x86_64-linux"}, 12, "f3a9c21", nil))
	if err != nil {
		t.Fatalf("resolveBuilds: %v", err)
	}
	if len(spec.Builds) != 0 {
		t.Errorf("expected no builds, got %v", spec.Builds)
	}
	if !strings.Contains(sb.String(), "0 of 12") {
		t.Errorf("expected a roll-up naming the counts, got %q", sb.String())
	}
}

func TestResolveBuilds_ChangedSubset(t *testing.T) {
	spec := RunSpec{Builds: []string{changedSentinel}}
	changed := flakeOutputs{System: "x86_64-linux", Packages: []string{"aeroflare"}}
	var sb strings.Builder
	if err := resolveBuilds(&spec, &sb, fakeDiff(t, changed, 12, "f3a9c21", nil)); err != nil {
		t.Fatalf("resolveBuilds: %v", err)
	}
	want := []string{".#packages.x86_64-linux.aeroflare"}
	if !reflect.DeepEqual(spec.Builds, want) {
		t.Errorf("got %v, want %v", spec.Builds, want)
	}
}

// Explicit installables listed next to the sentinel are always built, changed
// or not.
func TestResolveBuilds_ChangedKeepsExplicit(t *testing.T) {
	spec := RunSpec{Builds: []string{changedSentinel, ".#devShells.x86_64-linux.default"}}
	var sb strings.Builder
	if err := resolveBuilds(&spec, &sb, fakeDiff(t, flakeOutputs{System: "x86_64-linux"}, 12, "f3a9c21", nil)); err != nil {
		t.Fatalf("resolveBuilds: %v", err)
	}
	want := []string{".#devShells.x86_64-linux.default"}
	if !reflect.DeepEqual(spec.Builds, want) {
		t.Errorf("got %v, want %v", spec.Builds, want)
	}
}

// Runs asking for neither sentinel must not shell out to nix or git at all.
func TestResolveBuilds_NoSentinelIsNoOp(t *testing.T) {
	spec := RunSpec{Builds: []string{".#default"}}
	var sb strings.Builder
	differ := func(string) (changedResult, error) {
		t.Fatal("the differ ran without a `changed` entry")
		return changedResult{}, nil
	}
	if err := resolveBuilds(&spec, &sb, differ); err != nil {
		t.Fatalf("resolveBuilds: %v", err)
	}
	if !reflect.DeepEqual(spec.Builds, []string{".#default"}) {
		t.Errorf("builds changed: %v", spec.Builds)
	}
	if sb.String() != "" {
		t.Errorf("expected no output, got %q", sb.String())
	}
}

func TestResolveBuilds_MissingBase(t *testing.T) {
	all := flakeOutputs{
		System:   "x86_64-linux",
		Packages: []string{"a", "b"},
	}
	cases := []struct {
		name    string
		policy  MissingBasePolicy
		want    []string
		wantErr bool
	}{
		{"all rebuilds everything", MissingBaseAll,
			[]string{".#packages.x86_64-linux.a", ".#packages.x86_64-linux.b"}, false},
		{"none builds nothing", MissingBaseNone, nil, false},
		{"error fails the run", MissingBaseError, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := RunSpec{Builds: []string{changedSentinel}, OnMissingBase: tc.policy}
			var sb strings.Builder
			differ := func(string) (changedResult, error) {
				return changedResult{
					MissingBase: "no base commit: HEAD~1 is not a commit in this checkout",
					All:         all,
				}, nil
			}
			err := resolveBuilds(&spec, &sb, differ)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveBuilds: %v", err)
			}
			if !reflect.DeepEqual(spec.Builds, tc.want) {
				t.Errorf("got %v, want %v", spec.Builds, tc.want)
			}
			// The reason has to reach the log, or a silently reduced build set
			// looks like a correctly empty one.
			if !strings.Contains(sb.String(), "no base commit") {
				t.Errorf("the reason was not reported: %q", sb.String())
			}
		})
	}
}

// fakeDiff builds a differ returning a fixed result, so the orchestration is
// testable without a nix evaluation.
func fakeDiff(t *testing.T, changed flakeOutputs, total int, base string, err error) differ {
	t.Helper()
	return func(string) (changedResult, error) {
		return changedResult{Changed: changed, Total: total, Base: base}, err
	}
}
