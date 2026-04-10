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
	"testing"
)

func TestLanguageString(t *testing.T) {
	tests := []struct {
		lang Language
		want string
	}{
		{LanguageUnknown, "unknown"},
		{LanguageGo, "go"},
		{LanguagePython, "python"},
		{LanguageJavaScript, "javascript"},
		{LanguageTypeScript, "typescript"},
		{LanguageJava, "java"},
		{LanguageRust, "rust"},
		{LanguageC, "c"},
		{LanguageCPP, "cpp"},
		{LanguageRuby, "ruby"},
		{LanguagePHP, "php"},
		{LanguageMarkdown, "markdown"},
		{LanguageHTML, "html"},
		{LanguageJSON, "json"},
		{LanguageYAML, "yaml"},
		{LanguageTOML, "toml"},
		{LanguageXML, "xml"},
		{LanguageSQL, "sql"},
		{LanguageCSV, "csv"},
		{LanguageBash, "bash"},
		{LanguageShell, "shell"},
		{Language(999), "unknown"}, // Invalid language
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.lang.String()
			if got != tt.want {
				t.Errorf("Language.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLanguageExtension(t *testing.T) {
	tests := []struct {
		lang Language
		want string
	}{
		{LanguageGo, ".go"},
		{LanguagePython, ".py"},
		{LanguageJavaScript, ".js"},
		{LanguageTypeScript, ".ts"},
		{LanguageMarkdown, ".md"},
		{LanguageUnknown, ""},
		{Language(999), ""}, // Invalid language
	}

	for _, tt := range tests {
		t.Run(tt.lang.String(), func(t *testing.T) {
			got := tt.lang.Extension()
			if got != tt.want {
				t.Errorf("Language.Extension() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLanguageIsValid(t *testing.T) {
	tests := []struct {
		lang Language
		want bool
	}{
		{LanguageGo, true},
		{LanguagePython, true},
		{LanguageUnknown, false},
		{Language(999), false},
	}

	for _, tt := range tests {
		t.Run(tt.lang.String(), func(t *testing.T) {
			got := tt.lang.IsValid()
			if got != tt.want {
				t.Errorf("Language.IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLanguageIsProgramming(t *testing.T) {
	tests := []struct {
		lang Language
		want bool
	}{
		{LanguageGo, true},
		{LanguagePython, true},
		{LanguageJava, true},
		{LanguageMarkdown, false},
		{LanguageJSON, false},
		{LanguageUnknown, false},
	}

	for _, tt := range tests {
		t.Run(tt.lang.String(), func(t *testing.T) {
			got := tt.lang.IsProgramming()
			if got != tt.want {
				t.Errorf("Language.IsProgramming() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLanguageIsMarkup(t *testing.T) {
	tests := []struct {
		lang Language
		want bool
	}{
		{LanguageMarkdown, true},
		{LanguageHTML, true},
		{LanguageJSON, true},
		{LanguageYAML, true},
		{LanguageGo, false},
		{LanguagePython, false},
	}

	for _, tt := range tests {
		t.Run(tt.lang.String(), func(t *testing.T) {
			got := tt.lang.IsMarkup()
			if got != tt.want {
				t.Errorf("Language.IsMarkup() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLanguageIsShell(t *testing.T) {
	tests := []struct {
		lang Language
		want bool
	}{
		{LanguageBash, true},
		{LanguageShell, true},
		{LanguageGo, false},
		{LanguagePython, false},
	}

	for _, tt := range tests {
		t.Run(tt.lang.String(), func(t *testing.T) {
			got := tt.lang.IsShell()
			if got != tt.want {
				t.Errorf("Language.IsShell() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseLanguage(t *testing.T) {
	tests := []struct {
		input string
		want  Language
	}{
		{"go", LanguageGo},
		{"Go", LanguageGo},
		{"GO", LanguageGo},
		{" python ", LanguagePython},
		{"javascript", LanguageJavaScript},
		{"typescript", LanguageTypeScript},
		{"markdown", LanguageMarkdown},
		{"unknown", LanguageUnknown},
		{"invalid", LanguageUnknown},
		{"", LanguageUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseLanguage(tt.input)
			if got != tt.want {
				t.Errorf("ParseLanguage(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestDetectFromFilename(t *testing.T) {
	detector := NewDetector()

	tests := []struct {
		filename string
		want     Language
	}{
		// Go
		{"main.go", LanguageGo},
		{"package.GO", LanguageGo}, // Case insensitive

		// Python
		{"script.py", LanguagePython},
		{"app.pyw", LanguagePython},
		{"types.pyi", LanguagePython},

		// JavaScript
		{"index.js", LanguageJavaScript},
		{"component.jsx", LanguageJavaScript},
		{"module.mjs", LanguageJavaScript},
		{"lib.cjs", LanguageJavaScript},

		// TypeScript
		{"app.ts", LanguageTypeScript},
		{"component.tsx", LanguageTypeScript},

		// Java
		{"Main.java", LanguageJava},

		// Rust
		{"main.rs", LanguageRust},

		// C/C++
		{"program.c", LanguageC},
		{"header.h", LanguageC},
		{"app.cpp", LanguageCPP},
		{"lib.cc", LanguageCPP},
		{"header.hpp", LanguageCPP},

		// Ruby
		{"script.rb", LanguageRuby},
		{"Rakefile.rake", LanguageRuby},

		// PHP
		{"index.php", LanguagePHP},
		{"legacy.php3", LanguagePHP},

		// Markdown
		{"README.md", LanguageMarkdown},
		{"CHANGELOG.markdown", LanguageMarkdown},

		// HTML
		{"index.html", LanguageHTML},
		{"page.htm", LanguageHTML},

		// JSON
		{"config.json", LanguageJSON},

		// YAML
		{"config.yaml", LanguageYAML},
		{"deploy.yml", LanguageYAML},

		// TOML
		{"config.toml", LanguageTOML},

		// XML
		{"data.xml", LanguageXML},

		// SQL
		{"schema.sql", LanguageSQL},

		// CSV
		{"data.csv", LanguageCSV},

		// Shell
		{"script.sh", LanguageShell},
		{"install.bash", LanguageBash},
		{"setup.zsh", LanguageShell},

		// Unknown cases
		{"", LanguageUnknown},
		{"noextension", LanguageUnknown},
		{"file.unknown", LanguageUnknown},
		{"file.txt", LanguageUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got := detector.DetectFromFilename(tt.filename)
			if got != tt.want {
				t.Errorf("DetectFromFilename(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}

func TestDetectFromContent_Go(t *testing.T) {
	detector := NewDetector()
	content := []byte(`package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}`)

	lang, confidence, err := detector.DetectFromContent(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lang != LanguageGo {
		t.Errorf("got language %v, want %v", lang, LanguageGo)
	}
	if confidence < 0.7 {
		t.Errorf("confidence %v too low, want >= 0.7", confidence)
	}
}

func TestDetectFromContent_Python(t *testing.T) {
	detector := NewDetector()
	content := []byte(`def hello():
    print("Hello, World!")

class MyClass:
    def __init__(self):
        pass`)

	lang, confidence, err := detector.DetectFromContent(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lang != LanguagePython {
		t.Errorf("got language %v, want %v", lang, LanguagePython)
	}
	if confidence < 0.7 {
		t.Errorf("confidence %v too low, want >= 0.7", confidence)
	}
}

func TestDetectFromContent_JavaScript(t *testing.T) {
	detector := NewDetector()
	content := []byte(`const express = require('express');
const app = express();

function handler() {
    console.log("Hello");
}

async function fetchData() {
    await fetch('/api');
}`)

	lang, confidence, err := detector.DetectFromContent(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lang != LanguageJavaScript {
		t.Errorf("got language %v, want %v", lang, LanguageJavaScript)
	}
	if confidence < 0.7 {
		t.Errorf("confidence %v too low, want >= 0.7", confidence)
	}
}

func TestDetectFromContent_Rust(t *testing.T) {
	detector := NewDetector()
	content := []byte(`fn main() {
    let mut x = 5;
    println!("Value: {}", x);
}

impl MyTrait for MyStruct {
    fn method(&mut self) -> Result<(), Error> {
        match self.value {
            Some(v) => Ok(()),
            None => Err(Error),
        }
    }
}`)

	lang, confidence, err := detector.DetectFromContent(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lang != LanguageRust {
		t.Errorf("got language %v, want %v", lang, LanguageRust)
	}
	if confidence < 0.7 {
		t.Errorf("confidence %v too low, want >= 0.7", confidence)
	}
}

func TestDetectFromContent_SQL(t *testing.T) {
	detector := NewDetector()
	content := []byte(`SELECT users.name, orders.total
FROM users
JOIN orders ON users.id = orders.user_id
WHERE orders.status = 'completed'
ORDER BY orders.created_at DESC;

INSERT INTO users (name, email) VALUES ('John', 'john@example.com');
UPDATE users SET status = 'active' WHERE id = 1;`)

	lang, confidence, err := detector.DetectFromContent(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lang != LanguageSQL {
		t.Errorf("got language %v, want %v", lang, LanguageSQL)
	}
	if confidence < 0.7 {
		t.Errorf("confidence %v too low, want >= 0.7", confidence)
	}
}

func TestDetectFromContent_EmptyContent(t *testing.T) {
	detector := NewDetector()
	content := []byte{}

	lang, confidence, err := detector.DetectFromContent(content)
	if err != ErrEmptyContent {
		t.Errorf("expected ErrEmptyContent, got %v", err)
	}
	if lang != LanguageUnknown {
		t.Errorf("got language %v, want LanguageUnknown", lang)
	}
	if confidence != 0.0 {
		t.Errorf("got confidence %v, want 0.0", confidence)
	}
}

func TestDetectFromContent_UnknownLanguage(t *testing.T) {
	detector := NewDetector()
	content := []byte("This is just some plain text without any code patterns.")

	lang, confidence, err := detector.DetectFromContent(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lang != LanguageUnknown {
		t.Errorf("got language %v, want LanguageUnknown", lang)
	}
	if confidence != 0.0 {
		t.Errorf("got confidence %v, want 0.0", confidence)
	}
}

func TestDetectFromContent_BinaryData(t *testing.T) {
	detector := NewDetector()
	// Create binary content with null bytes
	content := make([]byte, 100)
	for i := 0; i < 50; i++ {
		content[i] = 0 // Null bytes
	}

	lang, confidence, err := detector.DetectFromContent(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lang != LanguageUnknown {
		t.Errorf("got language %v, want LanguageUnknown", lang)
	}
	if confidence != 0.0 {
		t.Errorf("got confidence %v, want 0.0", confidence)
	}
}

func TestDetectShebang(t *testing.T) {
	detector := NewDetector().(*detector)

	tests := []struct {
		name    string
		content []byte
		want    Language
		wantOk  bool
	}{
		{
			name:    "Python shebang",
			content: []byte("#!/usr/bin/env python3\nprint('hello')"),
			want:    LanguagePython,
			wantOk:  true,
		},
		{
			name:    "Python2 shebang",
			content: []byte("#!/usr/bin/python2\nprint 'hello'"),
			want:    LanguagePython,
			wantOk:  true,
		},
		{
			name:    "Bash shebang",
			content: []byte("#!/bin/bash\necho hello"),
			want:    LanguageBash,
			wantOk:  true,
		},
		{
			name:    "Shell shebang",
			content: []byte("#!/bin/sh\necho hello"),
			want:    LanguageShell,
			wantOk:  true,
		},
		{
			name:    "Node.js shebang",
			content: []byte("#!/usr/bin/env node\nconsole.log('hello')"),
			want:    LanguageJavaScript,
			wantOk:  true,
		},
		{
			name:    "Ruby shebang",
			content: []byte("#!/usr/bin/env ruby\nputs 'hello'"),
			want:    LanguageRuby,
			wantOk:  true,
		},
		{
			name:    "No shebang",
			content: []byte("package main\nfunc main() {}"),
			want:    LanguageUnknown,
			wantOk:  false,
		},
		{
			name:    "Empty content",
			content: []byte{},
			want:    LanguageUnknown,
			wantOk:  false,
		},
		{
			name:    "Comment not shebang",
			content: []byte("# This is a comment\nprint('hello')"),
			want:    LanguageUnknown,
			wantOk:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotOk := detector.detectShebang(tt.content)
			if got != tt.want {
				t.Errorf("detectShebang() language = %v, want %v", got, tt.want)
			}
			if gotOk != tt.wantOk {
				t.Errorf("detectShebang() ok = %v, want %v", gotOk, tt.wantOk)
			}
		})
	}
}

func TestDetect_CombinedStrategy(t *testing.T) {
	detector := NewDetector()

	tests := []struct {
		name              string
		content           []byte
		filename          string
		wantLang          Language
		wantMinConfidence float64
		wantMaxConfidence float64
	}{
		{
			name:              "Extension match - highest confidence",
			content:           []byte("some content"),
			filename:          "main.go",
			wantLang:          LanguageGo,
			wantMinConfidence: 1.0,
			wantMaxConfidence: 1.0,
		},
		{
			name:              "Shebang match - high confidence",
			content:           []byte("#!/usr/bin/env python3\nprint('hello')"),
			filename:          "script",
			wantLang:          LanguagePython,
			wantMinConfidence: 0.95,
			wantMaxConfidence: 0.95,
		},
		{
			name: "Content analysis - medium confidence",
			content: []byte(`package main
func main() {
	println("hello")
}`),
			filename:          "file",
			wantLang:          LanguageGo,
			wantMinConfidence: 0.7,
			wantMaxConfidence: 0.95,
		},
		{
			name:              "Extension overrides content",
			content:           []byte("print('python code')"),
			filename:          "script.js",
			wantLang:          LanguageJavaScript,
			wantMinConfidence: 1.0,
			wantMaxConfidence: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lang, confidence, err := detector.Detect(tt.content, tt.filename)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if lang != tt.wantLang {
				t.Errorf("Detect() language = %v, want %v", lang, tt.wantLang)
			}
			if confidence < tt.wantMinConfidence || confidence > tt.wantMaxConfidence {
				t.Errorf("Detect() confidence = %v, want between %v and %v",
					confidence, tt.wantMinConfidence, tt.wantMaxConfidence)
			}
		})
	}
}

func TestDetect_InvalidInput(t *testing.T) {
	detector := NewDetector()

	lang, confidence, err := detector.Detect([]byte{}, "")
	if err != ErrInvalidInput {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
	if lang != LanguageUnknown {
		t.Errorf("got language %v, want LanguageUnknown", lang)
	}
	if confidence != 0.0 {
		t.Errorf("got confidence %v, want 0.0", confidence)
	}
}

func TestDetect_OnlyFilename(t *testing.T) {
	detector := NewDetector()

	lang, confidence, err := detector.Detect(nil, "script.py")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lang != LanguagePython {
		t.Errorf("got language %v, want LanguagePython", lang)
	}
	if confidence != 1.0 {
		t.Errorf("got confidence %v, want 1.0", confidence)
	}
}

func TestDetect_OnlyContent(t *testing.T) {
	detector := NewDetector()
	content := []byte(`package main
func main() {}`)

	lang, confidence, err := detector.Detect(content, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lang != LanguageGo {
		t.Errorf("got language %v, want LanguageGo", lang)
	}
	if confidence < 0.7 {
		t.Errorf("confidence %v too low, want >= 0.7", confidence)
	}
}

func TestIsBinary(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
		want    bool
	}{
		{
			name:    "Empty content",
			content: []byte{},
			want:    false,
		},
		{
			name:    "Plain text",
			content: []byte("Hello, World!"),
			want:    false,
		},
		{
			name:    "Code content",
			content: []byte("package main\nfunc main() {}"),
			want:    false,
		},
		{
			name: "Binary content with null bytes",
			content: func() []byte {
				data := make([]byte, 100)
				for i := 0; i < 50; i++ {
					data[i] = 0
				}
				return data
			}(),
			want: true,
		},
		{
			name: "Mostly text with few null bytes",
			content: func() []byte {
				data := []byte("This is mostly text content")
				data[5] = 0
				return data
			}(),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isBinary(tt.content)
			if got != tt.want {
				t.Errorf("isBinary() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDetectFromContent_AllLanguages(t *testing.T) {
	detector := NewDetector()

	tests := []struct {
		lang    Language
		content string
	}{
		{LanguageGo, "package main\nimport \"fmt\"\nfunc main() { fmt.Println() }"},
		{LanguagePython, "def main():\n    import sys\n    class MyClass:\n        pass"},
		{LanguageJavaScript, "const x = 5;\nfunction test() {}\nconsole.log(x);"},
		{LanguageTypeScript, "interface User { name: string; age: number; }\ntype ID = string;"},
		{LanguageJava, "public class Main {\n    private static void main(String[] args) {}\n}"},
		{LanguageRust, "fn main() {\n    let mut x = 5;\n    impl Trait for Struct {}\n}"},
		{LanguageC, "#include <stdio.h>\nint main() {\n    printf(\"hello\");\n    return 0;\n}"},
		{LanguageCPP, "#include <iostream>\nusing namespace std;\nclass Test {};\nint main() {}"},
		{LanguageRuby, "def hello\n  puts 'hi'\nend\nclass MyClass\n  attr_accessor :name\nend"},
		{LanguagePHP, "<?php\nfunction test() {\n    echo $_GET['id'];\n}\n?>"},
		{LanguageMarkdown, "# Heading\n## Subheading\n```code```\n[link](url)\n**bold**"},
		{LanguageHTML, "<!DOCTYPE html>\n<html>\n<head></head>\n<body><div></div></body>\n</html>"},
		{LanguageJSON, `{"key": "value", "array": [1, 2, 3], "bool": true, "null": null}`},
		{LanguageYAML, "---\nkey: value\nlist:\n  - item1\n  - item2\n..."},
		{LanguageSQL, "SELECT * FROM users WHERE id = 1;\nINSERT INTO users VALUES ('test');\nUPDATE users SET status = 'active';"},
		{LanguageBash, "#!/bin/bash\necho 'test'\nif [ -f file ]; then\n  echo 'exists'\nfi"},
	}

	for _, tt := range tests {
		t.Run(tt.lang.String(), func(t *testing.T) {
			lang, confidence, err := detector.DetectFromContent([]byte(tt.content))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if lang != tt.lang {
				t.Errorf("DetectFromContent() = %v, want %v", lang, tt.lang)
			}
			if confidence < 0.7 {
				t.Errorf("confidence %v too low for %v", confidence, tt.lang)
			}
		})
	}
}

func BenchmarkDetectFromFilename(b *testing.B) {
	detector := NewDetector()
	filenames := []string{
		"main.go",
		"script.py",
		"index.js",
		"app.ts",
		"README.md",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, filename := range filenames {
			detector.DetectFromFilename(filename)
		}
	}
}

func BenchmarkDetectFromContent(b *testing.B) {
	detector := NewDetector()
	content := []byte(`package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		detector.DetectFromContent(content)
	}
}

func BenchmarkDetect(b *testing.B) {
	detector := NewDetector()
	content := []byte(`package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}`)
	filename := "main.go"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		detector.Detect(content, filename)
	}
}

func TestDetect_NoContentWithUnknownExtension(t *testing.T) {
	detector := NewDetector()

	lang, confidence, err := detector.Detect(nil, "file.unknown")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lang != LanguageUnknown {
		t.Errorf("got language %v, want LanguageUnknown", lang)
	}
	if confidence != 0.0 {
		t.Errorf("got confidence %v, want 0.0", confidence)
	}
}

// TestDetect_EmptyContentWithUnknownExtension tests the path where:
// - filename is provided but has unknown extension
// - empty content is provided
// This covers the path to line 73 (no content and no recognized extension)
func TestDetect_EmptyContentWithUnknownExtension(t *testing.T) {
	detector := NewDetector()

	lang, confidence, err := detector.Detect([]byte{}, "file.txt")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if lang != LanguageUnknown {
		t.Errorf("got language %v, want LanguageUnknown", lang)
	}
	if confidence != 0.0 {
		t.Errorf("got confidence %v, want 0.0", confidence)
	}
}

// TestDetectShebang_ShortContent tests shebang detection with content < 2 bytes
// This covers line 199 in detectShebang function
func TestDetectShebang_ShortContent(t *testing.T) {
	detector := NewDetector().(*detector)

	tests := []struct {
		name    string
		content []byte
		want    Language
		wantOk  bool
	}{
		{
			name:    "Single byte",
			content: []byte("#"),
			want:    LanguageUnknown,
			wantOk:  false,
		},
		{
			name:    "Single character non-shebang",
			content: []byte("x"),
			want:    LanguageUnknown,
			wantOk:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotOk := detector.detectShebang(tt.content)
			if got != tt.want {
				t.Errorf("detectShebang() language = %v, want %v", got, tt.want)
			}
			if gotOk != tt.wantOk {
				t.Errorf("detectShebang() ok = %v, want %v", gotOk, tt.wantOk)
			}
		})
	}
}

// TestDetectShebang_BashVariations tests various bash shebang formats
// This ensures the bash-specific check on line 212 is fully covered
func TestDetectShebang_BashVariations(t *testing.T) {
	detector := NewDetector().(*detector)

	tests := []struct {
		name    string
		content []byte
		want    Language
		wantOk  bool
	}{
		{
			name:    "Bash with env",
			content: []byte("#!/usr/bin/env bash\necho test"),
			want:    LanguageBash,
			wantOk:  true,
		},
		{
			name:    "Bash direct path",
			content: []byte("#!/usr/local/bin/bash\necho test"),
			want:    LanguageBash,
			wantOk:  true,
		},
		{
			name:    "Bash uppercase",
			content: []byte("#!/bin/BASH\necho test"),
			want:    LanguageBash,
			wantOk:  true,
		},
		{
			name:    "Just bash",
			content: []byte("#!bash\necho test"),
			want:    LanguageBash,
			wantOk:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotOk := detector.detectShebang(tt.content)
			if got != tt.want {
				t.Errorf("detectShebang() language = %v, want %v", got, tt.want)
			}
			if gotOk != tt.wantOk {
				t.Errorf("detectShebang() ok = %v, want %v", gotOk, tt.wantOk)
			}
		})
	}
}

// TestDetectShebang_NoNewline tests shebang without newline
// This covers the case where idx == -1 on line 205
func TestDetectShebang_NoNewline(t *testing.T) {
	detector := NewDetector().(*detector)

	tests := []struct {
		name    string
		content []byte
		want    Language
		wantOk  bool
	}{
		{
			name:    "Python shebang no newline",
			content: []byte("#!/usr/bin/python3"),
			want:    LanguagePython,
			wantOk:  true,
		},
		{
			name:    "Bash shebang no newline",
			content: []byte("#!/bin/bash"),
			want:    LanguageBash,
			wantOk:  true,
		},
		{
			name:    "Shell shebang no newline",
			content: []byte("#!/bin/sh"),
			want:    LanguageShell,
			wantOk:  true,
		},
		{
			name:    "Node shebang no newline",
			content: []byte("#!/usr/bin/env node"),
			want:    LanguageJavaScript,
			wantOk:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotOk := detector.detectShebang(tt.content)
			if got != tt.want {
				t.Errorf("detectShebang() language = %v, want %v", got, tt.want)
			}
			if gotOk != tt.wantOk {
				t.Errorf("detectShebang() ok = %v, want %v", gotOk, tt.wantOk)
			}
		})
	}
}

// TestDetectShebang_NonBashFromMap tests shebangs that match the map but not bash
// This covers line 217-220 where other shebangs are checked
func TestDetectShebang_NonBashFromMap(t *testing.T) {
	detector := NewDetector().(*detector)

	tests := []struct {
		name    string
		content []byte
		want    Language
		wantOk  bool
	}{
		{
			name:    "Zsh shebang",
			content: []byte("#!/bin/zsh\necho test"),
			want:    LanguageShell,
			wantOk:  true,
		},
		{
			name:    "Ksh shebang",
			content: []byte("#!/bin/ksh\necho test"),
			want:    LanguageShell,
			wantOk:  true,
		},
		{
			name:    "Ruby env shebang",
			content: []byte("#!/usr/bin/env ruby\nputs 'hi'"),
			want:    LanguageRuby,
			wantOk:  true,
		},
		{
			name:    "PHP shebang",
			content: []byte("#!/usr/bin/php\n<?php echo 'test'; ?>"),
			want:    LanguagePHP,
			wantOk:  true,
		},
		{
			name:    "Node.js shebang",
			content: []byte("#!/usr/bin/nodejs\nconsole.log('test')"),
			want:    LanguageJavaScript,
			wantOk:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotOk := detector.detectShebang(tt.content)
			if got != tt.want {
				t.Errorf("detectShebang() language = %v, want %v", got, tt.want)
			}
			if gotOk != tt.wantOk {
				t.Errorf("detectShebang() ok = %v, want %v", gotOk, tt.wantOk)
			}
		})
	}
}

// TestDetectShebang_UnknownShebang tests shebang that doesn't match any known patterns
// This covers line 223 where LanguageUnknown is returned
func TestDetectShebang_UnknownShebang(t *testing.T) {
	detector := NewDetector().(*detector)

	tests := []struct {
		name    string
		content []byte
	}{
		{
			name:    "Unknown interpreter",
			content: []byte("#!/usr/bin/unknownlang\ncode here"),
		},
		{
			name:    "Custom shebang",
			content: []byte("#!/custom/path/interpreter\ncode here"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotOk := detector.detectShebang(tt.content)
			if got != LanguageUnknown {
				t.Errorf("detectShebang() language = %v, want LanguageUnknown", got)
			}
			if gotOk != false {
				t.Errorf("detectShebang() ok = %v, want false", gotOk)
			}
		})
	}
}

// TestDetect_ShebangPathWithContent tests the shebang detection path within Detect
// This ensures lines 64-66 are covered (shebang detection in Detect function)
func TestDetect_ShebangPathWithContent(t *testing.T) {
	detector := NewDetector()

	tests := []struct {
		name              string
		content           []byte
		filename          string
		wantLang          Language
		wantMinConfidence float64
		wantMaxConfidence float64
	}{
		{
			name:              "Python shebang with unknown extension",
			content:           []byte("#!/usr/bin/env python3\ndef main():\n    pass"),
			filename:          "script",
			wantLang:          LanguagePython,
			wantMinConfidence: 0.95,
			wantMaxConfidence: 0.95,
		},
		{
			name:              "Bash shebang with unknown extension",
			content:           []byte("#!/bin/bash\necho hello"),
			filename:          "install",
			wantLang:          LanguageBash,
			wantMinConfidence: 0.95,
			wantMaxConfidence: 0.95,
		},
		{
			name:              "Shell shebang no filename",
			content:           []byte("#!/bin/sh\necho test"),
			filename:          "",
			wantLang:          LanguageShell,
			wantMinConfidence: 0.95,
			wantMaxConfidence: 0.95,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lang, confidence, err := detector.Detect(tt.content, tt.filename)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if lang != tt.wantLang {
				t.Errorf("Detect() language = %v, want %v", lang, tt.wantLang)
			}
			if confidence < tt.wantMinConfidence || confidence > tt.wantMaxConfidence {
				t.Errorf("Detect() confidence = %v, want between %v and %v",
					confidence, tt.wantMinConfidence, tt.wantMaxConfidence)
			}
		})
	}
}

// TestDetect_ContentAnalysisPath tests falling through to content analysis
// This ensures line 69 is covered
func TestDetect_ContentAnalysisPath(t *testing.T) {
	detector := NewDetector()

	tests := []struct {
		name              string
		content           []byte
		filename          string
		wantLang          Language
		wantMinConfidence float64
	}{
		{
			name: "Go code without extension or shebang",
			content: []byte(`package main

import "fmt"

func main() {
	fmt.Println("Hello")
}`),
			filename:          "noext",
			wantLang:          LanguageGo,
			wantMinConfidence: 0.7,
		},
		{
			name: "Python code without extension or shebang",
			content: []byte(`def hello():
    print("Hello, World!")

class MyClass:
    def __init__(self):
        pass`),
			filename:          "script",
			wantLang:          LanguagePython,
			wantMinConfidence: 0.7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lang, confidence, err := detector.Detect(tt.content, tt.filename)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if lang != tt.wantLang {
				t.Errorf("Detect() language = %v, want %v", lang, tt.wantLang)
			}
			if confidence < tt.wantMinConfidence {
				t.Errorf("Detect() confidence = %v, want >= %v", confidence, tt.wantMinConfidence)
			}
		})
	}
}
