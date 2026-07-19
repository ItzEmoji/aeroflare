package ui

import (
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/viper"
)

// AeroflareTheme returns a huh form theme with brand colors and rounded
// borders, applied consistently across the init wizard, the settings command,
// and every interactive credential prompt.
func AeroflareTheme() *huh.Theme {
	t := huh.ThemeBase()
	// "theme" is read from viper so it picks up both the --theme flag and
	// the persisted config value (see pkg/cmd/settings).
	themeName := viper.GetString("theme")

	var primaryColor lipgloss.Color
	var secondaryColor lipgloss.Color

	switch themeName {
	case "dracula":
		primaryColor = lipgloss.Color("#bd93f9")   // Purple
		secondaryColor = lipgloss.Color("#6272a4") // Comment
	case "catppuccin":
		primaryColor = lipgloss.Color("#cba6f7")   // Mauve
		secondaryColor = lipgloss.Color("#585b70") // Surface2
	case "gruvbox-dark":
		primaryColor = lipgloss.Color("#fe8019")   // Orange
		secondaryColor = lipgloss.Color("#504945") // Bg2
	case "gruvbox-light":
		primaryColor = lipgloss.Color("#af3a03")   // Orange
		secondaryColor = lipgloss.Color("#ebdbb2") // Bg1
	default:
		primaryColor = lipgloss.Color("#00FFFF")   // Cyan
		secondaryColor = lipgloss.Color("#555555") // Gray
	}

	t.Focused.Base = t.Focused.Base.Border(lipgloss.RoundedBorder()).BorderForeground(primaryColor)
	t.Focused.Title = t.Focused.Title.Foreground(primaryColor).Bold(true)
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(primaryColor)
	t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(primaryColor)
	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(primaryColor)

	t.Blurred.Base = t.Blurred.Base.Border(lipgloss.RoundedBorder()).BorderForeground(secondaryColor)
	t.Blurred.Title = t.Blurred.Title.Foreground(secondaryColor)

	return t
}
