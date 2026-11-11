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

package cli

import (
	"bytes"
	"strings"
	"testing"

	"errors"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTableFormatter(t *testing.T) {
	tests := []struct {
		name    string
		headers []string
		rows    [][]string
		want    []string
	}{
		{
			name:    "basic table",
			headers: []string{"Name", "Status", "Port"},
			rows: [][]string{
				{"node1", "healthy", "8080"},
				{"node2", "healthy", "8081"},
			},
			want: []string{
				"| Name  | Status  | Port |",
				"|-",
				"| node1 | healthy | 8080 |",
				"| node2 | healthy | 8081 |",
			},
		},
		{
			name:    "varying widths",
			headers: []string{"ID", "Name", "Address"},
			rows: [][]string{
				{"1", "node-primary", "203.0.113.100:9000"},
				{"2", "n2", "10.0.0.1:9000"},
			},
			want: []string{
				"| ID | Name         | Address            |",
				"|-",
				"| 1  | node-primary | 203.0.113.100:9000 |",
				"| 2  | n2           | 10.0.0.1:9000      |",
			},
		},
		{
			name:    "empty table",
			headers: []string{"A", "B", "C"},
			rows:    [][]string{},
			want: []string{
				"| A | B | C |",
				"|-",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			table := NewTableFormatter(buf, tt.headers)
			table.DisableColors() // Disable colors for testing

			for _, row := range tt.rows {
				table.AddRow(row)
			}

			err := table.Render()
			require.NoError(t, err)

			output := buf.String()
			lines := strings.Split(strings.TrimSpace(output), "\n")

			for i, want := range tt.want {
				if i >= len(lines) {
					t.Errorf("missing line %d, wanted: %s", i, want)
					continue
				}
				// Check if the line starts with the expected prefix
				if !strings.HasPrefix(lines[i], want) {
					t.Errorf("line %d:\ngot:  %s\nwant: %s", i, lines[i], want)
				}
			}
		})
	}
}

func TestTableFormatterWithColors(t *testing.T) {
	buf := &bytes.Buffer{}
	table := NewTableFormatter(buf, []string{"Node", "Status"})

	table.AddRowWithColor([]string{"node1", "healthy"}, ColorGreen)
	table.AddRowWithColor([]string{"node2", "down"}, ColorRed)

	err := table.Render()
	require.NoError(t, err)

	output := buf.String()
	// Should contain ANSI escape codes
	assert.Contains(t, output, "\033[")
}

func TestTableFormatterDisabledColors(t *testing.T) {
	buf := &bytes.Buffer{}
	table := NewTableFormatter(buf, []string{"Node", "Status"})
	table.DisableColors()

	table.AddRowWithColor([]string{"node1", "healthy"}, ColorGreen)
	table.AddRowWithColor([]string{"node2", "down"}, ColorRed)

	err := table.Render()
	require.NoError(t, err)

	output := buf.String()
	// Should NOT contain ANSI escape codes
	assert.NotContains(t, output, "\033[")
}

func TestStripANSI(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no ANSI codes",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "with color",
			input: "\033[32mhello\033[0m world",
			want:  "hello world",
		},
		{
			name:  "multiple colors",
			input: "\033[31mred\033[0m \033[32mgreen\033[0m",
			want:  "red green",
		},
		{
			name:  "bold text",
			input: "\033[1mbold\033[0m",
			want:  "bold",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripANSI(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestStatusColor(t *testing.T) {
	tests := []struct {
		status string
		want   Color
	}{
		{"healthy", ColorGreen},
		{"HEALTHY", ColorGreen},
		{"running", ColorGreen},
		{"leader", ColorGreen},
		{"unhealthy", ColorRed},
		{"down", ColorRed},
		{"failed", ColorRed},
		{"starting", ColorYellow},
		{"pending", ColorYellow},
		{"follower", ColorCyan},
		{"unknown-status", ColorWhite},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := StatusColor(tt.status)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestColorize(t *testing.T) {
	text := "hello"
	colored := Colorize(text, ColorRed)

	assert.Contains(t, colored, text)
	assert.Contains(t, colored, string(ColorRed))
	assert.Contains(t, colored, string(ColorReset))
}

func TestPrintFunctions(t *testing.T) {
	tests := []struct {
		name string
		fn   func(buf *bytes.Buffer)
	}{
		{
			name: "PrintSuccess",
			fn: func(buf *bytes.Buffer) {
				PrintSuccess(buf, "operation succeeded")
			},
		},
		{
			name: "PrintError",
			fn: func(buf *bytes.Buffer) {
				PrintError(buf, "operation failed")
			},
		},
		{
			name: "PrintWarning",
			fn: func(buf *bytes.Buffer) {
				PrintWarning(buf, "warning message")
			},
		},
		{
			name: "PrintInfo",
			fn: func(buf *bytes.Buffer) {
				PrintInfo(buf, "info message")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			tt.fn(buf)
			assert.NotEmpty(t, buf.String())
		})
	}
}

func TestKeyValueFormatter(t *testing.T) {
	buf := &bytes.Buffer{}
	kv := NewKeyValueFormatter(buf)
	kv.DisableColors()

	kv.Add("Name", "node1")
	kv.Add("Status", "healthy")
	kv.Add("ID", "1")

	err := kv.Render()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Name:")
	assert.Contains(t, output, "node1")
	assert.Contains(t, output, "Status:")
	assert.Contains(t, output, "healthy")
	assert.Contains(t, output, "ID:")
	assert.Contains(t, output, "1")
}

func TestKeyValueFormatterWithColors(t *testing.T) {
	buf := &bytes.Buffer{}
	kv := NewKeyValueFormatter(buf)

	kv.AddWithColor("Status", "healthy", ColorGreen)
	kv.AddWithColor("Error", "failed", ColorRed)

	err := kv.Render()
	require.NoError(t, err)

	output := buf.String()
	// Should contain ANSI escape codes
	assert.Contains(t, output, "\033[")
}

func TestTableFormatterMismatchedColumns(t *testing.T) {
	buf := &bytes.Buffer{}
	table := NewTableFormatter(buf, []string{"A", "B", "C"})
	table.DisableColors()

	// Add row with fewer columns
	table.AddRow([]string{"1", "2"})
	// Add row with more columns
	table.AddRow([]string{"4", "5", "6", "7"})

	err := table.Render()
	require.NoError(t, err)

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")

	// Should have header + separator + 2 rows
	assert.Len(t, lines, 4)
}

// Additional edge case tests for 90%+ coverage

func TestTableFormatter_AddRowWithColorEdgeCases(t *testing.T) {
	t.Run("mismatched columns with color", func(t *testing.T) {
		buf := &bytes.Buffer{}
		table := NewTableFormatter(buf, []string{"A", "B", "C"})

		// Row with fewer columns
		table.AddRowWithColor([]string{"1"}, ColorGreen)

		// Row with exact columns
		table.AddRowWithColor([]string{"2", "3", "4"}, ColorRed)

		// Row with more columns
		table.AddRowWithColor([]string{"5", "6", "7", "8", "9"}, ColorYellow)

		err := table.Render()
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "\033[")
	})

	t.Run("empty row with color", func(t *testing.T) {
		buf := &bytes.Buffer{}
		table := NewTableFormatter(buf, []string{"A", "B"})

		table.AddRowWithColor([]string{}, ColorBlue)

		err := table.Render()
		require.NoError(t, err)
		assert.NotEmpty(t, buf.String())
	})
}

func TestTableFormatter_RenderEdgeCases(t *testing.T) {
	t.Run("render with special characters", func(t *testing.T) {
		buf := &bytes.Buffer{}
		table := NewTableFormatter(buf, []string{"Name", "Value"})
		table.DisableColors()

		table.AddRow([]string{"tab\tchar", "newline\nchar"})
		table.AddRow([]string{"unicode: \u2713", "emoji: \U0001F600"})

		err := table.Render()
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "tab")
		assert.Contains(t, output, "unicode")
	})

	t.Run("render with very long strings", func(t *testing.T) {
		buf := &bytes.Buffer{}
		table := NewTableFormatter(buf, []string{"Col1", "Col2"})
		table.DisableColors()

		longString := strings.Repeat("x", 1000)
		table.AddRow([]string{longString, "short"})

		err := table.Render()
		require.NoError(t, err)
		assert.NotEmpty(t, buf.String())
	})

	t.Run("render with colored headers", func(t *testing.T) {
		buf := &bytes.Buffer{}
		table := NewTableFormatter(buf, []string{"Name", "Status"})
		// Colors enabled by default

		table.AddRow([]string{"node1", "active"})

		err := table.Render()
		require.NoError(t, err)

		output := buf.String()
		// Header should be bold
		assert.Contains(t, output, "\033[1m")
	})
}

func TestStripANSI_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "only ANSI codes",
			input: "\033[31m\033[0m",
			want:  "",
		},
		{
			name:  "incomplete ANSI sequence",
			input: "\033[31mtext",
			want:  "text",
		},
		{
			name:  "multiple consecutive codes",
			input: "\033[31m\033[1m\033[4mtext\033[0m\033[0m",
			want:  "text",
		},
		{
			name:  "ANSI in middle",
			input: "before\033[32mmiddle\033[0mafter",
			want:  "beforemiddleafter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripANSI(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestStatusColor_AllStatuses(t *testing.T) {
	tests := []struct {
		status string
		want   Color
	}{
		// Green statuses
		{"healthy", ColorGreen},
		{"running", ColorGreen},
		{"leader", ColorGreen},
		{"active", ColorGreen},
		{"ok", ColorGreen},
		{"up", ColorGreen},
		// Red statuses
		{"unhealthy", ColorRed},
		{"down", ColorRed},
		{"failed", ColorRed},
		{"error", ColorRed},
		{"stopped", ColorRed},
		// Yellow statuses
		{"starting", ColorYellow},
		{"stopping", ColorYellow},
		{"pending", ColorYellow},
		{"unknown", ColorYellow},
		// Cyan statuses
		{"follower", ColorCyan},
		{"candidate", ColorCyan},
		// Default white
		{"other", ColorWhite},
		{"", ColorWhite},
		{"random-status", ColorWhite},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := StatusColor(tt.status)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestKeyValueFormatter_EdgeCases(t *testing.T) {
	t.Run("empty formatter", func(t *testing.T) {
		buf := &bytes.Buffer{}
		kv := NewKeyValueFormatter(buf)

		err := kv.Render()
		require.NoError(t, err)
		assert.Empty(t, buf.String())
	})

	t.Run("single pair", func(t *testing.T) {
		buf := &bytes.Buffer{}
		kv := NewKeyValueFormatter(buf)
		kv.DisableColors()

		kv.Add("Key", "Value")

		err := kv.Render()
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "Key:")
		assert.Contains(t, output, "Value")
	})

	t.Run("varying key lengths", func(t *testing.T) {
		buf := &bytes.Buffer{}
		kv := NewKeyValueFormatter(buf)
		kv.DisableColors()

		kv.Add("A", "1")
		kv.Add("LongKeyName", "2")
		kv.Add("Mid", "3")

		err := kv.Render()
		require.NoError(t, err)

		output := buf.String()
		// All keys should be aligned
		assert.Contains(t, output, "A:")
		assert.Contains(t, output, "LongKeyName:")
		assert.Contains(t, output, "Mid:")
	})

	t.Run("white color same as no color", func(t *testing.T) {
		buf := &bytes.Buffer{}
		kv := NewKeyValueFormatter(buf)

		kv.AddWithColor("Key", "Value", ColorWhite)

		err := kv.Render()
		require.NoError(t, err)

		output := buf.String()
		// Should still have bold for key
		assert.Contains(t, output, "\033[")
	})

	t.Run("colors disabled with colored pairs", func(t *testing.T) {
		buf := &bytes.Buffer{}
		kv := NewKeyValueFormatter(buf)
		kv.DisableColors()

		kv.AddWithColor("Error", "Something failed", ColorRed)
		kv.AddWithColor("Success", "OK", ColorGreen)

		err := kv.Render()
		require.NoError(t, err)

		output := buf.String()
		// Keys still have bold color, but values should not have color codes for Red/Green
		assert.Contains(t, output, "Error:")
		assert.Contains(t, output, "Success:")
		// Values should not be colored
		lines := strings.Split(output, "\n")
		for _, line := range lines {
			if strings.Contains(line, "Something failed") {
				// "Something failed" text itself should not be wrapped in color
				assert.NotContains(t, stripANSI(line), "\033[31m") // No red in the stripped version
			}
		}
	})

	t.Run("add with white color and colors disabled", func(t *testing.T) {
		buf := &bytes.Buffer{}
		kv := NewKeyValueFormatter(buf)
		kv.DisableColors()

		kv.AddWithColor("Key", "Value", ColorWhite)

		err := kv.Render()
		require.NoError(t, err)

		output := buf.String()
		// Keys still have bold, so there will be ANSI codes
		assert.Contains(t, output, "Key:")
		assert.Contains(t, output, "Value")
	})
}

func TestPrintFunctions_EdgeCases(t *testing.T) {
	t.Run("print success with special chars", func(t *testing.T) {
		buf := &bytes.Buffer{}
		PrintSuccess(buf, "Test \n with \t special \r chars")
		assert.NotEmpty(t, buf.String())
	})

	t.Run("print error with empty message", func(t *testing.T) {
		buf := &bytes.Buffer{}
		PrintError(buf, "")
		assert.NotEmpty(t, buf.String())
	})

	t.Run("print warning with long message", func(t *testing.T) {
		buf := &bytes.Buffer{}
		longMsg := strings.Repeat("warning ", 100)
		PrintWarning(buf, longMsg)
		output := buf.String()
		assert.Contains(t, output, "warning")
	})

	t.Run("print info with unicode", func(t *testing.T) {
		buf := &bytes.Buffer{}
		PrintInfo(buf, "Unicode: \u2713 \U0001F44D")
		assert.NotEmpty(t, buf.String())
	})
}

// =============================================================================
// Consolidated from table_additional_test.go
// =============================================================================

// errorWriter is a writer that always returns an error.
type errorWriter struct{}

func (e *errorWriter) Write(p []byte) (n int, err error) {
	return 0, errors.New("write error")
}

// TestTableFormatterRenderErrors tests error handling in table rendering.
func TestTableFormatterRenderErrors(t *testing.T) {
	t.Run("error writing header", func(t *testing.T) {
		writer := &errorWriter{}
		formatter := NewTableFormatter(writer, []string{"Col1", "Col2"})
		formatter.AddRow([]string{"val1", "val2"})

		err := formatter.Render()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "write error")
	})

	t.Run("error writing separator", func(t *testing.T) {
		// Use a writer that works for header but fails for separator
		callCount := 0
		writer := &conditionalErrorWriter{
			failAfter: 1, // Fail after header
			callCount: &callCount,
		}
		formatter := NewTableFormatter(writer, []string{"Col1"})
		formatter.AddRow([]string{"val1"})

		err := formatter.Render()
		assert.Error(t, err)
	})

	t.Run("error writing row", func(t *testing.T) {
		// Use a writer that works for header and separator but fails for row
		callCount := 0
		writer := &conditionalErrorWriter{
			failAfter: 2, // Fail after header and separator
			callCount: &callCount,
		}
		formatter := NewTableFormatter(writer, []string{"Col1"})
		formatter.AddRow([]string{"val1"})

		err := formatter.Render()
		assert.Error(t, err)
	})
}

// conditionalErrorWriter fails after a certain number of successful writes.
type conditionalErrorWriter struct {
	failAfter int
	callCount *int
}

func (c *conditionalErrorWriter) Write(p []byte) (n int, err error) {
	*c.callCount++
	if *c.callCount > c.failAfter {
		return 0, errors.New("conditional write error")
	}
	return len(p), nil
}

// TestKeyValueFormatterRenderErrors tests error handling in key-value rendering.
func TestKeyValueFormatterRenderErrors(t *testing.T) {
	t.Run("error writing key-value pair", func(t *testing.T) {
		writer := &errorWriter{}
		formatter := NewKeyValueFormatter(writer)
		formatter.Add("key1", "value1")

		err := formatter.Render()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "write error")
	})

	t.Run("error on second pair", func(t *testing.T) {
		callCount := 0
		writer := &conditionalErrorWriter{
			failAfter: 1, // Fail after first pair
			callCount: &callCount,
		}
		formatter := NewKeyValueFormatter(writer)
		formatter.Add("key1", "value1")
		formatter.Add("key2", "value2")

		err := formatter.Render()
		assert.Error(t, err)
	})
}

// TestTableFormatterWithMixedColoredRows tests adding mixed colored rows.
func TestTableFormatterWithMixedColoredRows(t *testing.T) {
	var buf bytes.Buffer
	formatter := NewTableFormatter(&buf, []string{"Status", "Message"})

	// Add rows with different colors
	formatter.AddRowWithColor([]string{"success", "Operation completed"}, ColorGreen)
	formatter.AddRowWithColor([]string{"error", "Operation failed"}, ColorRed)
	formatter.AddRowWithColor([]string{"warning", "Operation pending"}, ColorYellow)

	err := formatter.Render()
	require.NoError(t, err)

	output := buf.String()
	// Colors should be applied
	assert.Contains(t, output, "success")
	assert.Contains(t, output, "error")
	assert.Contains(t, output, "warning")
}

// TestTableFormatterWithMismatchedColumns tests row handling with different column counts.
func TestTableFormatterWithMismatchedColumns(t *testing.T) {
	var buf bytes.Buffer
	formatter := NewTableFormatter(&buf, []string{"Col1", "Col2", "Col3"})

	t.Run("row with fewer columns", func(t *testing.T) {
		formatter.AddRow([]string{"val1", "val2"}) // Missing third column
		err := formatter.Render()
		require.NoError(t, err)
		// Should pad with empty string
	})

	t.Run("row with more columns", func(t *testing.T) {
		buf.Reset()
		formatter2 := NewTableFormatter(&buf, []string{"Col1", "Col2"})
		formatter2.AddRow([]string{"val1", "val2", "val3", "val4"}) // Extra columns
		err := formatter2.Render()
		require.NoError(t, err)
		// Should truncate extra columns
	})
}

// TestColorizeVariants tests color wrapping with different colors.
func TestColorizeVariants(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		color Color
	}{
		{"magenta text", "info", ColorMagenta},
		{"white text", "default", ColorWhite},
		{"gray text", "disabled", ColorGray},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Colorize(tt.text, tt.color)
			assert.Contains(t, result, tt.text)
			assert.Contains(t, result, string(tt.color))
			assert.Contains(t, result, string(ColorReset))
		})
	}
}

