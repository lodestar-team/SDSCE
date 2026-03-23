package ui

import (
	"os"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

var (
	// Color detection
	noColor    = os.Getenv("NO_COLOR") != ""
	termColors = detectTerminalColors()
)

// detectTerminalColors returns the number of colors supported by the terminal
func detectTerminalColors() int {
	if noColor {
		return 0
	}

	// Check if we're in a TTY
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return 0
	}

	// Check COLORTERM for truecolor support
	colorterm := os.Getenv("COLORTERM")
	if colorterm == "truecolor" || colorterm == "24bit" {
		return 3 // TrueColor
	}

	// Check TERM for 256 color support
	termEnv := os.Getenv("TERM")
	if termEnv == "xterm-256color" || termEnv == "screen-256color" {
		return 2 // ANSI256
	}

	// Default to ANSI colors
	return 1 // ANSI
}

// HasColor returns true if terminal supports colors
func HasColor() bool {
	return termColors > 0
}

// Common color palette
var (
	// Headers and labels
	ColorHeader    = lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#ffffff"}
	ColorLabel     = lipgloss.AdaptiveColor{Light: "#4a4a4a", Dark: "#a8a8a8"}
	ColorDim       = lipgloss.AdaptiveColor{Light: "#888888", Dark: "#606060"}
	ColorHighlight = lipgloss.AdaptiveColor{Light: "#0066cc", Dark: "#4da6ff"}
	ColorSuccess   = lipgloss.AdaptiveColor{Light: "#00aa00", Dark: "#00ff00"}
	ColorWarning   = lipgloss.AdaptiveColor{Light: "#cc8800", Dark: "#ffaa00"}
	ColorError     = lipgloss.AdaptiveColor{Light: "#cc0000", Dark: "#ff4444"}
	ColorAddress   = lipgloss.AdaptiveColor{Light: "#6600cc", Dark: "#9966ff"}
	ColorValue     = lipgloss.AdaptiveColor{Light: "#00aa88", Dark: "#00ffcc"}
	ColorTimestamp = lipgloss.AdaptiveColor{Light: "#6666aa", Dark: "#8888ff"}
	ColorSignature = lipgloss.AdaptiveColor{Light: "#aa6600", Dark: "#ffaa66"}
)

// Common styles
var (
	// Section header style
	StyleHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorHeader).
			MarginTop(1).
			MarginBottom(0)

	// Field label style (left side)
	StyleLabel = lipgloss.NewStyle().
			Foreground(ColorLabel).
			Bold(true).
			Width(20).
			Align(lipgloss.Right)

	// Dimmed/secondary text
	StyleDim = lipgloss.NewStyle().
			Foreground(ColorDim).
			Italic(true)

	// Success message
	StyleSuccess = lipgloss.NewStyle().
			Foreground(ColorSuccess).
			Bold(true)

	// Warning message
	StyleWarning = lipgloss.NewStyle().
			Foreground(ColorWarning).
			Bold(true)

	// Error message
	StyleError = lipgloss.NewStyle().
			Foreground(ColorError).
			Bold(true)

	// Ethereum address
	StyleAddress = lipgloss.NewStyle().
			Foreground(ColorAddress).
			Bold(false)

	// Numeric value (GRT amounts, etc.)
	StyleValue = lipgloss.NewStyle().
			Foreground(ColorValue).
			Bold(true)

	// Timestamp
	StyleTimestamp = lipgloss.NewStyle().
			Foreground(ColorTimestamp)

	// Signature/hash
	StyleSignature = lipgloss.NewStyle().
			Foreground(ColorSignature).
			Bold(false)

	// Code/monospace
	StyleCode = lipgloss.NewStyle().
			Foreground(ColorDim)

	// Separator
	StyleSeparator = lipgloss.NewStyle().
			Foreground(ColorDim).
			Faint(true)
)

// Render a field with label and value
func Field(label, value string) string {
	if !HasColor() {
		return label + ": " + value
	}
	return StyleLabel.Render(label+":") + " " + value
}

// Render a header
func Header(text string) string {
	if !HasColor() {
		return "=== " + text + " ==="
	}
	return StyleHeader.Render("▸ " + text)
}

// Render a separator line
func Separator() string {
	if !HasColor() {
		return "---"
	}
	return StyleSeparator.Render("────────────────────────────────────────")
}
