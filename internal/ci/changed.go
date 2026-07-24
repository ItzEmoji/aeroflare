package ci

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// changedSentinel is the `builds` entry that expands to only those outputs
// whose derivation differs from the base commit's.
const changedSentinel = "changed"

// MissingBasePolicy is what `changed` does when there is no usable base commit
// to diff against.
type MissingBasePolicy string

const (
	// MissingBaseAll builds every discovered output, as `all` would. The
	// default: a cache job that over-builds is slow, whereas one that silently
	// builds nothing leaves holes in the cache.
	MissingBaseAll MissingBasePolicy = "all"
	// MissingBaseError fails the run.
	MissingBaseError MissingBasePolicy = "error"
	// MissingBaseNone builds nothing and succeeds.
	MissingBaseNone MissingBasePolicy = "none"
)

// ParseMissingBasePolicy parses an on-missing-base value. Empty means the
// default.
func ParseMissingBasePolicy(s string) (MissingBasePolicy, error) {
	switch p := MissingBasePolicy(strings.ToLower(strings.TrimSpace(s))); p {
	case "":
		return MissingBaseAll, nil
	case MissingBaseAll, MissingBaseError, MissingBaseNone:
		return p, nil
	default:
		return "", fmt.Errorf("on-missing-base: unknown value %q, want one of: %s, %s, %s",
			s, MissingBaseAll, MissingBaseError, MissingBaseNone)
	}
}

// flakeDrvs is the changed-evaluation's result: the derivation path of each
// output the flake exposes, for one system. An output that failed to evaluate
// decodes as the empty string, since the expression yields null for it.
type flakeDrvs struct {
	System              string            `json:"system"`
	Packages            map[string]string `json:"packages"`
	DevShells           map[string]string `json:"devShells"`
	NixosConfigurations map[string]string `json:"nixosConfigurations"`
}

// Total is the number of evaluated outputs across every class.
func (d flakeDrvs) Total() int {
	return len(d.Packages) + len(d.DevShells) + len(d.NixosConfigurations)
}

// changedClass returns the names in head whose derivation differs from base's.
//
// An absent base entry reads as the empty string, so a package that is new at
// HEAD compares unequal and is built — which is what a first appearance should
// do. An output that failed to evaluate at HEAD is built unconditionally: the
// build then reports the real error, exactly as `all` does today, rather than
// swallowing a broken package in silence.
func changedClass(base, head map[string]string) []string {
	var out []string
	for name, drv := range head {
		if drv == "" || base[name] != drv {
			out = append(out, name)
		}
	}
	if out == nil {
		return nil
	}
	return sorted(out)
}

// changedOutputs returns the subset of head that differs from base, shaped as
// discovery's result so it renders through the same Installables path.
//
// Outputs present at base but gone at HEAD are absent from head and so are
// never built; there is nothing left to build them from.
func changedOutputs(base, head flakeDrvs) flakeOutputs {
	return flakeOutputs{
		System:              head.System,
		Packages:            changedClass(base.Packages, head.Packages),
		DevShells:           changedClass(base.DevShells, head.DevShells),
		NixosConfigurations: changedClass(base.NixosConfigurations, head.NixosConfigurations),
	}
}

// changedExpr renders the Nix expression that maps flakePath's outputs to their
// derivation paths.
//
// It mirrors discoverExpr's structure — one expression, `or {}` for absent
// classes, NixOS configurations filtered to the running system — and adds two
// things discovery does not need. tryEval yields null for a package that
// *throws*, rather than letting it abort the whole enumeration; and
// unsafeDiscardStringContext strips the string context a drvPath carries, which
// --json cannot serialise.
//
// tryEval is a narrow net, not a general one: it catches `throw` and `assert`
// and nothing else, so an attribute-missing or type error — a `callPacakge`
// typo, say — propagates and fails the evaluation outright. Nor is per-output
// isolation available to rescue it, because the common NUR shape
// `filterAttrs (_: isDerivation) …` forces every sibling to produce any one
// attribute: when one package there is broken, no package evaluates. A tree
// that will not evaluate is therefore treated as exactly that, and the caller
// looks to its ancestry rather than pretending the enumeration succeeded.
func changedExpr(flakePath string) string {
	return `let
  f = builtins.getFlake "` + escapeNixString(flakePath) + `";
  s = builtins.currentSystem;
  drv = d:
    let r = builtins.tryEval (builtins.unsafeDiscardStringContext d.drvPath);
    in if r.success then r.value else null;
in {
  system = s;
  packages = builtins.mapAttrs (_: drv) (f.packages.${s} or {});
  devShells = builtins.mapAttrs (_: drv) (f.devShells.${s} or {});
  nixosConfigurations = builtins.listToAttrs (map
    (n: { name = n; value = drv f.nixosConfigurations.${n}` + toplevelAttr + `; })
    (builtins.filter
      (n: (f.nixosConfigurations.${n}.pkgs.stdenv.hostPlatform.system or null) == s)
      (builtins.attrNames (f.nixosConfigurations or {}))));
}`
}

