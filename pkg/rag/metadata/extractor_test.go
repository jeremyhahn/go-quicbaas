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

package metadata

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/rag/chunking"
)

func TestNewExtractor(t *testing.T) {
	config := DefaultConfig()
	extractor := NewExtractor(config)
	assert.NotNil(t, extractor)
}

func TestExtractor_Extract_Basic(t *testing.T) {
	extractor := NewExtractor(ExtractorConfig{
		EnableGit:       false,
		ExtractKeywords: false,
	})

	chunk := chunking.Chunk{
		Content: "package main\nfunc Hello() {\n\tfmt.Println(\"hello\")\n}",
		Metadata: chunking.Metadata{
			FunctionName: "Hello",
			StartLine:    2,
			EndLine:      4,
		},
		Tokens: 100,
		Index:  0,
	}

	fileInfo := FileInfo{
		Path:     "/path/to/main.go",
		Size:     1024,
		ModTime:  time.Now(),
		Language: "Go",
	}

	metadata := extractor.Extract(chunk, fileInfo)

	assert.Equal(t, "main.go", metadata.FileName)
	assert.Equal(t, "/path/to/main.go", metadata.FilePath)
	assert.Equal(t, int64(1024), metadata.FileSize)
	assert.Equal(t, "Go", metadata.Language)
	assert.Equal(t, "Hello", metadata.FunctionName)
	assert.Equal(t, 2, metadata.StartLine)
	assert.Equal(t, 4, metadata.EndLine)
	assert.Equal(t, "function", metadata.Scope)
	assert.False(t, metadata.TooLarge)
	assert.False(t, metadata.Incomplete)
}

func TestExtractor_Extract_WithKeywords(t *testing.T) {
	extractor := NewExtractor(ExtractorConfig{
		EnableGit:       false,
		ExtractKeywords: true,
		MaxKeywords:     5,
	})

	chunk := chunking.Chunk{
		Content: "database connection pool manager handles multiple connections efficiently",
		Tokens:  50,
	}

	fileInfo := FileInfo{
		Path:     "/path/to/db.go",
		Language: "Go",
	}

	metadata := extractor.Extract(chunk, fileInfo)

	assert.NotEmpty(t, metadata.Keywords)
	// Keywords should exclude stop words like "the", "and", "for"
	for _, keyword := range metadata.Keywords {
		assert.False(t, isStopWord(keyword))
		assert.GreaterOrEqual(t, len(keyword), 4)
	}
}

func TestExtractor_Extract_TooLarge(t *testing.T) {
	extractor := NewExtractor(ExtractorConfig{
		EnableGit:       false,
		ExtractKeywords: false,
	})

	chunk := chunking.Chunk{
		Content: "large content",
		Tokens:  10000, // Exceeds 8192 threshold
	}

	fileInfo := FileInfo{
		Path: "/path/to/large.go",
	}

	metadata := extractor.Extract(chunk, fileInfo)
	assert.True(t, metadata.TooLarge)
}

func TestExtractor_Extract_Incomplete(t *testing.T) {
	extractor := NewExtractor(ExtractorConfig{
		EnableGit:       false,
		ExtractKeywords: false,
	})

	chunk := chunking.Chunk{
		Content: "incomplete chunk",
		Metadata: chunking.Metadata{
			Context: map[string]string{
				"incomplete": "true",
			},
		},
		Tokens: 100,
	}

	fileInfo := FileInfo{
		Path: "/path/to/file.go",
	}

	metadata := extractor.Extract(chunk, fileInfo)
	assert.True(t, metadata.Incomplete)
}

