package ui

import (
	"fmt"
	"strings"
)

type BoxField struct {
	Label string
	Value string
}

func PrintSummaryBox(title string, fields []BoxField) {
	maxLabel := 0
	for _, f := range fields {
		if len(f.Label) > maxLabel {
			maxLabel = len(f.Label)
		}
	}

	maxWidth := len(title)
	for _, f := range fields {
		lineLen := maxLabel + 1 + 1 + len(f.Value)
		if lineLen > maxWidth {
			maxWidth = lineLen
		}
	}

	width := maxWidth + 4

	horizontal := strings.Repeat("─", width)

	fmt.Println()
	fmt.Printf("  ╭%s╮\n", horizontal)
	fmt.Printf("  │  %-*s  │\n", width-4, title)
	fmt.Printf("  ├%s┤\n", horizontal)

	for _, f := range fields {
		labelStr := f.Label + ":"
		remaining := width - 4 - (maxLabel + 2)
		fmt.Printf("  │  %-*s %-*s  │\n", maxLabel+1, labelStr, remaining, f.Value)
	}

	fmt.Printf("  ╰%s╯\n", horizontal)
	fmt.Println()
}