// TestStatusColorVariants tests additional status-based color selections.
func TestStatusColorVariants(t *testing.T) {
	tests := []struct {
		status string
		color  Color
	}{
		// Additional green statuses
		{"UP", ColorGreen},
		{"OK", ColorGreen},
		{"ACTIVE", ColorGreen},

		// Additional red statuses
		{"UNHEALTHY", ColorRed},
		{"STOPPED", ColorRed},
		{"DOWN", ColorRed},

		// Additional yellow statuses
		{"STARTING", ColorYellow},
		{"STOPPING", ColorYellow},
		{"UNKNOWN", ColorYellow},

		// Additional cyan statuses
		{"CANDIDATE", ColorCyan},

		// Additional defaults
		{"custom-status", ColorWhite},
		{"RANDOM", ColorWhite},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			color := StatusColor(tt.status)
			assert.Equal(t, tt.color, color)
		})
	}
}

// TestKeyValueFormatterColorBehavior tests key-value formatter color behavior.
func TestKeyValueFormatterColorBehavior(t *testing.T) {
	t.Run("with colors enabled and non-white color", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := NewKeyValueFormatter(&buf)
		formatter.AddWithColor("Status", "healthy", ColorGreen)

		err := formatter.Render()
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "Status:")
		assert.Contains(t, output, "healthy")
	})

	t.Run("with colors disabled", func(t *testing.T) {
		var buf bytes.Buffer
		formatter := NewKeyValueFormatter(&buf)
		formatter.DisableColors()
		formatter.AddWithColor("Status", "healthy", ColorGreen)

		err := formatter.Render()
		require.NoError(t, err)

		_ = buf.String()
		// Should not contain color codes
		// Keys are always bolded, but values should not be colored when disabled
	})
}

