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

package language

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
)

var (
	// ErrEmptyContent is returned when content is empty and cannot be analyzed.
	ErrEmptyContent = errors.New("content is empty")

	// ErrInvalidInput is returned when inputs are invalid.
	ErrInvalidInput = errors.New("invalid input")
)

// Detector provides language detection from file content and filenames.
type Detector interface {
	// Detect analyzes both content and filename to determine the language.
	// Returns the detected language, confidence score (0.0-1.0), and any error.
	Detect(content []byte, filename string) (Language, float64, error)

	// DetectFromFilename determines language based solely on the filename.
	// Returns LanguageUnknown if the language cannot be determined.
	DetectFromFilename(filename string) Language

	// DetectFromContent analyzes only the content to determine the language.
	// Returns the detected language, confidence score (0.0-1.0), and any error.
	DetectFromContent(content []byte) (Language, float64, error)
}

// detector implements the Detector interface.
type detector struct {
	extensions map[string]Language
	shebangs   map[string]Language
	patterns   map[Language][]string
}

// NewDetector creates a new language detector with default configuration.
func NewDetector() Detector {
	return &detector{
		extensions: buildExtensionMap(),
		shebangs:   buildShebangMap(),
		patterns:   buildPatternMap(),
	}
}

// Detect implements the Detector interface.
func (d *detector) Detect(content []byte, filename string) (Language, float64, error) {
	if len(content) == 0 && filename == "" {
		return LanguageUnknown, 0.0, ErrInvalidInput
	}

	// 1. Try extension-based detection (fastest, most reliable)
	if filename != "" {
		if lang := d.DetectFromFilename(filename); lang != LanguageUnknown {
			return lang, 1.0, nil
		}
	}

	// 2. If we have content, try shebang detection
	if len(content) > 0 {
		if lang, ok := d.detectShebang(content); ok {
			return lang, 0.95, nil
		}

		// 3. Fall back to content analysis
		return d.DetectFromContent(content)
	}

	// No content and no recognized extension
	return LanguageUnknown, 0.0, nil
}

// DetectFromFilename implements the Detector interface.
func (d *detector) DetectFromFilename(filename string) Language {
	if filename == "" {
		return LanguageUnknown
	}

	// Extract extension (including the dot)
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		return LanguageUnknown
	}

	// Check for known extensions
	if lang, ok := d.extensions[ext]; ok {
		return lang
	}

	return LanguageUnknown
}

// DetectFromContent implements the Detector interface.
func (d *detector) DetectFromContent(content []byte) (Language, float64, error) {
	if len(content) == 0 {
		return LanguageUnknown, 0.0, ErrEmptyContent
	}

	// Check if content appears to be binary
	if isBinary(content) {
		return LanguageUnknown, 0.0, nil
	}

	// Score each language based on pattern matches
	scores := make(map[Language]int)
	weights := make(map[Language]float64)
	contentStr := string(content)
	contentLower := strings.ToLower(contentStr)

	for lang, patterns := range d.patterns {
		matchCount := 0
		totalWeight := 0.0

		for _, pattern := range patterns {
			if strings.Contains(contentLower, strings.ToLower(pattern)) {
				matchCount++
				// Give more weight to unique/distinctive patterns
				weight := 1.0
				if isDistinctivePattern(lang, pattern) {
					weight = 2.0
				}
				totalWeight += weight
			}
		}

		if matchCount > 0 {
			scores[lang] = matchCount
			weights[lang] = totalWeight
		}
	}

	// Find the language with the highest weighted score
	var bestLang Language
	var bestWeight float64

	for lang, weight := range weights {
		if weight > bestWeight {
			bestWeight = weight
			bestLang = lang
		}
	}

	if bestLang == LanguageUnknown || bestWeight == 0.0 {
		return LanguageUnknown, 0.0, nil
	}

	// Calculate confidence based on match strength
	// More matches and distinctive patterns = higher confidence
	matchRatio := float64(scores[bestLang]) / float64(len(d.patterns[bestLang]))
	confidence := 0.7 + (matchRatio * 0.25)

	// Boost confidence for high weight (distinctive patterns)
	if bestWeight > 5.0 {
		confidence += 0.1
	}

	if confidence > 0.95 {
		confidence = 0.95
	}

	return bestLang, confidence, nil
}