// changedLine renders the one-line roll-up of what the diff selected.
func changedLine(changed flakeOutputs, total int, base string) string {
	return fmt.Sprintf("changed  %d of %d outputs differ from %s  (%s)",
		changed.Total(), total, base, changed.System)
}

// isNullSHA reports whether s is an all-zero object name, which is what a push
// webhook reports as `before` for the first push to a branch: no commit, rather
// than a commit of all zeros. Length is not checked, so this holds for both the
// 40-character SHA-1 form and a future SHA-256 one.
func isNullSHA(s string) bool {
	return s != "" && strings.Trim(s, "0") == ""
}

// eventPayload is the slice of the GitHub webhook payload base resolution
// reads. A push carries `before`; a pull_request carries neither that nor an
// equivalent, so its base is the target branch's tip at the time the run began.
type eventPayload struct {
	Before      string `json:"before"`
	PullRequest *struct {
		Base struct {
			SHA string `json:"sha"`
		} `json:"base"`
	} `json:"pull_request"`
}

// baseFromEvent reads the base commit out of the webhook payload at path.
//
// Every failure — no path, no file, unparseable JSON, an event class carrying
// neither field, an all-zero `before` — yields the empty string rather than an
// error. None of them is a fault to report: they only mean the event cannot
// name a base, which is precisely what on-missing-base exists to decide.
func baseFromEvent(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var ev eventPayload
	if err := json.Unmarshal(data, &ev); err != nil {
		return ""
	}
	if ev.PullRequest != nil && ev.PullRequest.Base.SHA != "" {
		return ev.PullRequest.Base.SHA
	}
	if isNullSHA(ev.Before) {
		return ""
	}
	return ev.Before
}

// resolveBaseRef picks the ref to diff against: an explicit setting first, then
// whatever the CI event names, then the previous commit. The result is a ref
// git still has to resolve — verifyCommit is what decides it exists.
func resolveBaseRef(explicit, eventPath string) string {
	if e := strings.TrimSpace(explicit); e != "" {
		return e
	}
	if b := baseFromEvent(eventPath); b != "" {
		return b
	}
	return "HEAD~1"
}

// verifyCommit resolves ref to a commit sha in dir.
//
// The `^{commit}` peel is what makes this a reachability check and not just a
// syntax one: a sha absent from a shallow clone, or one a force-push orphaned,
// parses fine and fails here.
func verifyCommit(dir, ref string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	cmd.Dir = dir
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("%s is not a commit in this checkout: %s", ref, msg)
		}
		return "", fmt.Errorf("%s is not a commit in this checkout", ref)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// baseAncestryLimit bounds how far back the diff will look for a base that
// evaluates. Each step costs a worktree and an evaluation, and a repository
// whose flake has been broken for more commits than this has a problem a
// smarter base will not fix — at which point on-missing-base decides.
const baseAncestryLimit = 10

// baseAncestry returns sha and its nearest ancestors, nearest first.
//
// First-parent, so a merge commit's base is the branch's own history rather
// than everything the merge dragged in; and bounded, so the walk is a bounded
// cost rather than a history-length one. A shallow clone simply stops early at
// its boundary, which needs no special case: a short ancestry is one the walk
// can exhaust, and exhausting it is already a defined outcome.
func baseAncestry(dir, sha string) ([]string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("git", "rev-list", "--first-parent",
		fmt.Sprintf("--max-count=%d", baseAncestryLimit), sha)
	cmd.Dir = dir
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("listing the ancestry of %s: %s", shortSHA(sha), msg)
		}
		return nil, fmt.Errorf("listing the ancestry of %s: %w", shortSHA(sha), err)
	}
	return strings.Fields(stdout.String()), nil
}

// baseEval evaluates the flake at one commit and returns its derivations.
type baseEval func(sha string) (flakeDrvs, error)

