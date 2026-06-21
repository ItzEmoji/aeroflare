package store

import (
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
)

// PathInfo holds metadata about a Nix store path.
type PathInfo struct {
	Path       string   // full store path: /nix/store/<hash>-<name>
	Hash       string   // base32 hash part
	Name       string   // name part (without hash prefix)
	References []string // full store paths of references
	Deriver    string   // full store path of deriver (.drv) or empty
	System     string   // build system, e.g. "x86_64-linux"
}

// StoreBackend defines operations on a Nix store.
type StoreBackend interface {
	Closure(paths []string) ([]string, error)
	PathInfo(paths []string) ([]PathInfo, error)
	Dump(path string) (io.ReadCloser, error)
}

// LegacyStoreBackend implements StoreBackend using stable nix-store commands.
type LegacyStoreBackend struct{}

// Closure returns the combined closure of the given store paths using nix-store -qR.
func (b *LegacyStoreBackend) Closure(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	args := append([]string{"-qR"}, paths...)
	cmd := exec.Command("nix-store", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("nix-store -qR: %w", err)
	}
	var refs []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line != "" {
			refs = append(refs, line)
		}
	}
	return refs, nil
}

type pathInfoJSON struct {
	Path       string   `json:"path"`
	References []string `json:"references"`
	Deriver    string   `json:"deriver"`
	System     string   `json:"system"`
}

// PathInfo returns metadata for multiple store paths.
func (b *LegacyStoreBackend) PathInfo(paths []string) ([]PathInfo, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	args := append([]string{"path-info", "--json"}, paths...)
	cmd := exec.Command("nix", args...)
	output, err := cmd.Output()
	if err != nil {
		// Fallback to legacy nix-store commands if nix path-info fails
		var results []PathInfo
		for _, p := range paths {
			info := PathInfo{Path: p}
			hash, name, err := ParsePath(p)
			if err == nil {
				info.Hash = hash
				info.Name = name
			}
			if err := getPathInfoLegacy(p, &info); err != nil {
				return nil, err
			}
			results = append(results, info)
		}
		return results, nil
	}

	var raw json.RawMessage
	if err := json.Unmarshal(output, &raw); err != nil {
		return nil, fmt.Errorf("parse nix path-info json: %w", err)
	}

	var data []pathInfoJSON

	if len(raw) > 0 && raw[0] == '[' {
		if err := json.Unmarshal(raw, &data); err != nil {
			return nil, fmt.Errorf("parse nix path-info json array: %w", err)
		}
	} else if len(raw) > 0 && raw[0] == '{' {
		// Newer Nix versions might return a map of path -> info
		var dataMap map[string]pathInfoJSON
		if err := json.Unmarshal(raw, &dataMap); err == nil {
			for k, v := range dataMap {
				if v.Path == "" {
					v.Path = k
				}
				data = append(data, v)
			}
		} else {
			// Fallback: perhaps it's a single object?
			var single pathInfoJSON
			if err := json.Unmarshal(raw, &single); err == nil {
				data = append(data, single)
			} else {
				return nil, fmt.Errorf("parse nix path-info json object: %w", err)
			}
		}
	} else {
		return nil, fmt.Errorf("unexpected json output from nix path-info")
	}

	results := make([]PathInfo, 0, len(data))
	for _, d := range data {
		hash, name, err := ParsePath(d.Path)
		if err != nil {
			return nil, err
		}
		results = append(results, PathInfo{
			Path:       d.Path,
			Hash:       hash,
			Name:       name,
			References: d.References,
			Deriver:    d.Deriver,
			System:     d.System,
		})
	}
	return results, nil
}

func getPathInfoLegacy(path string, info *PathInfo) error {
	// Get references
	cmd := exec.Command("nix-store", "-q", "--references", path)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("nix-store -q --references: %w", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			info.References = append(info.References, line)
		}
	}

	// Get deriver
	cmd = exec.Command("nix-store", "-q", "--deriver", path)
	output, err = cmd.Output()
	if err == nil {
		d := strings.TrimSpace(string(output))
		if d != "" && d != "unknown-deriver" {
			info.Deriver = d
		}
	}

	return nil
}

type dumpReadCloser struct {
	io.ReadCloser
	cmd *exec.Cmd
}

func (d *dumpReadCloser) Close() error {
	err := d.ReadCloser.Close()
	_ = d.cmd.Wait() // wait to prevent zombie, ignore error as closing pipe might cause SIGPIPE
	return err
}

// Dump streams the NAR serialized representation of a path using nix-store --dump.
func (b *LegacyStoreBackend) Dump(path string) (io.ReadCloser, error) {
	cmd := exec.Command("nix-store", "--dump", path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start nix-store --dump: %w", err)
	}
	return &dumpReadCloser{ReadCloser: stdout, cmd: cmd}, nil
}

// ParsePath parses /nix/store/<hash>-<name> and returns the hash and name parts.
func ParsePath(path string) (hash, name string, err error) {
	base := filepath.Base(path)
	idx := strings.Index(base, "-")
	if idx < 0 {
		return "", "", fmt.Errorf("invalid store path (no hash separator): %s", path)
	}
	return base[:idx], base[idx+1:], nil
}

// GetPathInfo retrieves metadata about a single store path.
// This is a convenience wrapper around LegacyStoreBackend.PathInfo.
func GetPathInfo(path string) (*PathInfo, error) {
	b := &LegacyStoreBackend{}
	infos, err := b.PathInfo([]string{path})
	if err != nil {
		return nil, err
	}
	if len(infos) == 0 {
		return nil, fmt.Errorf("no path info returned for %s", path)
	}
	return &infos[0], nil
}
