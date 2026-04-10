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

// Package language provides language detection capabilities for RAG document processing.
package language

import "strings"

// Language represents a detected programming or markup language.
type Language int

const (
	// LanguageUnknown indicates the language could not be determined.
	LanguageUnknown Language = iota

	// Programming languages (ordered by priority).
	LanguageGo
	LanguagePython
	LanguageJavaScript
	LanguageTypeScript
	LanguageJava
	LanguageRust
	LanguageC
	LanguageCPP
	LanguageRuby
	LanguagePHP

	// Markup and configuration languages.
	LanguageMarkdown
	LanguageHTML
	LanguageJSON
	LanguageYAML
	LanguageTOML
	LanguageXML

	// Data and query languages.
	LanguageSQL
	LanguageCSV

	// Shell scripting.
	LanguageBash
	LanguageShell
)

// languageNames maps Language constants to their string representations.
var languageNames = map[Language]string{
	LanguageUnknown:    "unknown",
	LanguageGo:         "go",
	LanguagePython:     "python",
	LanguageJavaScript: "javascript",
	LanguageTypeScript: "typescript",
	LanguageJava:       "java",
	LanguageRust:       "rust",
	LanguageC:          "c",
	LanguageCPP:        "cpp",
	LanguageRuby:       "ruby",
	LanguagePHP:        "php",
	LanguageMarkdown:   "markdown",
	LanguageHTML:       "html",
	LanguageJSON:       "json",
	LanguageYAML:       "yaml",
	LanguageTOML:       "toml",
	LanguageXML:        "xml",
	LanguageSQL:        "sql",
	LanguageCSV:        "csv",
	LanguageBash:       "bash",
	LanguageShell:      "shell",
}

// languageExtensions maps Language constants to their primary file extensions.
var languageExtensions = map[Language]string{
	LanguageGo:         ".go",
	LanguagePython:     ".py",
	LanguageJavaScript: ".js",
	LanguageTypeScript: ".ts",
	LanguageJava:       ".java",
	LanguageRust:       ".rs",
	LanguageC:          ".c",
	LanguageCPP:        ".cpp",
	LanguageRuby:       ".rb",
	LanguagePHP:        ".php",
	LanguageMarkdown:   ".md",
	LanguageHTML:       ".html",
	LanguageJSON:       ".json",
	LanguageYAML:       ".yaml",
	LanguageTOML:       ".toml",
	LanguageXML:        ".xml",
	LanguageSQL:        ".sql",
	LanguageCSV:        ".csv",
	LanguageBash:       ".bash",
	LanguageShell:      ".sh",
}

// String returns the string representation of the Language.
func (l Language) String() string {
	if name, ok := languageNames[l]; ok {
		return name
	}
	return "unknown"
}

// Extension returns the primary file extension for the Language.
func (l Language) Extension() string {
	if ext, ok := languageExtensions[l]; ok {
		return ext
	}
	return ""
}

// IsValid returns true if the Language is a known language.
func (l Language) IsValid() bool {
	_, ok := languageNames[l]
	return ok && l != LanguageUnknown
}

// IsProgramming returns true if the Language is a programming language.
func (l Language) IsProgramming() bool {
	return l >= LanguageGo && l <= LanguagePHP
}

// IsMarkup returns true if the Language is a markup or configuration language.
func (l Language) IsMarkup() bool {
	return l >= LanguageMarkdown && l <= LanguageXML
}

// IsShell returns true if the Language is a shell scripting language.
func (l Language) IsShell() bool {
	return l == LanguageBash || l == LanguageShell
}

// ParseLanguage converts a string to a Language constant.
func ParseLanguage(s string) Language {
	s = strings.ToLower(strings.TrimSpace(s))
	for lang, name := range languageNames {
		if name == s {
			return lang
		}
	}
	return LanguageUnknown
}