// firstEvaluatable walks ancestry in order and returns the derivations of the
// first commit whose flake evaluates, along with that commit.
//
// Walking back is what keeps one broken commit from costing a full rebuild:
// the requested base failing to evaluate says nothing about whether the current
// outputs changed, only that this particular commit cannot answer the question.
// An older ancestor can, and answers it conservatively — it can only make more
// outputs look changed, never fewer, since anything that changed in between is
// still a difference from the older tree. Nothing is lost from the cache
// either: a commit skipped over is one whose own build cannot have succeeded.
func firstEvaluatable(ancestry []string, eval baseEval) (flakeDrvs, string, error) {
	if len(ancestry) == 0 {
		return flakeDrvs{}, "", fmt.Errorf("no base commit to diff against")
	}
	var err error
	for _, sha := range ancestry {
		var drvs flakeDrvs
		if drvs, err = eval(sha); err == nil {
			return drvs, sha, nil
		}
	}
	if len(ancestry) == 1 {
		return flakeDrvs{}, "", fmt.Errorf("base %s could not be evaluated: %w",
			shortSHA(ancestry[0]), err)
	}
	return flakeDrvs{}, "", fmt.Errorf(
		"base %s could not be evaluated, nor could any of its %d nearest ancestors: %w",
		shortSHA(ancestry[0]), len(ancestry)-1, err)
}

// withBaseWorktree checks sha out into a throwaway worktree of dir and calls fn
// with its path.
//
// A worktree, rather than stashing or `git show`, because evaluation needs the
// base commit's whole tree — flake.nix, flake.lock and every package file — laid
// out on disk, and it must not be disturbed by whatever the working tree
// currently holds. Removal is deferred, so an evaluation failure inside fn does
// not leave the worktree behind.
func withBaseWorktree(dir, sha string, fn func(path string) error) error {
	tmp, err := os.MkdirTemp("", "aeroflare-base-")
	if err != nil {
		return fmt.Errorf("creating a worktree directory: %w", err)
	}
	// git refuses to add a worktree at an existing path, so hand it a name
	// inside the temp directory rather than the directory itself.
	path := filepath.Join(tmp, "tree")

	var stderr bytes.Buffer
	add := exec.Command("git", "worktree", "add", "--detach", "--quiet", path, sha)
	add.Dir = dir
	add.Stderr = &stderr
	if err := add.Run(); err != nil {
		_ = os.RemoveAll(tmp)
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("checking out %s: %w\n%s", sha, err, msg)
		}
		return fmt.Errorf("checking out %s: %w", sha, err)
	}
	defer func() {
		// `git worktree remove` also drops the administrative entry under
		// .git/worktrees; removing the directory alone would leave the
		// repository listing a worktree that no longer exists.
		rm := exec.Command("git", "worktree", "remove", "--force", path)
		rm.Dir = dir
		_ = rm.Run()
		_ = os.RemoveAll(tmp)
	}()

	return fn(path)
}

// changedResult is one diff of a checkout against its base commit.
//
// MissingBase and All exist because "there is no base" is a normal outcome
// rather than a failure: the run still has to decide what to build, and only
// the configured policy can say. Both are empty on the ordinary path.
type changedResult struct {
	Changed     flakeOutputs // the outputs to build
	Total       int          // how many outputs were compared
	Base        string       // the base commit actually diffed against, abbreviated
	Requested   string       // the base asked for; differs from Base after a walk-back
	MissingBase string       // why no base was usable, if none was
	All         flakeOutputs // every discovered output, for the `all` fallback
}

// differ diffs the checkout at dir against its base commit. It is a function
// type so the orchestration can be tested without a nix evaluation.
type differ func(dir string) (changedResult, error)

// shortSHA abbreviates a commit for log lines, leaving anything that is not a
// full object name — a branch, a tag, `HEAD~1` — alone.
func shortSHA(sha string) string {
	if len(sha) < 40 {
		return sha
	}
	return sha[:7]
}

// outputs reduces the evaluated derivations to their names, so a `changed` run
// with no usable base can fall back to exactly what `all` would have built.
func (d flakeDrvs) outputs() flakeOutputs {
	names := func(m map[string]string) []string {
		if len(m) == 0 {
			return nil
		}
		out := make([]string, 0, len(m))
		for n := range m {
			out = append(out, n)
		}
		return sorted(out)
	}
	return flakeOutputs{
		System:              d.System,
		Packages:            names(d.Packages),
		DevShells:           names(d.DevShells),
		NixosConfigurations: names(d.NixosConfigurations),
	}
}

