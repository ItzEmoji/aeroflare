package ci

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// defaultUpstream is the upstream binary cache used when none is configured.
const defaultUpstream = "https://cache.nixos.org"

// StringList is a YAML value written either as a bare scalar or as a sequence.
// Both decode to a slice, so `upstream-cache: none` and a list of URLs are
// equally valid.
type StringList []string

// UnmarshalYAML accepts a scalar or a sequence of scalars.
func (s *StringList) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		var one string
		if err := value.Decode(&one); err != nil {
			return err
		}
		*s = StringList{one}
		return nil
	case yaml.SequenceNode:
		var many []string
		if err := value.Decode(&many); err != nil {
			return err
		}
		*s = StringList(many)
		return nil
	default:
		return fmt.Errorf("line %d: upstream-cache must be a string or a list of strings", value.Line)
	}
}

// resolveUpstreams applies the upstream-cache rules: unset means the default
// cache, "none" means no filtering at all, and an explicit list replaces the
// default rather than extending it. An empty slice is indistinguishable from an
// absent key at this layer, so it is treated as unset; the JSON schema is what
// rejects `upstream-cache: []`.
func resolveUpstreams(raw []string) ([]string, error) {
	var urls []string
	none := false
	for _, entry := range raw {
		entry = strings.TrimSpace(entry)
		switch {
		case entry == "":
		case strings.EqualFold(entry, "none"):
			none = true
		default:
			urls = append(urls, entry)
		}
	}

	if none && len(urls) > 0 {
		return nil, fmt.Errorf(`upstream-cache: "none" cannot be combined with other entries`)
	}
	if none {
		return nil, nil
	}
	if len(urls) == 0 {
		return []string{defaultUpstream}, nil
	}
	return urls, nil
}

// FileConfig is the on-disk .aeroflare-ci.yaml schema.
type FileConfig struct {
	Builds         []string   `yaml:"builds"`
	Caches         []string   `yaml:"caches"`
	Compression    string     `yaml:"compression"`
	SigningKey     string     `yaml:"signing-key"`
	Workers        int        `yaml:"workers"`
	UpstreamCaches StringList `yaml:"upstream-cache"`
}

// Inputs are inline overrides (flags/env). Empty/zero fields mean "not set".
type Inputs struct {
	Builds         []string
	Caches         []string
	Compression    string
	SigningKey     string
	Workers        int
	UpstreamCaches []string
}

// RunSpec is the fully resolved configuration for one aeroflare-ci run.
type RunSpec struct {
	Builds         []string
	Caches         []CacheSpec
	Compression    string
	SigningKey     string
	Workers        int
	UpstreamCaches []string // empty disables upstream filtering
}

// LoadFile reads and parses a config file. A missing file is only an error when
// required is true (i.e. an explicit --config path); the optional default path
// returns an empty config with found=false.
func LoadFile(path string, required bool) (FileConfig, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) && !required {
			return FileConfig{}, false, nil
		}
		return FileConfig{}, false, err
	}
	var fc FileConfig
	if err := yaml.Unmarshal(data, &fc); err != nil {
		return FileConfig{}, false, fmt.Errorf("parsing %s: %w", path, err)
	}
	return fc, true, nil
}

// actionInputNames are the GitHub Action's input keys. A build entry starting
// with one of these followed by a colon is almost certainly a YAML indentation
// mistake rather than a flake installable.
var actionInputNames = []string{
	"builds", "cache", "cache-token", "compression", "config",
	"release-repo", "release-version", "signing-key", "skip-attestation",
	"token", "upstream-cache", "workers",
}

// validateBuilds rejects build entries that are really a mis-indented action
// input. `builds: |` is a YAML literal block scalar, so a sibling input indented
// one level too deep becomes another line of the builds string, and nix is then
// handed an installable like "upstream-cache: https://cache.nixos.org". Catching
// it here beats a confusing `nix build` failure.
func validateBuilds(builds []string) error {
	for _, b := range builds {
		entry := strings.TrimSpace(b)
		for _, name := range actionInputNames {
			if !strings.HasPrefix(entry, name) {
				continue
			}
			if rest := entry[len(name):]; strings.HasPrefix(rest, ":") {
				return fmt.Errorf(
					"builds contains %q, which is the %q action input, not a flake installable: "+
						"check your indentation — a line under `builds: |` is part of the builds "+
						"value, not a sibling input", entry, name)
			}
		}
	}
	return nil
}

// Resolve merges a file config with inline inputs (inputs override; list fields
// replace rather than append), applies defaults, validates, and parses caches.
func Resolve(fc FileConfig, in Inputs) (RunSpec, error) {
	spec := RunSpec{
		Builds:      fc.Builds,
		Compression: fc.Compression,
		SigningKey:  fc.SigningKey,
		Workers:     fc.Workers,
	}
	rawCaches := fc.Caches
	rawUpstreams := []string(fc.UpstreamCaches)

	if len(in.Builds) > 0 {
		spec.Builds = in.Builds
	}
	if len(in.Caches) > 0 {
		rawCaches = in.Caches
	}
	if in.Compression != "" {
		spec.Compression = in.Compression
	}
	if in.SigningKey != "" {
		spec.SigningKey = in.SigningKey
	}
	if in.Workers != 0 {
		spec.Workers = in.Workers
	}
	if len(in.UpstreamCaches) > 0 {
		rawUpstreams = in.UpstreamCaches
	}

	if spec.Compression == "" {
		spec.Compression = "zstd"
	}
	if spec.Workers == 0 {
		spec.Workers = 50
	}

	if len(spec.Builds) == 0 {
		return RunSpec{}, fmt.Errorf("no builds configured: set 'builds' in the config file or pass --build")
	}
	if err := validateBuilds(spec.Builds); err != nil {
		return RunSpec{}, err
	}
	if len(rawCaches) == 0 {
		return RunSpec{}, fmt.Errorf("no caches configured: set 'caches' in the config file or pass --cache")
	}
	upstreams, err := resolveUpstreams(rawUpstreams)
	if err != nil {
		return RunSpec{}, err
	}
	spec.UpstreamCaches = upstreams
	for _, c := range rawCaches {
		cs, err := ParseCacheSpec(c)
		if err != nil {
			return RunSpec{}, err
		}
		spec.Caches = append(spec.Caches, cs)
	}
	return spec, nil
}
