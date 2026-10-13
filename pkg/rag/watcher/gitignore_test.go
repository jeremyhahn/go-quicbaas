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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGitIgnoreMatcher(t *testing.T) {
	patterns := []string{"*.txt", "temp/", "!important.txt"}
	matcher, err := newGitIgnoreMatcher(patterns)

	require.NoError(t, err)
	assert.NotNil(t, matcher)
	assert.Len(t, matcher.patterns, 3)
}

func TestNewGitIgnoreMatcher_EmptyPatterns(t *testing.T) {
	matcher, err := newGitIgnoreMatcher([]string{})

	require.NoError(t, err)
	assert.NotNil(t, matcher)
	assert.Empty(t, matcher.patterns)
}

func TestNewGitIgnoreMatcher_RejectsMalformedGlob(t *testing.T) {
	for _, raw := range []string{"[unclosed", "!/logs/[a-/", "dir/\\"} {
		_, err := newGitIgnoreMatcher([]string{"*.tmp", raw})

		var causeErr *WatcherCauseError
		require.ErrorAs(t, err, &causeErr, raw)
		require.ErrorIs(t, err, filepath.ErrBadPattern, raw)
		assert.Contains(t, err.Error(), raw)
	}
}

func TestGlobMatch(t *testing.T) {
	assert.True(t, globMatch("*.go", "main.go"))
	assert.False(t, globMatch("*.go", "main.rs"))
	assert.False(t, globMatch("[unclosed", "[unclosed"), "a malformed glob matches nothing")
}

func TestGitIgnoreMatcher_ParsePattern(t *testing.T) {
	matcher := &gitIgnoreMatcher{}

	tests := []struct {
		name            string
		input           string
		expectedGlob    string
		expectedNegate  bool
		expectedDirOnly bool
	}{
		{
			name:            "simple pattern",
			input:           "*.txt",
			expectedGlob:    "*.txt",
			expectedNegate:  false,
			expectedDirOnly: false,
		},
		{
			name:            "directory pattern",
			input:           "node_modules/",
			expectedGlob:    "node_modules",
			expectedNegate:  false,
			expectedDirOnly: true,
		},
		{
			name:            "negated pattern",
			input:           "!important.txt",
			expectedGlob:    "important.txt",
			expectedNegate:  true,
			expectedDirOnly: false,
		},
		{
			name:            "negated directory",
			input:           "!keep/",
			expectedGlob:    "keep",
			expectedNegate:  true,
			expectedDirOnly: true,
		},
		{
			name:            "leading slash",
			input:           "/root.txt",
			expectedGlob:    "root.txt",
			expectedNegate:  false,
			expectedDirOnly: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := matcher.parsePattern(tt.input)

			assert.Equal(t, tt.input, p.raw)
			assert.Equal(t, tt.expectedGlob, p.glob)
			assert.Equal(t, tt.expectedNegate, p.negated)
			assert.Equal(t, tt.expectedDirOnly, p.dirOnly)
		})
	}
}

