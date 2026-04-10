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

package language_test

import (
	"fmt"

	"github.com/jeremyhahn/go-quicbaas/pkg/rag/language"
)

// ExampleDetector_Detect demonstrates detecting language from filename and content.
func ExampleDetector_Detect() {
	detector := language.NewDetector()

	content := []byte(`package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}`)

	lang, confidence, err := detector.Detect(content, "main.go")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Language: %s\n", lang)
	fmt.Printf("Confidence: %.2f\n", confidence)
	// Output:
	// Language: go
	// Confidence: 1.00
}

// ExampleDetector_DetectFromFilename demonstrates detecting language from filename only.
func ExampleDetector_DetectFromFilename() {
	detector := language.NewDetector()

	examples := []string{
		"script.py",
		"index.js",
		"main.go",
		"README.md",
	}

	for _, filename := range examples {
		lang := detector.DetectFromFilename(filename)
		fmt.Printf("%s -> %s\n", filename, lang)
	}
	// Output:
	// script.py -> python
	// index.js -> javascript
	// main.go -> go
	// README.md -> markdown
}

// ExampleDetector_DetectFromContent demonstrates detecting language from content only.
func ExampleDetector_DetectFromContent() {
	detector := language.NewDetector()

	content := []byte(`def hello_world():
    print("Hello, World!")

if __name__ == "__main__":
    hello_world()`)

	lang, confidence, err := detector.DetectFromContent(content)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("Language: %s (confidence: %.2f)\n", lang, confidence)
	// Output:
	// Language: python (confidence: 0.76)
}

// ExampleLanguage_String demonstrates converting a Language to string.
func ExampleLanguage_String() {
	languages := []language.Language{
		language.LanguageGo,
		language.LanguagePython,
		language.LanguageJavaScript,
		language.LanguageUnknown,
	}

	for _, lang := range languages {
		fmt.Println(lang.String())
	}
	// Output:
	// go
	// python
	// javascript
	// unknown
}

// ExampleParseLanguage demonstrates parsing a language string.
func ExampleParseLanguage() {
	examples := []string{"go", "Python", "JAVASCRIPT", "unknown"}

	for _, s := range examples {
		lang := language.ParseLanguage(s)
		fmt.Printf("%s -> %s\n", s, lang)
	}
	// Output:
	// go -> go
	// Python -> python
	// JAVASCRIPT -> javascript
	// unknown -> unknown
}

// ExampleLanguage_IsProgramming demonstrates checking if a language is a programming language.
func ExampleLanguage_IsProgramming() {
	languages := []language.Language{
		language.LanguageGo,
		language.LanguageMarkdown,
		language.LanguageJSON,
	}

	for _, lang := range languages {
		fmt.Printf("%s is programming: %v\n", lang, lang.IsProgramming())
	}
	// Output:
	// go is programming: true
	// markdown is programming: false
	// json is programming: false
}