func TestExtractor_Extract_Scope(t *testing.T) {
	testCases := []struct {
		name          string
		chunk         chunking.Chunk
		expectedScope string
	}{
		{
			name: "global scope",
			chunk: chunking.Chunk{
				Content: "var globalVar = 42",
			},
			expectedScope: "global",
		},
		{
			name: "function scope",
			chunk: chunking.Chunk{
				Content: "func Process() {}",
				Metadata: chunking.Metadata{
					FunctionName: "Process",
				},
			},
			expectedScope: "function",
		},
		{
			name: "method scope",
			chunk: chunking.Chunk{
				Content: "func (s *Service) Handle() {}",
				Metadata: chunking.Metadata{
					FunctionName: "Handle",
					ClassName:    "Service",
				},
			},
			expectedScope: "method",
		},
		{
			name: "class scope",
			chunk: chunking.Chunk{
				Content: "type Service struct {}",
				Metadata: chunking.Metadata{
					ClassName: "Service",
				},
			},
			expectedScope: "class",
		},
	}

	extractor := NewExtractor(ExtractorConfig{
		EnableGit:       false,
		ExtractKeywords: false,
	})

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fileInfo := FileInfo{Path: "/test.go"}
			metadata := extractor.Extract(tc.chunk, fileInfo)
			assert.Equal(t, tc.expectedScope, metadata.Scope)
		})
	}
}

func TestExtractor_Extract_WithTags(t *testing.T) {
	extractor := NewExtractor(ExtractorConfig{
		EnableGit:       false,
		ExtractKeywords: false,
	})

	chunk := chunking.Chunk{
		Content: "test content",
		Metadata: chunking.Metadata{
			Tags: []string{"important", "api"},
		},
	}

	fileInfo := FileInfo{Path: "/test.go"}
	metadata := extractor.Extract(chunk, fileInfo)

	assert.Equal(t, []string{"important", "api"}, metadata.Tags)
}

func TestExtractor_Extract_WithContext(t *testing.T) {
	extractor := NewExtractor(ExtractorConfig{
		EnableGit:       false,
		ExtractKeywords: false,
	})

	chunk := chunking.Chunk{
		Content: "test content",
		Metadata: chunking.Metadata{
			Context: map[string]string{
				"module":  "auth",
				"version": "v1",
			},
		},
	}

	fileInfo := FileInfo{Path: "/test.go"}
	metadata := extractor.Extract(chunk, fileInfo)

	assert.NotNil(t, metadata.Context)
	assert.Equal(t, "auth", metadata.Context["module"])
	assert.Equal(t, "v1", metadata.Context["version"])
}

func TestExtractor_Extract_WithGitInfo(t *testing.T) {
	// Require git to be available
	_, err := exec.LookPath("git")
	require.NoError(t, err, "git must be available in the CI environment")

	// Create temp git repo for testing
	tmpDir := t.TempDir()

	// Initialize git repo
	cmd := exec.Command("git", "init", tmpDir)
	require.NoError(t, cmd.Run())

	// Configure git
	cmd = exec.Command("git", "-C", tmpDir, "config", "user.name", "Test User")
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "-C", tmpDir, "config", "user.email", "test@example.com")
	require.NoError(t, cmd.Run())

	// Disable GPG signing for test commits
	cmd = exec.Command("git", "-C", tmpDir, "config", "commit.gpgsign", "false")
	require.NoError(t, cmd.Run())

	// Set remote URL
	cmd = exec.Command("git", "-C", tmpDir, "remote", "add", "origin", "https://github.com/test/repo.git")
	require.NoError(t, cmd.Run())

	// Create a test file
	testFile := filepath.Join(tmpDir, "test.go")
	err = os.WriteFile(testFile, []byte("package main"), 0644)
	require.NoError(t, err)

	// Add and commit
	cmd = exec.Command("git", "-C", tmpDir, "add", ".")
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "-C", tmpDir, "commit", "-m", "initial commit")
	require.NoError(t, cmd.Run())

	// Test extraction with git enabled
	extractor := NewExtractor(ExtractorConfig{
		EnableGit:       true,
		ExtractKeywords: false,
		CacheGitInfo:    false,
	})

	chunk := chunking.Chunk{
		Content: "package main\nfunc main() {}",
		Metadata: chunking.Metadata{
			FunctionName: "main",
		},
		Tokens: 50,
	}

	fileInfo := FileInfo{
		Path:     testFile,
		Size:     1024,
		ModTime:  time.Now(),
		Language: "Go",
	}

	metadata := extractor.Extract(chunk, fileInfo)

	// Verify git metadata was populated
	assert.NotEmpty(t, metadata.GitRepo)
	assert.Equal(t, "https://github.com/test/repo.git", metadata.GitRepo)
	assert.NotEmpty(t, metadata.GitBranch)
	assert.NotEmpty(t, metadata.GitCommit)
	assert.Contains(t, metadata.GitAuthor, "Test User")
}

