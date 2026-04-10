// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

// Package cli provides utilities for building command-line interfaces.
// It includes table formatters, output helpers, and common CLI patterns.
package cli

import (
	"fmt"
	"io"
	"strings"
)

// TableFormatter provides formatted table output for CLI commands.
type TableFormatter struct {
	headers []string
	rows    [][]string
	widths  []int
	writer  io.Writer
	colors  bool
}

// NewTableFormatter creates a new table formatter with the given headers.
func NewTableFormatter(writer io.Writer, headers []string) *TableFormatter {
	return &TableFormatter{
		headers: headers,
		rows:    make([][]string, 0),
		widths:  make([]int, len(headers)),
		writer:  writer,
		colors:  true,
	}
}

// DisableColors disables color output.
func (t *TableFormatter) DisableColors() {
	t.colors = false
}

// AddRow adds a row to the table.
func (t *TableFormatter) AddRow(row []string) {
	if len(row) != len(t.headers) {
		// Pad or truncate to match headers
		newRow := make([]string, len(t.headers))
		copy(newRow, row)
		row = newRow
	}
	t.rows = append(t.rows, row)
}

// AddRowWithColor adds a row with color coding based on status.
func (t *TableFormatter) AddRowWithColor(row []string, color Color) {
	if len(row) != len(t.headers) {
		newRow := make([]string, len(t.headers))
		copy(newRow, row)
		row = newRow
	}

	// Apply color to the entire row if colors are enabled
	if t.colors {
		coloredRow := make([]string, len(row))
		for i, cell := range row {
			coloredRow[i] = Colorize(cell, color)
		}
		t.rows = append(t.rows, coloredRow)
	} else {
		t.rows = append(t.rows, row)
	}
}

// Render renders the table to the output writer.
func (t *TableFormatter) Render() error {
	// Calculate column widths
	t.calculateWidths()

	// Print header
	if err := t.printHeader(); err != nil {
		return err
	}

	// Print separator
	if err := t.printSeparator(); err != nil {
		return err
	}

	// Print rows
	for _, row := range t.rows {
		if err := t.printRow(row); err != nil {
			return err
		}
	}

	return nil
}

// calculateWidths calculates the maximum width for each column.
func (t *TableFormatter) calculateWidths() {
	// Initialize with header widths
	for i, header := range t.headers {
		t.widths[i] = len(header)
	}

	// Update with row widths (accounting for ANSI color codes)
	for _, row := range t.rows {
		for i, cell := range row {
			// Strip ANSI codes for width calculation
			cellLen := len(stripANSI(cell))
			if cellLen > t.widths[i] {
				t.widths[i] = cellLen
			}
		}
	}
}

// printHeader prints the table header.
func (t *TableFormatter) printHeader() error {
	parts := make([]string, len(t.headers))
	for i, header := range t.headers {
		parts[i] = t.pad(header, t.widths[i])
	}

	header := "| " + strings.Join(parts, " | ") + " |"
	if t.colors {
		header = Colorize(header, ColorBold)
	}

	_, err := fmt.Fprintln(t.writer, header)
	return err
}

// printSeparator prints a separator line.
func (t *TableFormatter) printSeparator() error {
	parts := make([]string, len(t.widths))
	for i, width := range t.widths {
		parts[i] = strings.Repeat("-", width)
	}

	separator := "|-" + strings.Join(parts, "-|-") + "-|"
	_, err := fmt.Fprintln(t.writer, separator)
	return err
}

// printRow prints a single row.
func (t *TableFormatter) printRow(row []string) error {
	parts := make([]string, len(row))
	for i, cell := range row {
		// Strip ANSI codes for width calculation, but preserve them in output
		stripped := stripANSI(cell)
		padding := t.widths[i] - len(stripped)
		parts[i] = cell + strings.Repeat(" ", padding)
	}

	_, err := fmt.Fprintln(t.writer, "| "+strings.Join(parts, " | ")+" |")
	return err
}