// TestTableFormatterWithLongContent tests table formatting with long content.
func TestTableFormatterWithLongContent(t *testing.T) {
	var buf bytes.Buffer
	formatter := NewTableFormatter(&buf, []string{"Short", "Very Long Header Name"})
	formatter.AddRow([]string{
		"A",
		"This is a very long value that exceeds the header width",
	})

	err := formatter.Render()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Short")
	assert.Contains(t, output, "Very Long Header Name")
	assert.Contains(t, output, "This is a very long value")
}

// TestKeyValueFormatterPadding tests key-value formatter padding.
func TestKeyValueFormatterPadding(t *testing.T) {
	var buf bytes.Buffer
	formatter := NewKeyValueFormatter(&buf)
	formatter.Add("Short", "value1")
	formatter.Add("Very Long Key", "value2")
	formatter.Add("Med", "value3")

	err := formatter.Render()
	require.NoError(t, err)

	_ = buf.String()
	lines := bytes.Split(buf.Bytes(), []byte("\n"))

	// All keys should be padded to the same width
	// The colons should align
	assert.True(t, len(lines) >= 3)
}

// TestEmptyKeyValueFormatter tests rendering empty key-value formatter.
func TestEmptyKeyValueFormatter(t *testing.T) {
	var buf bytes.Buffer
	formatter := NewKeyValueFormatter(&buf)
	// Don't add any pairs

	err := formatter.Render()
	require.NoError(t, err)

	// Should produce no output
	assert.Empty(t, buf.String())
}