// isDistinctivePattern returns true if the pattern is highly distinctive for the language.
func isDistinctivePattern(lang Language, pattern string) bool {
	distinctive := map[Language][]string{
		LanguageGo:         {"package ", "func ", ":= ", "defer ", "chan "},
		LanguagePython:     {"def ", "elif ", "__init__", "self."},
		LanguageJavaScript: {"const ", "let ", "=>", "async ", "await "},
		LanguageTypeScript: {": string", ": number", ": boolean", "<T>"},
		LanguageJava:       {"public class", "@Override", "import java."},
		LanguageRust:       {"fn ", "let mut", "impl ", "&mut ", "Result<", "Option<"},
		LanguageC:          {"#include", "malloc(", "free(", "NULL"},
		LanguageCPP:        {"#include <iostream>", "std::", "template<", "nullptr"},
		LanguageRuby:       {"attr_accessor", "do |"},
		LanguagePHP:        {"<?php", "$_GET", "$_POST"},
		LanguageBash:       {"#!/bin/bash"},
		LanguageShell:      {"#!/bin/sh"},
	}

	patterns, ok := distinctive[lang]
	if !ok {
		return false
	}

	for _, dp := range patterns {
		if strings.EqualFold(dp, pattern) {
			return true
		}
	}
	return false
}

// detectShebang checks for shebang patterns at the start of content.
func (d *detector) detectShebang(content []byte) (Language, bool) {
	if len(content) < 2 || content[0] != '#' || content[1] != '!' {
		return LanguageUnknown, false
	}

	// Extract the first line
	firstLine := content
	if idx := bytes.IndexByte(content, '\n'); idx >= 0 {
		firstLine = content[:idx]
	}

	shebangLine := strings.ToLower(string(firstLine))

	// Check for bash specifically (more specific check first)
	if strings.Contains(shebangLine, "/bash") || strings.Contains(shebangLine, "bash") {
		return LanguageBash, true
	}

	// Check against other known shebangs
	for pattern, lang := range d.shebangs {
		if strings.Contains(shebangLine, pattern) {
			return lang, true
		}
	}

	return LanguageUnknown, false
}

// isBinary performs a simple heuristic check for binary content.
// Returns true if the content appears to be binary data.
func isBinary(content []byte) bool {
	if len(content) == 0 {
		return false
	}

	// Check first 512 bytes (or less if content is shorter)
	checkLen := 512
	if len(content) < checkLen {
		checkLen = len(content)
	}

	// Count null bytes and non-printable characters
	nullCount := 0
	for i := 0; i < checkLen; i++ {
		if content[i] == 0 {
			nullCount++
		}
	}

	// If more than 10% null bytes, likely binary
	return float64(nullCount)/float64(checkLen) > 0.1
}

// buildExtensionMap creates the extension to Language mapping.
func buildExtensionMap() map[string]Language {
	return map[string]Language{
		// Go
		".go": LanguageGo,

		// Python
		".py":  LanguagePython,
		".pyw": LanguagePython,
		".pyi": LanguagePython,
		".py3": LanguagePython,
		".pyx": LanguagePython,
		".pxd": LanguagePython,
		".pxi": LanguagePython,

		// JavaScript
		".js":  LanguageJavaScript,
		".jsx": LanguageJavaScript,
		".mjs": LanguageJavaScript,
		".cjs": LanguageJavaScript,

		// TypeScript
		".ts":  LanguageTypeScript,
		".tsx": LanguageTypeScript,

		// Java
		".java": LanguageJava,

		// Rust
		".rs": LanguageRust,

		// C
		".c": LanguageC,
		".h": LanguageC,

		// C++
		".cpp": LanguageCPP,
		".cc":  LanguageCPP,
		".cxx": LanguageCPP,
		".c++": LanguageCPP,
		".hpp": LanguageCPP,
		".hh":  LanguageCPP,
		".hxx": LanguageCPP,
		".h++": LanguageCPP,

		// Ruby
		".rb":   LanguageRuby,
		".rbw":  LanguageRuby,
		".rake": LanguageRuby,

		// PHP
		".php":   LanguagePHP,
		".php3":  LanguagePHP,
		".php4":  LanguagePHP,
		".php5":  LanguagePHP,
		".phtml": LanguagePHP,

		// Markdown
		".md":       LanguageMarkdown,
		".markdown": LanguageMarkdown,
		".mdown":    LanguageMarkdown,
		".mkd":      LanguageMarkdown,

		// HTML
		".html": LanguageHTML,
		".htm":  LanguageHTML,

		// JSON
		".json": LanguageJSON,

		// YAML
		".yaml": LanguageYAML,
		".yml":  LanguageYAML,

		// TOML
		".toml": LanguageTOML,

		// XML
		".xml": LanguageXML,

		// SQL
		".sql": LanguageSQL,

		// CSV
		".csv": LanguageCSV,

		// Bash/Shell
		".sh":   LanguageShell,
		".bash": LanguageBash,
		".zsh":  LanguageShell,
		".ksh":  LanguageShell,
	}
}