func TestExtractor_Extract_WithGitInfoError(t *testing.T) {
	// Test that extraction continues even if git info fails
	extractor := NewExtractor(ExtractorConfig{
		EnableGit:       true,
		ExtractKeywords: false,
	})

	chunk := chunking.Chunk{
		Content: "package main",
		Tokens:  50,
	}

	// Use a non-git directory
	tmpDir := t.TempDir()
	fileInfo := FileInfo{
		Path:     filepath.Join(tmpDir, "test.go"),
		Size:     1024,
		ModTime:  time.Now(),
		Language: "Go",
	}

	// Should not panic and should return metadata without git info
	metadata := extractor.Extract(chunk, fileInfo)

	assert.Empty(t, metadata.GitRepo)
	assert.Empty(t, metadata.GitBranch)
	assert.Empty(t, metadata.GitCommit)
	assert.Empty(t, metadata.GitAuthor)
}

func TestExtractor_ExtractFileInfo(t *testing.T) {
	// Create a temporary file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")
	content := []byte("package main")
	err := os.WriteFile(testFile, content, 0644)
	require.NoError(t, err)

	extractor := NewExtractor(DefaultConfig())
	fileInfo, err := extractor.ExtractFileInfo(testFile)

	assert.NoError(t, err)
	assert.Equal(t, testFile, fileInfo.Path)
	assert.Equal(t, int64(len(content)), fileInfo.Size)
	assert.False(t, fileInfo.IsDir)
	assert.Equal(t, "Go", fileInfo.Language)
}

func TestExtractor_ExtractFileInfo_Directory(t *testing.T) {
	tmpDir := t.TempDir()

	extractor := NewExtractor(DefaultConfig())
	fileInfo, err := extractor.ExtractFileInfo(tmpDir)

	assert.NoError(t, err)
	assert.True(t, fileInfo.IsDir)
}

func TestExtractor_ExtractFileInfo_NotFound(t *testing.T) {
	extractor := NewExtractor(DefaultConfig())
	_, err := extractor.ExtractFileInfo("/nonexistent/file.go")

	assert.Error(t, err)
}

func TestExtractor_ExtractGitInfo(t *testing.T) {
	// Require git to be available
	_, err := exec.LookPath("git")
	require.NoError(t, err, "git must be available in the CI environment")

	// Create temp git repo for testing
	tmpDir := t.TempDir()

	// Initialize git repo
	cmd := exec.Command("git", "init", tmpDir)
	require.NoError(t, cmd.Run())

	// Configure git
	cmd = exec.Command("git", "-C", tmpDir, "config", "user.name", "Test User")
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "-C", tmpDir, "config", "user.email", "test@example.com")
	require.NoError(t, cmd.Run())

	// Disable GPG signing for test commits
	cmd = exec.Command("git", "-C", tmpDir, "config", "commit.gpgsign", "false")
	require.NoError(t, cmd.Run())

	// Set remote URL
	cmd = exec.Command("git", "-C", tmpDir, "remote", "add", "origin", "https://github.com/test/repo.git")
	require.NoError(t, cmd.Run())

	// Create a test file
	testFile := filepath.Join(tmpDir, "test.go")
	err = os.WriteFile(testFile, []byte("package main"), 0644)
	require.NoError(t, err)

	// Add and commit
	cmd = exec.Command("git", "-C", tmpDir, "add", ".")
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "-C", tmpDir, "commit", "-m", "initial commit")
	require.NoError(t, cmd.Run())

	// Test extraction
	extractor := NewExtractor(ExtractorConfig{
		EnableGit:    true,
		CacheGitInfo: false,
	})

	gitInfo, err := extractor.ExtractGitInfo(testFile)

	assert.NoError(t, err)
	assert.NotNil(t, gitInfo)
	assert.NotEmpty(t, gitInfo.Commit)
	assert.NotEmpty(t, gitInfo.Branch)
	assert.Contains(t, gitInfo.Author, "Test User")
	assert.Equal(t, "https://github.com/test/repo.git", gitInfo.Repo)
}

