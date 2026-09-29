// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package version

import (
	_ "embed"
	"strings"
)

// toolchainMK is the repository's toolchain pins, the same file the Makefile
// includes. See toolchain.mk for why the pins live there.
//
//go:embed toolchain.mk
var toolchainMK string

// ToolchainUnreadableError reports lines of toolchain.mk that are not plain
// `NAME := value` assignments, which the Go side cannot read.
type ToolchainUnreadableError struct {
	Lines []string
}

// Error implements the error interface.
func (e *ToolchainUnreadableError) Error() string {
	return "version: toolchain.mk lines are not NAME := value: " + strings.Join(e.Lines, "; ")
}

// Toolchain returns the toolchain pins this build was released with, keyed by
// their Makefile names (GO_VERSION, GOLANGCI_VERSION, ...).
func Toolchain() (map[string]string, error) {
	return parseToolchain(toolchainMK)
}

// parseToolchain reads `NAME := value` lines, skipping blanks and comments.
func parseToolchain(text string) (map[string]string, error) {
	pins := map[string]string{}
	var unreadable []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, ":=")
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if !ok || name == "" || value == "" || strings.ContainsAny(name, " \t$()") || strings.Contains(value, "$(") {
			unreadable = append(unreadable, line)
			continue
		}
		pins[name] = value
	}
	if len(unreadable) > 0 {
		return pins, &ToolchainUnreadableError{Lines: unreadable}
	}
	return pins, nil
}
