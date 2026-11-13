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

func TestDetectShebang(t *testing.T) {
	detector := NewDetector().(*detector)

	tests := []struct {
		name    string
		content []byte
		want    Language
		wantOk  bool
	}{
		{"Python env", []byte("#!/usr/bin/env python3\nprint('hello')"), LanguagePython, true},
		{"Python2 direct path", []byte("#!/usr/bin/python2\nprint 'hello'"), LanguagePython, true},
		{"Python no newline", []byte("#!/usr/bin/python3"), LanguagePython, true},
		{"Bash", []byte("#!/bin/bash\necho hello"), LanguageBash, true},
		{"Bash env", []byte("#!/usr/bin/env bash\necho test"), LanguageBash, true},
		{"Bash local path", []byte("#!/usr/local/bin/bash\necho test"), LanguageBash, true},
		{"Bash uppercase", []byte("#!/bin/BASH\necho test"), LanguageBash, true},
		{"Bash without path", []byte("#!bash\necho test"), LanguageBash, true},
		{"Bash no newline", []byte("#!/bin/bash"), LanguageBash, true},
		{"Shell", []byte("#!/bin/sh\necho hello"), LanguageShell, true},
		{"Shell no newline", []byte("#!/bin/sh"), LanguageShell, true},
		{"Zsh", []byte("#!/bin/zsh\necho test"), LanguageShell, true},
		{"Ksh", []byte("#!/bin/ksh\necho test"), LanguageShell, true},
		{"Node env", []byte("#!/usr/bin/env node\nconsole.log('hello')"), LanguageJavaScript, true},
		{"Node no newline", []byte("#!/usr/bin/env node"), LanguageJavaScript, true},
		{"Nodejs", []byte("#!/usr/bin/nodejs\nconsole.log('test')"), LanguageJavaScript, true},
		{"Ruby env", []byte("#!/usr/bin/env ruby\nputs 'hello'"), LanguageRuby, true},
		{"PHP", []byte("#!/usr/bin/php\n<?php echo 'test'; ?>"), LanguagePHP, true},
		{"Unknown interpreter", []byte("#!/usr/bin/unknownlang\ncode here"), LanguageUnknown, false},
		{"Custom interpreter", []byte("#!/custom/path/interpreter\ncode here"), LanguageUnknown, false},
		{"No shebang", []byte("package main\nfunc main() {}"), LanguageUnknown, false},
		{"Comment not shebang", []byte("# This is a comment\nprint('hello')"), LanguageUnknown, false},
		{"Empty content", []byte{}, LanguageUnknown, false},
		{"Single hash", []byte("#"), LanguageUnknown, false},
		{"Single non-hash byte", []byte("x"), LanguageUnknown, false},
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
		wantErr           error
	}{
		{"Extension match - highest confidence", []byte("some content"), "main.go", LanguageGo, 1.0, 1.0, nil},
		{"Extension overrides content", []byte("print('python code')"), "script.js", LanguageJavaScript, 1.0, 1.0, nil},
		{"Extension without content", nil, "script.py", LanguagePython, 1.0, 1.0, nil},
		{"Shebang match - high confidence", []byte("#!/usr/bin/env python3\nprint('hello')"), "script", LanguagePython, 0.95, 0.95, nil},
		{"Bash shebang with unknown extension", []byte("#!/bin/bash\necho hello"), "install", LanguageBash, 0.95, 0.95, nil},
		{"Shell shebang without filename", []byte("#!/bin/sh\necho test"), "", LanguageShell, 0.95, 0.95, nil},
		{"Content analysis - Go", []byte("package main\nfunc main() {\n\tprintln(\"hello\")\n}"), "file", LanguageGo, 0.7, 0.95, nil},
		{"Content analysis - Go without filename", []byte("package main\nfunc main() {}"), "", LanguageGo, 0.7, 0.95, nil},
		{"Content analysis - Python", []byte("def hello():\n    print(\"Hello, World!\")\n\nclass MyClass:\n    def __init__(self):\n        pass"), "script", LanguagePython, 0.7, 0.95, nil},
		{"No content and unknown extension", nil, "file.unknown", LanguageUnknown, 0.0, 0.0, nil},
		{"Empty content and unknown extension", []byte{}, "file.txt", LanguageUnknown, 0.0, 0.0, nil},
		{"No content and no filename", []byte{}, "", LanguageUnknown, 0.0, 0.0, ErrInvalidInput},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lang, confidence, err := detector.Detect(tt.content, tt.filename)
			if err != tt.wantErr {
				t.Fatalf("Detect() error = %v, want %v", err, tt.wantErr)
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

	// Content with null bytes is treated as binary.
	binary := make([]byte, 100)

	tests := []struct {
		name    string
		lang    Language
		content string
		wantErr error
	}{
		{"Go", LanguageGo, "package main\nimport \"fmt\"\nfunc main() { fmt.Println() }", nil},
		{"Python", LanguagePython, "def main():\n    import sys\n    class MyClass:\n        pass", nil},
		{"JavaScript", LanguageJavaScript, "const x = 5;\nfunction test() {}\nconsole.log(x);", nil},
		{"TypeScript", LanguageTypeScript, "interface User { name: string; age: number; }\ntype ID = string;", nil},
		{"Java", LanguageJava, "public class Main {\n    private static void main(String[] args) {}\n}", nil},
		{"Rust", LanguageRust, "fn main() {\n    let mut x = 5;\n    impl Trait for Struct {}\n}", nil},
		{"C", LanguageC, "#include <stdio.h>\nint main() {\n    printf(\"hello\");\n    return 0;\n}", nil},
		{"CPP", LanguageCPP, "#include <iostream>\nusing namespace std;\nclass Test {};\nint main() {}", nil},
		{"Ruby", LanguageRuby, "def hello\n  puts 'hi'\nend\nclass MyClass\n  attr_accessor :name\nend", nil},
		{"PHP", LanguagePHP, "<?php\nfunction test() {\n    echo $_GET['id'];\n}\n?>", nil},
		{"Markdown", LanguageMarkdown, "# Heading\n## Subheading\n```code```\n[link](url)\n**bold**", nil},
		{"HTML", LanguageHTML, "<!DOCTYPE html>\n<html>\n<head></head>\n<body><div></div></body>\n</html>", nil},
		{"JSON", LanguageJSON, `{"key": "value", "array": [1, 2, 3], "bool": true, "null": null}`, nil},
		{"YAML", LanguageYAML, "---\nkey: value\nlist:\n  - item1\n  - item2\n...", nil},
		{"SQL", LanguageSQL, "SELECT * FROM users WHERE id = 1;\nINSERT INTO users VALUES ('test');\nUPDATE users SET status = 'active';", nil},
		{"Bash", LanguageBash, "#!/bin/bash\necho 'test'\nif [ -f file ]; then\n  echo 'exists'\nfi", nil},
		{"Empty", LanguageUnknown, "", ErrEmptyContent},
		{"PlainText", LanguageUnknown, "This is just some plain text without any code patterns.", nil},
		{"Binary", LanguageUnknown, string(binary), nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lang, confidence, err := detector.DetectFromContent([]byte(tt.content))
			if err != tt.wantErr {
				t.Fatalf("DetectFromContent() error = %v, want %v", err, tt.wantErr)
			}
			if lang != tt.lang {
				t.Errorf("DetectFromContent() = %v, want %v", lang, tt.lang)
			}
			if tt.lang == LanguageUnknown {
				if confidence != 0.0 {
					t.Errorf("confidence = %v, want 0.0", confidence)
				}
			} else if confidence < 0.7 {
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
