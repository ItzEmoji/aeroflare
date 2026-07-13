package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/itzemoji/aeroflare/internal/aerocmd"
	"github.com/itzemoji/aeroflare/pkg/cmd/root"
	"github.com/spf13/cobra/doc"
)

// sanitizeForMDX escapes angle brackets in prose so Docusaurus' MDX parser
// does not treat placeholders like <host> or <token> in command help text as
// JSX tags (which fails the build). Content inside fenced code blocks is left
// untouched, since MDX does not parse JSX there.
func sanitizeForMDX(content string) string {
	lines := strings.Split(content, "\n")
	inFence := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		line = strings.ReplaceAll(line, "<", "&lt;")
		line = strings.ReplaceAll(line, ">", "&gt;")
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

func main() {
	// Default to the Docusaurus docs tree; allow an explicit override as the
	// first argument.
	outDir := "./docs/docs/reference/cli"
	if len(os.Args) > 1 && os.Args[1] != "" {
		outDir = os.Args[1]
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(outDir, 0755); err != nil {
		fmt.Printf("Error creating directory: %v\n", err)
		os.Exit(1)
	}

	// Generate markdown documentation tree
	f := aerocmd.NewFactory("")
	rootCmd := root.NewCmdRoot(f, "", "")
	err := doc.GenMarkdownTree(rootCmd, outDir)
	if err != nil {
		fmt.Printf("Error generating markdown docs: %v\n", err)
		os.Exit(1)
	}

	// Post-process every generated page to be MDX-safe.
	entries, err := filepath.Glob(filepath.Join(outDir, "*.md"))
	if err != nil {
		fmt.Printf("Error listing generated docs: %v\n", err)
		os.Exit(1)
	}
	for _, path := range entries {
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			fmt.Printf("Error reading %s: %v\n", path, rerr)
			os.Exit(1)
		}
		if werr := os.WriteFile(path, []byte(sanitizeForMDX(string(data))), 0644); werr != nil {
			fmt.Printf("Error writing %s: %v\n", path, werr)
			os.Exit(1)
		}
	}

	fmt.Printf("Successfully generated Cobra CLI markdown documentation in %s!\n", outDir)

	// Create _category_.json for the CLI reference directory
	categoryJSON := `{
  "label": "CLI Reference (Auto-generated)",
  "position": 1,
  "link": {
    "type": "generated-index",
    "description": "Auto-generated reference documentation for Aeroflare CLI commands and flags."
  }
}
`
	err = os.WriteFile(filepath.Join(outDir, "_category_.json"), []byte(categoryJSON), 0644)
	if err != nil {
		fmt.Printf("Error writing _category_.json: %v\n", err)
		os.Exit(1)
	}
}
