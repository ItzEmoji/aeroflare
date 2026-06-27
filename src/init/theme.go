package setup

import (
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// AeroflareTheme returns a custom theme with brand colors and rounded borders.
func AeroflareTheme() *huh.Theme {
	t := huh.ThemeBase()
	
	cyan := lipgloss.Color("#00FFFF")
	gray := lipgloss.Color("#555555")

	t.Focused.Base = t.Focused.Base.Border(lipgloss.RoundedBorder()).BorderForeground(cyan)
	t.Focused.Title = t.Focused.Title.Foreground(cyan).Bold(true)
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(cyan)
	t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(cyan)
	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(cyan)

	t.Blurred.Base = t.Blurred.Base.Border(lipgloss.RoundedBorder()).BorderForeground(gray)
	t.Blurred.Title = t.Blurred.Title.Foreground(gray)

	return t
}