func TestExtractor_ExtractGitInfo_NotGitRepo(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")
	err := os.WriteFile(testFile, []byte("package main"), 0644)
	require.NoError(t, err)

	extractor := NewExtractor(ExtractorConfig{EnableGit: true})
	_, err = extractor.ExtractGitInfo(testFile)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a git repository")
}

func TestExtractor_ExtractGitInfo_Caching(t *testing.T) {
	// Require git to be available
	_, err := exec.LookPath("git")
	require.NoError(t, err, "git must be available in the CI environment")

	// Create temp git repo
	tmpDir := t.TempDir()

	cmd := exec.Command("git", "init", tmpDir)
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "-C", tmpDir, "config", "user.name", "Test")
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "-C", tmpDir, "config", "user.email", "test@test.com")
	require.NoError(t, cmd.Run())

	// Disable GPG signing for test commits
	cmd = exec.Command("git", "-C", tmpDir, "config", "commit.gpgsign", "false")
	require.NoError(t, cmd.Run())

	testFile := filepath.Join(tmpDir, "test.go")
	err = os.WriteFile(testFile, []byte("package main"), 0644)
	require.NoError(t, err)

	cmd = exec.Command("git", "-C", tmpDir, "add", ".")
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "-C", tmpDir, "commit", "-m", "test")
	require.NoError(t, cmd.Run())

	// Test with caching enabled
	extractor := NewExtractor(ExtractorConfig{
		EnableGit:    true,
		CacheGitInfo: true,
	})

	// First call - should cache
	gitInfo1, err := extractor.ExtractGitInfo(testFile)
	assert.NoError(t, err)

	// Second call - should use cache
	gitInfo2, err := extractor.ExtractGitInfo(testFile)
	assert.NoError(t, err)

	// Should be the same
	assert.Equal(t, gitInfo1.Commit, gitInfo2.Commit)
	assert.Equal(t, gitInfo1.Branch, gitInfo2.Branch)
}

func TestExtractor_ExtractGitInfo_DirectoryPath(t *testing.T) {
	// Require git to be available
	_, err := exec.LookPath("git")
	require.NoError(t, err, "git must be available in the CI environment")

	// Create temp git repo
	tmpDir := t.TempDir()

	cmd := exec.Command("git", "init", tmpDir)
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "-C", tmpDir, "config", "user.name", "Test")
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "-C", tmpDir, "config", "user.email", "test@test.com")
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "-C", tmpDir, "config", "commit.gpgsign", "false")
	require.NoError(t, cmd.Run())

	testFile := filepath.Join(tmpDir, "test.go")
	err = os.WriteFile(testFile, []byte("package main"), 0644)
	require.NoError(t, err)

	cmd = exec.Command("git", "-C", tmpDir, "add", ".")
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "-C", tmpDir, "commit", "-m", "test")
	require.NoError(t, cmd.Run())

	extractor := NewExtractor(ExtractorConfig{
		EnableGit:    true,
		CacheGitInfo: false,
	})

	// Test with directory path instead of file path
	gitInfo, err := extractor.ExtractGitInfo(tmpDir)

	assert.NoError(t, err)
	assert.NotNil(t, gitInfo)
	assert.NotEmpty(t, gitInfo.Branch)
	assert.NotEmpty(t, gitInfo.Commit)
}

func TestExtractKeywords(t *testing.T) {
	extractor := &extractor{
		config: ExtractorConfig{
			MaxKeywords: 3,
		},
	}

	content := `
	The database connection pool manager handles multiple database connections
	efficiently. The pool ensures optimal resource utilization through connection
	reuse and automatic cleanup. Database performance is critical for scalability.
	`

	keywords := extractor.extractKeywords(content)

	assert.LessOrEqual(t, len(keywords), 3)

	// Should not contain stop words
	for _, keyword := range keywords {
		assert.False(t, isStopWord(keyword))
	}

	// "database" should appear since it's frequent and not a stop word
	containsDatabase := false
	for _, kw := range keywords {
		if kw == "database" {
			containsDatabase = true
			break
		}
	}
	assert.True(t, containsDatabase, "Expected 'database' in keywords")
}

