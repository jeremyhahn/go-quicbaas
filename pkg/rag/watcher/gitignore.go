//go:build rag

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

package watcher

import (
	"path/filepath"
	"strconv"
	"strings"
)

// gitIgnoreMatcher matches paths against .gitignore-style patterns.
type gitIgnoreMatcher struct {
	patterns []pattern
}

// pattern represents a single gitignore pattern.
type pattern struct {
	raw     string // Original pattern
	negated bool   // True if pattern starts with !
	dirOnly bool   // True if pattern ends with /
	glob    string // Pattern for matching
}

// newGitIgnoreMatcher creates a new gitignore matcher with the given patterns.
func newGitIgnoreMatcher(patterns []string) (*gitIgnoreMatcher, error) {
	m := &gitIgnoreMatcher{
		patterns: make([]pattern, 0, len(patterns)),
	}

	for _, p := range patterns {
		if p == "" {
			continue
		}

		pat := m.parsePattern(p)
		// Reject a malformed glob here so matching never meets one.
		if _, err := filepath.Match(filepath.ToSlash(pat.glob), ""); err != nil {
			return nil, &WatcherCauseError{Message: "invalid exclude pattern " + strconv.Quote(p), Cause: err}
		}
		m.patterns = append(m.patterns, pat)
	}

	return m, nil
}

// parsePattern parses a gitignore pattern.
func (m *gitIgnoreMatcher) parsePattern(raw string) pattern {
	p := pattern{
		raw: raw,
	}

	// Check for negation
	if strings.HasPrefix(raw, "!") {
		p.negated = true
		raw = raw[1:]
	}

	// Check for directory-only pattern
	if strings.HasSuffix(raw, "/") {
		p.dirOnly = true
		raw = strings.TrimSuffix(raw, "/")
	}

	// Remove leading slash
	raw = strings.TrimPrefix(raw, "/")

	p.glob = raw
	return p
}

// Match checks if a path matches any of the ignore patterns.
// Returns true if the path should be excluded.
func (m *gitIgnoreMatcher) Match(path string) bool {
	// Normalize path
	path = filepath.Clean(path)

	matched := false

	for _, p := range m.patterns {
		if m.matchPattern(path, p) {
			if p.negated {
				matched = false
			} else {
				matched = true
			}
		}
	}

	return matched
}

// matchPattern checks if a path matches a single pattern.
func (m *gitIgnoreMatcher) matchPattern(path string, p pattern) bool {
	// Normalize path separators
	path = filepath.ToSlash(path)
	glob := filepath.ToSlash(p.glob)

	// For directory-only patterns, check if path is or contains a directory
	if p.dirOnly {
		// Check if the path ends with the directory name
		if strings.Contains(path, glob+"/") || strings.HasSuffix(path, "/"+glob) || path == glob {
			return true
		}
	}

	// Simple pattern matching
	if globMatch(glob, filepath.Base(path)) {
		return true
	}

	// Try matching against full path
	if globMatch(glob, path) {
		return true
	}

	// Try matching with ** wildcard
	if strings.Contains(glob, "**") {
		pattern := strings.ReplaceAll(glob, "**", "*")
		if m.matchWildcard(path, pattern) {
			return true
		}
	}

	// Check if any path component matches for non-wildcard patterns only.
	// Wildcard patterns like "*.tmp" should only match the basename (already checked above).
	// Simple name patterns like "node_modules" should match any path component.
	if !strings.ContainsAny(glob, "*?[") {
		parts := strings.Split(path, "/")
		for _, part := range parts {
			if part == glob {
				return true
			}
		}
	}

	return false
}

// matchWildcard performs wildcard matching with * support.
func (m *gitIgnoreMatcher) matchWildcard(path, pattern string) bool {
	// Simple wildcard matching
	if pattern == "*" {
		return true
	}

	if globMatch(pattern, path) {
		return true
	}

	// Check all path segments
	parts := strings.Split(path, "/")
	for i := range parts {
		segment := strings.Join(parts[i:], "/")
		if globMatch(pattern, segment) {
			return true
		}
	}

	return false
}

// globMatch reports whether name matches glob. newGitIgnoreMatcher rejects
// malformed globs, so Match's only error, ErrBadPattern, means no match.
func globMatch(glob, name string) bool {
	matched, err := filepath.Match(glob, name)
	return err == nil && matched
}
