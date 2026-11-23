//go:build mcp

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

package mcp

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// apiDocToolToken matches a documented tool name in the API reference: an
// exact served name, or a family written as "<prefix>_*".
var apiDocToolToken = regexp.MustCompile(`^(quicbaas|qrdb|xkms|project|prompt|sdlc)_[A-Za-z0-9_]*(_\*)?$`)

// apiDocBacktick finds every backtick-quoted token on a line.
var apiDocBacktick = regexp.MustCompile("`([^`]+)`")

// TestAPIDoc_ToolInventoryMatchesServedSurface pins the "Available Tools"
// section of docs/mcp-server/api.md to the tools the composed MCP server
// serves: no documented tool may be missing from the server, and no served
// tool may be left out of the page.
func TestAPIDoc_ToolInventoryMatchesServedSurface(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/mcp-server/api.md")
	if err != nil {
		t.Fatalf("read api.md: %v", err)
	}

	lines := strings.Split(string(raw), "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimRight(line, " \r") == "## Available Tools" {
			start = i + 1
			break
		}
	}
	if start < 0 {
		t.Fatalf("api.md has no \"## Available Tools\" section")
	}

	exact := map[string]bool{}
	var families []string
	for _, line := range lines[start:] {
		if strings.HasPrefix(line, "## ") {
			break
		}
		for _, m := range apiDocBacktick.FindAllStringSubmatch(line, -1) {
			token := m[1]
			if !apiDocToolToken.MatchString(token) {
				continue
			}
			if strings.HasSuffix(token, "_*") {
				families = append(families, strings.TrimSuffix(token, "*"))
				continue
			}
			exact[token] = true
		}
	}

	served := map[string]bool{}
	specs := ModuleTools()
	for _, spec := range specs {
		served[spec.Tool] = true
	}
	for name := range qrdbNativeDuplicateTools {
		served[name] = true
	}

	var phantom []string
	for token := range exact {
		if !served[token] {
			phantom = append(phantom, token)
		}
	}
	reportAPIDocOffenders(t, "documented tools the server does not serve", phantom)

	var emptyFamilies []string
	for _, prefix := range families {
		found := false
		for name := range served {
			if strings.HasPrefix(name, prefix) {
				found = true
				break
			}
		}
		if !found {
			emptyFamilies = append(emptyFamilies, prefix+"*")
		}
	}
	reportAPIDocOffenders(t, "documented tool families that match no served tool", emptyFamilies)

	var undocumented []string
	for _, spec := range specs {
		if exact[spec.Tool] || hasAnyPrefix(spec.Tool, families) {
			continue
		}
		undocumented = append(undocumented, spec.Tool)
	}
	reportAPIDocOffenders(t, "served tools missing from api.md", undocumented)

	var publicMissing []string
	for name := range publicTools {
		if !exact[name] {
			publicMissing = append(publicMissing, name)
		}
	}
	reportAPIDocOffenders(t, "public tools not named exactly in api.md", publicMissing)
}

func hasAnyPrefix(name string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func reportAPIDocOffenders(t *testing.T, what string, names []string) {
	t.Helper()
	if len(names) == 0 {
		return
	}
	sort.Strings(names)
	t.Errorf("%s (%d): %s", what, len(names), strings.Join(names, ", "))
}