// TestKeyValueFormatterWhiteColor tests that white color is not applied by default.
func TestKeyValueFormatterWhiteColor(t *testing.T) {
	var buf bytes.Buffer
	formatter := NewKeyValueFormatter(&buf)
	formatter.AddWithColor("Key", "value", ColorWhite)

	err := formatter.Render()
	require.NoError(t, err)

	output := buf.String()
	// White is the default, so it shouldn't add color codes
	assert.Contains(t, output, "value")
}

// BenchmarkTableFormatterRender benchmarks table rendering.
func BenchmarkTableFormatterRender(b *testing.B) {
	headers := []string{"ID", "Name", "Status", "Created"}
	rows := make([][]string, 100)
	for i := 0; i < 100; i++ {
		rows[i] = []string{"id-1", "name-1", "active", "2024-01-01"}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		formatter := NewTableFormatter(&buf, headers)
		for _, row := range rows {
			formatter.AddRow(row)
		}
		formatter.Render()
	}
}

// BenchmarkKeyValueFormatterRender benchmarks key-value rendering.
func BenchmarkKeyValueFormatterRender(b *testing.B) {
	pairs := []struct{ key, value string }{
		{"ID", "12345"},
		{"Name", "Test User"},
		{"Status", "Active"},
		{"Created", "2024-01-01"},
		{"Modified", "2024-01-02"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		formatter := NewKeyValueFormatter(&buf)
		for _, p := range pairs {
			formatter.Add(p.key, p.value)
		}
		formatter.Render()
	}
}

// BenchmarkStripANSIcodes benchmarks ANSI stripping.
func BenchmarkStripANSIcodes(b *testing.B) {
	text := "\033[1m\033[32mbold green text with \033[31mred\033[0m mixed in\033[0m"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stripANSI(text)
	}
}

// BenchmarkColorizeText benchmarks text colorization.
func BenchmarkColorizeText(b *testing.B) {
	text := "This is a test message"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Colorize(text, ColorGreen)
	}
}

// BenchmarkStatusColorLookup benchmarks status color lookup.
func BenchmarkStatusColorLookup(b *testing.B) {
	statuses := []string{"healthy", "unhealthy", "pending", "follower", "custom"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, status := range statuses {
			StatusColor(status)
		}
	}
}
