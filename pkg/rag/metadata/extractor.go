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
	"sort"
	"strings"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/rag/chunking"
)

// Extractor defines the metadata extraction interface.
type Extractor interface {
	// Extract metadata from a chunk
	Extract(chunk chunking.Chunk, fileInfo FileInfo) ChunkMetadata

	// ExtractFileInfo gets file-level metadata
	ExtractFileInfo(path string) (FileInfo, error)

	// ExtractGitInfo gets git metadata (optional)
	ExtractGitInfo(path string) (*GitInfo, error)
}

// extractor implements the Extractor interface.
type extractor struct {
	config   ExtractorConfig
	gitCache map[string]*GitInfo
	mu       sync.RWMutex // Protects gitCache
}

// NewExtractor creates a new metadata extractor.
func NewExtractor(config ExtractorConfig) Extractor {
	return &extractor{
		config:   config,
		gitCache: make(map[string]*GitInfo),
	}
}

// Extract extracts comprehensive metadata from a chunk.
func (e *extractor) Extract(chunk chunking.Chunk, fileInfo FileInfo) ChunkMetadata {
	metadata := ChunkMetadata{
		FilePath:  fileInfo.Path,
		FileName:  filepath.Base(fileInfo.Path),
		FileSize:  fileInfo.Size,
		Language:  fileInfo.Language,
		ModTime:   fileInfo.ModTime,
		StartLine: chunk.Metadata.StartLine,
		EndLine:   chunk.Metadata.EndLine,
	}

	// Copy code context from chunk metadata
	metadata.PackageName = chunk.Metadata.FunctionName
	metadata.FunctionName = chunk.Metadata.FunctionName
	metadata.ClassName = chunk.Metadata.ClassName
	metadata.Tags = chunk.Metadata.Tags

	// Copy custom context
	if chunk.Metadata.Context != nil {
		metadata.Context = make(map[string]string, len(chunk.Metadata.Context))
		for k, v := range chunk.Metadata.Context {
			metadata.Context[k] = v
		}
	}

	// Determine scope
	metadata.Scope = e.determineScope(chunk)

	// Extract git info if enabled
	if e.config.EnableGit {
		if gitInfo, err := e.ExtractGitInfo(fileInfo.Path); err == nil && gitInfo != nil {
			metadata.GitRepo = gitInfo.Repo
			metadata.GitBranch = gitInfo.Branch
			metadata.GitCommit = gitInfo.Commit
			metadata.GitAuthor = gitInfo.Author
		}
	}

	// Extract keywords if enabled
	if e.config.ExtractKeywords {
		metadata.Keywords = e.extractKeywords(chunk.Content)
	}

	// Set quality indicators
	metadata.TooLarge = chunk.Tokens > 8192
	// Check if context has incomplete marker
	if chunk.Metadata.Context != nil {
		if incomplete, ok := chunk.Metadata.Context["incomplete"]; ok && incomplete == "true" {
			metadata.Incomplete = true
		}
	}

	return metadata
}

// ExtractFileInfo extracts file-level metadata.
func (e *extractor) ExtractFileInfo(path string) (FileInfo, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return FileInfo{}, err
	}

	info := FileInfo{
		Path:    path,
		Size:    stat.Size(),
		ModTime: stat.ModTime(),
		IsDir:   stat.IsDir(),
	}

	// Detect language from file extension
	if !stat.IsDir() {
		info.Language = detectLanguage(path)
	}

	return info, nil
}

// ExtractGitInfo extracts git repository metadata.
func (e *extractor) ExtractGitInfo(path string) (*GitInfo, error) {
	// Find directory containing the file
	dir := path
	if stat, err := os.Stat(path); err == nil && !stat.IsDir() {
		dir = filepath.Dir(path)
	}

	// Check cache
	if e.config.CacheGitInfo {
		e.mu.RLock()
		if info, ok := e.gitCache[dir]; ok {
			e.mu.RUnlock()
			return info, nil
		}
		e.mu.RUnlock()
	}

	// Find .git directory
	gitDir := findGitDir(dir)
	if gitDir == "" {
		return nil, ErrNotGitRepository
	}

	info := &GitInfo{}

	// Get remote URL
	if output, err := e.runGitCommand(gitDir, "config", "--get", "remote.origin.url"); err == nil {
		info.Repo = strings.TrimSpace(output)
	}

	// Get current branch
	if output, err := e.runGitCommand(gitDir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		info.Branch = strings.TrimSpace(output)
	}

	// Get current commit
	if output, err := e.runGitCommand(gitDir, "rev-parse", "HEAD"); err == nil {
		commit := strings.TrimSpace(output)
		if len(commit) > 7 {
			commit = commit[:7] // Short hash
		}
		info.Commit = commit
	}

	// Get author of last commit for this file
	if output, err := e.runGitCommand(gitDir, "log", "-1", "--format=%an <%ae>", "--", path); err == nil {
		info.Author = strings.TrimSpace(output)
	}

	// Cache result
	if e.config.CacheGitInfo {
		e.mu.Lock()
		e.gitCache[dir] = info
		e.mu.Unlock()
	}

	return info, nil
}