// pad pads a string to the specified width.
func (t *TableFormatter) pad(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// stripANSI removes ANSI color codes from a string.
func stripANSI(s string) string {
	// Simple ANSI code stripper
	result := ""
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if r == 'm' {
				inEscape = false
			}
			continue
		}
		result += string(r)
	}
	return result
}

// Color represents a terminal color.
type Color string

const (
	// ColorReset resets all attributes.
	ColorReset Color = "\033[0m"
	// ColorBold makes text bold.
	ColorBold Color = "\033[1m"
	// ColorRed makes text red.
	ColorRed Color = "\033[31m"
	// ColorGreen makes text green.
	ColorGreen Color = "\033[32m"
	// ColorYellow makes text yellow.
	ColorYellow Color = "\033[33m"
	// ColorBlue makes text blue.
	ColorBlue Color = "\033[34m"
	// ColorMagenta makes text magenta.
	ColorMagenta Color = "\033[35m"
	// ColorCyan makes text cyan.
	ColorCyan Color = "\033[36m"
	// ColorWhite makes text white.
	ColorWhite Color = "\033[37m"
	// ColorGray makes text gray.
	ColorGray Color = "\033[90m"
)

// Colorize wraps text with the specified color.
func Colorize(text string, color Color) string {
	return string(color) + text + string(ColorReset)
}

// StatusColor returns the appropriate color for a status string.
func StatusColor(status string) Color {
	switch strings.ToLower(status) {
	case "healthy", "running", "leader", "active", "ok", "up":
		return ColorGreen
	case "unhealthy", "down", "failed", "error", "stopped":
		return ColorRed
	case "starting", "stopping", "pending", "unknown":
		return ColorYellow
	case "follower", "candidate":
		return ColorCyan
	default:
		return ColorWhite
	}
}

// PrintSuccess prints a success message in green.
func PrintSuccess(writer io.Writer, message string) {
	fmt.Fprintln(writer, Colorize("✓ "+message, ColorGreen))
}

// PrintError prints an error message in red.
func PrintError(writer io.Writer, message string) {
	fmt.Fprintln(writer, Colorize("✗ "+message, ColorRed))
}

// PrintWarning prints a warning message in yellow.
func PrintWarning(writer io.Writer, message string) {
	fmt.Fprintln(writer, Colorize("⚠ "+message, ColorYellow))
}

// PrintInfo prints an info message in cyan.
func PrintInfo(writer io.Writer, message string) {
	fmt.Fprintln(writer, Colorize("ℹ "+message, ColorCyan))
}

// KeyValueFormatter provides formatted key-value output.
type KeyValueFormatter struct {
	writer io.Writer
	maxKey int
	pairs  []kvPair
	colors bool
}

type kvPair struct {
	key   string
	value string
	color Color
}

// NewKeyValueFormatter creates a new key-value formatter.
func NewKeyValueFormatter(writer io.Writer) *KeyValueFormatter {
	return &KeyValueFormatter{
		writer: writer,
		pairs:  make([]kvPair, 0),
		colors: true,
	}
}

// DisableColors disables color output.
func (kv *KeyValueFormatter) DisableColors() {
	kv.colors = false
}

// Add adds a key-value pair.
func (kv *KeyValueFormatter) Add(key, value string) {
	kv.AddWithColor(key, value, ColorWhite)
}

// AddWithColor adds a key-value pair with a specific color.
func (kv *KeyValueFormatter) AddWithColor(key, value string, color Color) {
	if len(key) > kv.maxKey {
		kv.maxKey = len(key)
	}
	kv.pairs = append(kv.pairs, kvPair{
		key:   key,
		value: value,
		color: color,
	})
}

// Render renders the key-value pairs.
func (kv *KeyValueFormatter) Render() error {
	for _, pair := range kv.pairs {
		padding := strings.Repeat(" ", kv.maxKey-len(pair.key))
		key := Colorize(pair.key+":", ColorBold)
		value := pair.value

		if kv.colors && pair.color != ColorWhite {
			value = Colorize(value, pair.color)
		}

		_, err := fmt.Fprintf(kv.writer, "%s%s %s\n", key, padding, value)
		if err != nil {
			return err
		}
	}
	return nil
}