// evalDrvs evaluates dir's flake and returns each output's derivation path.
func evalDrvs(dir string) (flakeDrvs, error) {
	var out flakeDrvs
	err := evalFlake(dir, changedSentinel, changedExpr, &out)
	return out, err
}

// newDiffer returns the differ that compares a checkout's derivations against
// baseRef's, or against baseRef's nearest evaluatable ancestor.
//
// A base that is missing outright, or whose whole reachable ancestry refuses to
// evaluate, is reported through changedResult rather than as an error: neither
// is this run's fault, and the policy decides what happens next. A HEAD
// evaluation failure is an error, because there is then nothing meaningful to
// build — the same stance `all` takes today.
func newDiffer(baseRef string) differ {
	return func(dir string) (changedResult, error) {
		head, err := evalDrvs(dir)
		if err != nil {
			return changedResult{}, err
		}
		all := head.outputs()

		sha, err := verifyCommit(dir, baseRef)
		if err != nil {
			return changedResult{MissingBase: err.Error(), All: all}, nil
		}

		ancestry, err := baseAncestry(dir, sha)
		if err != nil {
			return changedResult{MissingBase: err.Error(), All: all}, nil
		}

		base, used, err := firstEvaluatable(ancestry, func(candidate string) (flakeDrvs, error) {
			var drvs flakeDrvs
			err := withBaseWorktree(dir, candidate, func(path string) error {
				var evalErr error
				drvs, evalErr = evalDrvs(path)
				return evalErr
			})
			return drvs, err
		})
		if err != nil {
			return changedResult{MissingBase: err.Error(), All: all}, nil
		}

		return changedResult{
			Changed:   changedOutputs(base, head),
			Total:     head.Total(),
			Base:      shortSHA(used),
			Requested: shortSHA(sha),
		}, nil
	}
}

// resolveChanged expands the `changed` sentinel, applying the missing-base
// policy when there is nothing to diff against.
func resolveChanged(spec *RunSpec, w io.Writer, d differ) ([]string, error) {
	res, err := d(discoverRef)
	if err != nil {
		return nil, err
	}

	if res.MissingBase == "" {
		installables := res.Changed.Installables(discoverRef)
		// Which commit the diff is against changes what its result means, so a
		// base that is not the one asked for has to be said out loud.
		if res.Requested != "" && res.Requested != res.Base {
			_, _ = fmt.Fprintf(w, "base %s could not be evaluated, diffing against %s instead\n",
				res.Requested, res.Base)
		}
		_, _ = fmt.Fprintf(w, "%s\n", changedLine(res.Changed, res.Total, res.Base))
		for _, i := range installables {
			_, _ = fmt.Fprintf(w, "  %s\n", i)
		}
		return installables, nil
	}

	switch spec.OnMissingBase {
	case MissingBaseError:
		return nil, fmt.Errorf("%s\n  set `fetch-depth: 0` on actions/checkout, "+
			"or name a base with the `base` input", res.MissingBase)
	case MissingBaseNone:
		_, _ = fmt.Fprintf(w, "%s, building nothing (on-missing-base: none)\n", res.MissingBase)
		return nil, nil
	default:
		_, _ = fmt.Fprintf(w, "%s, falling back to 'all'\n", res.MissingBase)
		_, _ = fmt.Fprintf(w, "%s\n", discoverLine(res.All))
		return res.All.Installables(discoverRef), nil
	}
}

// resolveBuilds expands both sentinels in spec.Builds in place. It shells out
// only for the sentinels actually present, and leaves a list of explicit
// installables entirely alone.
func resolveBuilds(spec *RunSpec, w io.Writer, d differ) error {
	expansions := make(map[string][]string, 2)

	if hasSentinel(spec.Builds, discoverSentinel) {
		out, err := discoverFlake(discoverRef)
		if err != nil {
			return err
		}
		if out.Total() == 0 {
			return fmt.Errorf("'all' found nothing to build: the flake exposes no packages, "+
				"devShells or nixosConfigurations for %s", out.System)
		}
		_, _ = fmt.Fprintf(w, "%s\n", discoverLine(out))
		expansions[discoverSentinel] = out.Installables(discoverRef)
	}

	if hasSentinel(spec.Builds, changedSentinel) {
		installables, err := resolveChanged(spec, w, d)
		if err != nil {
			return err
		}
		expansions[changedSentinel] = installables
	}

	spec.Builds = expandBuilds(spec.Builds, expansions)
	return nil
}
