package network

import (
	"sort"
	"strings"
)

// GCResult holds the result of a garbage collection pass
type GCResult struct {
	LivePaths  []string
	DeadPaths  []string
	FreedBytes int64
}

// RunGC performs garbage collection on the given CacheIndex.
// If maxFreed is > 0, it stops after freeing at least maxFreed bytes.
func RunGC(index *PushCacheIndex, maxFreed int64) *GCResult {
	// Parse references from all narinfos to build a graph
	graph := make(map[string][]string)
	for hash, entry := range index.Entries {
		refs := extractReferences(entry.Narinfo)
		graph[hash] = refs
	}

	// Find all live paths by traversing from GCRoots
	live := make(map[string]bool)
	queue := []string{}
	queue = append(queue, index.GCRoots...)

	for len(queue) > 0 {
		hash := queue[0]
		queue = queue[1:]

		if live[hash] {
			continue
		}
		live[hash] = true

		if refs, ok := graph[hash]; ok {
			queue = append(queue, refs...)
		}
	}

	// Identify dead paths
	result := &GCResult{}
	for hash := range index.Entries {
		if live[hash] {
			result.LivePaths = append(result.LivePaths, hash)
		} else {
			result.DeadPaths = append(result.DeadPaths, hash)
		}
	}

	sort.Strings(result.LivePaths)
	sort.Strings(result.DeadPaths)

	// Remove dead paths from index
	freed := int64(0)
	for _, hash := range result.DeadPaths {
		if maxFreed > 0 && freed >= maxFreed {
			break
		}
		freed += index.Entries[hash].NarSize
		delete(index.Entries, hash)
	}
	result.FreedBytes = freed

	return result
}

// extractReferences parses the References field from a narinfo string
func extractReferences(narinfo string) []string {
	var refs []string
	lines := strings.Split(narinfo, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "References:") {
			parts := strings.Fields(line)
			if len(parts) > 1 {
				for _, p := range parts[1:] {
					// p is typically like "hash-name"
					hashParts := strings.SplitN(p, "-", 2)
					if len(hashParts) > 0 {
						refs = append(refs, hashParts[0])
					}
				}
			}
			break
		}
	}
	return refs
}