func TestExtractKeywords_Empty(t *testing.T) {
	extractor := &extractor{
		config: ExtractorConfig{
			MaxKeywords: 5,
		},
	}

	keywords := extractor.extractKeywords("")
	assert.Empty(t, keywords)
}

func TestExtractKeywords_OnlyStopWords(t *testing.T) {
	extractor := &extractor{
		config: ExtractorConfig{
			MaxKeywords: 5,
		},
	}

	keywords := extractor.extractKeywords("the and for with that this from")
	assert.Empty(t, keywords)
}

func TestDetectLanguage(t *testing.T) {
	testCases := []struct {
		path     string
		expected string
	}{
		{"/path/to/file.go", "Go"},
		{"/path/to/script.py", "Python"},
		{"/path/to/app.js", "JavaScript"},
		{"/path/to/App.java", "Java"},
		{"/path/to/main.rs", "Rust"},
		{"/path/to/style.css", "CSS"},
		{"/path/to/doc.md", "Markdown"},
		{"/path/to/unknown.xyz", "Unknown"},
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			lang := detectLanguage(tc.path)
			assert.Equal(t, tc.expected, lang)
		})
	}
}

func TestFindGitDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Create nested directories
	nestedDir := filepath.Join(tmpDir, "a", "b", "c")
	err := os.MkdirAll(nestedDir, 0755)
	require.NoError(t, err)

	// Create .git directory at root
	gitDir := filepath.Join(tmpDir, ".git")
	err = os.Mkdir(gitDir, 0755)
	require.NoError(t, err)

	// Should find .git from nested directory
	found := findGitDir(nestedDir)
	assert.Equal(t, tmpDir, found)
}

func TestFindGitDir_NotFound(t *testing.T) {
	tmpDir := t.TempDir()

	found := findGitDir(tmpDir)
	assert.Empty(t, found)
}

func TestIsStopWord(t *testing.T) {
	assert.True(t, isStopWord("the"))
	assert.True(t, isStopWord("and"))
	assert.True(t, isStopWord("for"))
	assert.False(t, isStopWord("database"))
	assert.False(t, isStopWord("function"))
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.True(t, config.EnableGit)
	assert.True(t, config.ExtractKeywords)
	assert.Equal(t, 10, config.MaxKeywords)
	assert.True(t, config.CacheGitInfo)
}

// Benchmark tests
func BenchmarkExtractor_Extract(b *testing.B) {
	extractor := NewExtractor(ExtractorConfig{
		EnableGit:       false,
		ExtractKeywords: true,
		MaxKeywords:     10,
	})

	chunk := chunking.Chunk{
		Content: `package main

import "fmt"

func Hello() {
	fmt.Println("Hello, World!")
	// This is a sample function for benchmarking
	// It contains multiple lines of code
	for i := 0; i < 10; i++ {
		fmt.Println(i)
	}
}`,
		Metadata: chunking.Metadata{
			FunctionName: "Hello",
			StartLine:    5,
			EndLine:      12,
		},
		Tokens: 100,
	}

	fileInfo := FileInfo{
		Path:     "/test/main.go",
		Size:     500,
		ModTime:  time.Now(),
		Language: "Go",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = extractor.Extract(chunk, fileInfo)
	}
}

func BenchmarkExtractKeywords(b *testing.B) {
	extractor := &extractor{
		config: ExtractorConfig{
			MaxKeywords: 10,
		},
	}

	content := `
	The database connection pool manager handles multiple database connections
	efficiently. The pool ensures optimal resource utilization through connection
	reuse and automatic cleanup. Database performance is critical for application
	scalability and reliability. Connection pooling reduces overhead and improves
	throughput by reusing existing connections rather than creating new ones.
	`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = extractor.extractKeywords(content)
	}
}

func BenchmarkDetectLanguage(b *testing.B) {
	paths := []string{
		"/path/to/file.go",
		"/path/to/script.py",
		"/path/to/app.js",
		"/path/to/style.css",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, path := range paths {
			_ = detectLanguage(path)
		}
	}
}