func TestGitIgnoreMatcher_Match(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		path     string
		expected bool
	}{
		{
			name:     "wildcard match",
			patterns: []string{"*.txt"},
			path:     "/path/to/file.txt",
			expected: true,
		},
		{
			name:     "wildcard no match",
			patterns: []string{"*.txt"},
			path:     "/path/to/file.go",
			expected: false,
		},
		{
			name:     "directory match",
			patterns: []string{"node_modules/"},
			path:     "/project/node_modules/package",
			expected: true,
		},
		{
			name:     "directory exact match",
			patterns: []string{"temp/"},
			path:     "temp",
			expected: true,
		},
		{
			name:     "negation",
			patterns: []string{"*.txt", "!important.txt"},
			path:     "important.txt",
			expected: false,
		},
		{
			name:     "negation no match",
			patterns: []string{"*.txt", "!important.txt"},
			path:     "other.txt",
			expected: true,
		},
		{
			name:     "multiple patterns",
			patterns: []string{"*.log", "*.tmp", "temp/"},
			path:     "/var/log/app.log",
			expected: true,
		},
		{
			name:     "no match",
			patterns: []string{"*.log"},
			path:     "/path/to/file.txt",
			expected: false,
		},
		{
			name:     "git directory",
			patterns: []string{".git/"},
			path:     "/project/.git/config",
			expected: true,
		},
		{
			name:     "hidden file",
			patterns: []string{".DS_Store"},
			path:     "/path/.DS_Store",
			expected: true,
		},
		{
			name:     "pyc files",
			patterns: []string{"*.pyc"},
			path:     "/python/module.pyc",
			expected: true,
		},
		{
			name:     "pycache directory",
			patterns: []string{"__pycache__/"},
			path:     "/python/__pycache__/module.pyc",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher, err := newGitIgnoreMatcher(tt.patterns)
			require.NoError(t, err)

			matched := matcher.Match(tt.path)
			assert.Equal(t, tt.expected, matched, "path: %s", tt.path)
		})
	}
}

func TestGitIgnoreMatcher_ComplexPatterns(t *testing.T) {
	patterns := []string{
		".git/",
		".git/*",
		"node_modules/",
		"*.pyc",
		"__pycache__/",
		".DS_Store",
		"*.tmp",
		"!important.tmp",
	}

	matcher, err := newGitIgnoreMatcher(patterns)
	require.NoError(t, err)

	testCases := []struct {
		path     string
		expected bool
	}{
		{"/project/.git/HEAD", true},
		{"/project/node_modules/lib/index.js", true},
		{"/python/module.pyc", true},
		{"/python/__pycache__/compiled.pyc", true},
		{"/.DS_Store", true},
		{"/temp/file.tmp", true},
		{"/temp/important.tmp", false}, // negated
		{"/src/main.go", false},
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			matched := matcher.Match(tc.path)
			assert.Equal(t, tc.expected, matched, "path: %s", tc.path)
		})
	}
}

func TestGitIgnoreMatcher_EmptyInput(t *testing.T) {
	matcher, err := newGitIgnoreMatcher([]string{""})
	require.NoError(t, err)

	matched := matcher.Match("/any/path.txt")
	assert.False(t, matched)
}

func TestMatcher_WildcardPatterns(t *testing.T) {
	matcher := &gitIgnoreMatcher{}

	tests := []struct {
		pattern string
		path    string
		match   bool
	}{
		{"*.go", "test.go", true},
		{"*.go", "test.txt", false},
		{"test.*", "test.go", true},
		{"test.*", "test.txt", true},
		{"src/*/test.go", "src/pkg/test.go", true},
		{"src/*/test.go", "src/a/b/test.go", false},
		{"**/*.go", "a/b/c/test.go", true},
		{"**/*.go", "test.go", false},
		{"", "anything", false},
		{"no-wild", "no-wild", true},
		{"no-wild", "other", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.path, func(t *testing.T) {
			result := matcher.matchWildcard(tt.path, tt.pattern)
			if result != tt.match {
				t.Errorf("matchWildcard(%q, %q) = %v, want %v",
					tt.pattern, tt.path, result, tt.match)
			}
		})
	}
}

func TestMatcher_PatternEdgeCases(t *testing.T) {
	matcher, err := newGitIgnoreMatcher([]string{
		"dir/",
		"!keep.txt",
		"**/*.tmp",
	})
	require.NoError(t, err)

	tests := []struct {
		path  string
		match bool
	}{
		{"dir/file.txt", true},
		{"dir", true},
		{"keep.txt", false},
		{"a/b/test.tmp", true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := matcher.Match(tt.path)
			if result != tt.match {
				t.Errorf("Match(%q) = %v, want %v", tt.path, result, tt.match)
			}
		})
	}
}