// runGitCommand executes a git command and returns output.
func (e *extractor) runGitCommand(gitDir string, args ...string) (string, error) {
	cmdArgs := append([]string{"-C", gitDir}, args...)
	cmd := exec.Command("git", cmdArgs...)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

// findGitDir finds the .git directory by walking up the directory tree.
func findGitDir(startDir string) string {
	dir := startDir
	for {
		gitPath := filepath.Join(dir, ".git")
		if stat, err := os.Stat(gitPath); err == nil && stat.IsDir() {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached root
			return ""
		}
		dir = parent
	}
}

// determineScope determines the scope of a chunk.
func (e *extractor) determineScope(chunk chunking.Chunk) string {
	if chunk.Metadata.FunctionName != "" {
		if chunk.Metadata.ClassName != "" {
			return "method"
		}
		return "function"
	}
	if chunk.Metadata.ClassName != "" {
		return "class"
	}
	return "global"
}

// extractKeywords extracts the most frequent keywords from content.
func (e *extractor) extractKeywords(content string) []string {
	// Simple keyword extraction:
	// 1. Split into words
	// 2. Remove stop words
	// 3. Count frequency
	// 4. Return top N

	words := strings.Fields(strings.ToLower(content))
	wordCount := make(map[string]int)

	for _, word := range words {
		// Remove punctuation
		word = strings.Trim(word, ".,!?;:()[]{}\"'`")

		// Skip short words and stop words
		if len(word) < 4 || isStopWord(word) {
			continue
		}

		wordCount[word]++
	}

	// Sort by frequency and return top N
	return topN(wordCount, e.config.MaxKeywords)
}

// topN returns the top N keys by value from a map.
func topN(wordCount map[string]int, n int) []string {
	type kv struct {
		key   string
		value int
	}

	var sorted []kv
	for k, v := range wordCount {
		sorted = append(sorted, kv{k, v})
	}

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].value > sorted[j].value
	})

	result := make([]string, 0, n)
	for i := 0; i < len(sorted) && i < n; i++ {
		result = append(result, sorted[i].key)
	}

	return result
}

// isStopWord checks if a word is a stop word.
func isStopWord(word string) bool {
	return stopWords[word]
}

// stopWords contains common English stop words.
var stopWords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true,
	"that": true, "this": true, "from": true, "have": true,
	"been": true, "has": true, "had": true, "were": true,
	"was": true, "are": true, "but": true, "not": true,
	"you": true, "all": true, "can": true, "her": true,
	"will": true, "one": true, "our": true, "out": true,
	"more": true, "when": true, "there": true, "into": true,
	"which": true, "them": true, "than": true, "then": true,
	"your": true, "what": true, "about": true, "would": true,
	"their": true, "they": true, "some": true, "these": true,
	"only": true, "other": true, "such": true, "could": true,
	"should": true, "also": true, "make": true, "like": true,
	"well": true, "very": true, "just": true, "here": true,
}

// detectLanguage detects programming language from file extension.
func detectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	languages := map[string]string{
		".go":    "Go",
		".py":    "Python",
		".js":    "JavaScript",
		".ts":    "TypeScript",
		".java":  "Java",
		".c":     "C",
		".cpp":   "C++",
		".cc":    "C++",
		".h":     "C",
		".hpp":   "C++",
		".rs":    "Rust",
		".rb":    "Ruby",
		".php":   "PHP",
		".cs":    "C#",
		".swift": "Swift",
		".kt":    "Kotlin",
		".scala": "Scala",
		".sh":    "Shell",
		".bash":  "Bash",
		".sql":   "SQL",
		".md":    "Markdown",
		".txt":   "Text",
		".json":  "JSON",
		".yaml":  "YAML",
		".yml":   "YAML",
		".xml":   "XML",
		".html":  "HTML",
		".css":   "CSS",
	}

	if lang, ok := languages[ext]; ok {
		return lang
	}
	return "Unknown"
}