// buildShebangMap creates the shebang pattern to Language mapping.
func buildShebangMap() map[string]Language {
	return map[string]Language{
		"python":  LanguagePython,
		"python2": LanguagePython,
		"python3": LanguagePython,
		"sh":      LanguageShell,
		"/bin/sh": LanguageShell,
		"zsh":     LanguageShell,
		"ksh":     LanguageShell,
		"ruby":    LanguageRuby,
		"php":     LanguagePHP,
		"node":    LanguageJavaScript,
		"nodejs":  LanguageJavaScript,
	}
}

// buildPatternMap creates language-specific keyword patterns for content analysis.
func buildPatternMap() map[Language][]string {
	return map[Language][]string{
		LanguageGo: {
			"package ",
			"func ",
			"import \"",
			"type ",
			"struct {",
			"interface {",
			"defer ",
			"go ",
			"chan ",
			":= ",
		},
		LanguagePython: {
			"def ",
			"class ",
			"import ",
			"from ",
			"__init__",
			"self.",
			"elif ",
			"print(",
		},
		LanguageJavaScript: {
			"function",
			"const ",
			"let ",
			"var ",
			"=>",
			"console.log",
			"require(",
			"module.exports",
			"async ",
			"await ",
		},
		LanguageTypeScript: {
			"interface ",
			"type ",
			": string",
			": number",
			": boolean",
			"<T>",
			"implements ",
			"enum ",
		},
		LanguageJava: {
			"public class",
			"private ",
			"protected ",
			"import java.",
			"static void",
			"new ",
			"extends ",
			"implements ",
			"@Override",
		},
		LanguageRust: {
			"fn ",
			"let mut",
			"impl ",
			"trait ",
			"use ",
			"pub ",
			"&mut ",
			"Result<",
			"Option<",
			"match ",
		},
		LanguageC: {
			"#include",
			"int main",
			"void ",
			"printf(",
			"scanf(",
			"malloc(",
			"free(",
			"NULL",
		},
		LanguageCPP: {
			"#include <iostream>",
			"std::",
			"namespace ",
			"class ",
			"template<",
			"cout <<",
			"cin >>",
			"virtual ",
			"nullptr",
		},
		LanguageRuby: {
			"def ",
			"class ",
			"module ",
			"require ",
			"attr_accessor",
			"puts ",
			"do |",
			"end",
		},
		LanguagePHP: {
			"<?php",
			"$_GET",
			"$_POST",
			"function ",
			"echo ",
			"->",
			"namespace ",
		},
		LanguageMarkdown: {
			"# ",
			"## ",
			"### ",
			"```",
			"[",
			"](",
			"**",
			"__",
		},
		LanguageHTML: {
			"<!DOCTYPE",
			"<html",
			"<head>",
			"<body>",
			"<div",
			"<p>",
			"</",
		},
		LanguageJSON: {
			"\": ",
			"null",
			"true",
			"false",
		},
		LanguageYAML: {
			"---",
			"...",
			": ",
			"- ",
		},
		LanguageTOML: {
			" = ",
			"\"\"\"",
			"[",
		},
		LanguageXML: {
			"<?xml",
			"xmlns",
			"/>",
		},
		LanguageSQL: {
			"SELECT ",
			"FROM ",
			"WHERE ",
			"INSERT INTO",
			"UPDATE ",
			"DELETE FROM",
			"CREATE TABLE",
			"ALTER TABLE",
			"JOIN ",
		},
		LanguageCSV: {
			",",
		},
		LanguageBash: {
			"#!/bin/bash",
			"echo ",
			"if [",
			"then",
			"fi",
			"for ",
			"do",
			"done",
		},
		LanguageShell: {
			"#!/bin/sh",
			"echo ",
			"if [",
			"then",
			"fi",
		},
	}
}
