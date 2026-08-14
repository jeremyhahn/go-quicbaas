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

package chunking

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
)

var (
	// ErrInvalidConfig indicates invalid chunker configuration.
	ErrInvalidConfig = errors.New("invalid chunker configuration")
	// ErrEmptyContent indicates empty content was provided.
	ErrEmptyContent = errors.New("empty content")
)

// TextChunkerConfig configures the text chunking behavior.
type TextChunkerConfig struct {
	ChunkSize         int  // Target size in tokens
	ChunkOverlap      int  // Overlap in tokens
	MinChunkSize      int  // Minimum chunk size
	RespectSentences  bool // Try to break on sentences
	RespectParagraphs bool // Try to break on paragraphs
}

// DefaultTextChunkerConfig returns sensible defaults.
func DefaultTextChunkerConfig() TextChunkerConfig {
	return TextChunkerConfig{
		ChunkSize:         512,
		ChunkOverlap:      51,
		MinChunkSize:      50,
		RespectSentences:  true,
		RespectParagraphs: true,
	}
}

// TextChunker implements text-based chunking with sliding window.
type TextChunker struct {
	config    TextChunkerConfig
	tokenizer Tokenizer

	// Pre-compiled regex patterns for efficiency
	paragraphSplitter *regexp.Regexp
}

// NewTextChunker creates a new text chunker with the given configuration.
func NewTextChunker(config TextChunkerConfig) *TextChunker {
	// Validate configuration
	if config.ChunkSize <= 0 {
		config.ChunkSize = 512
	}
	if config.ChunkOverlap < 0 {
		config.ChunkOverlap = 0
	}
	if config.ChunkOverlap >= config.ChunkSize {
		config.ChunkOverlap = config.ChunkSize / 10 // Default to 10% overlap
	}
	if config.MinChunkSize <= 0 {
		config.MinChunkSize = 50
	}

	return &TextChunker{
		config:            config,
		tokenizer:         NewSimpleTokenizer(),
		paragraphSplitter: regexp.MustCompile(`\n\s*\n`),
	}
}

// Chunk splits content into overlapping chunks.
func (c *TextChunker) Chunk(content []byte, metadata Metadata) ([]Chunk, error) {
	if len(content) == 0 {
		return nil, ErrEmptyContent
	}

	text := string(content)

	// Handle paragraph-based chunking if enabled
	if c.config.RespectParagraphs {
		return c.chunkByParagraphs(text, metadata)
	}

	// Handle sentence-based chunking if enabled
	if c.config.RespectSentences {
		return c.chunkBySentences(text, metadata)
	}

	// Fall back to token-based sliding window
	return c.chunkByTokens(text, metadata)
}

// Name returns the chunker strategy name.
func (c *TextChunker) Name() string {
	return "text"
}

// chunkByParagraphs splits text into chunks respecting paragraph boundaries.
func (c *TextChunker) chunkByParagraphs(text string, metadata Metadata) ([]Chunk, error) {
	paragraphs := c.paragraphSplitter.Split(text, -1)

	var chunks []Chunk
	var currentChunk strings.Builder
	var currentLines []string
	startLine := metadata.StartLine
	if startLine == 0 {
		startLine = 1
	}
	currentStartLine := startLine
	chunkIndex := 0

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		paraTokens := c.tokenizer.Count(para)
		currentTokens := c.tokenizer.Count(currentChunk.String())

		// If adding this paragraph exceeds chunk size, create a chunk
		if currentTokens > 0 && currentTokens+paraTokens > c.config.ChunkSize {
			// Create chunk from current content
			chunkText := currentChunk.String()
			if c.tokenizer.Count(chunkText) >= c.config.MinChunkSize {
				chunks = append(chunks, c.createChunk(
					chunkText,
					metadata,
					currentStartLine,
					currentStartLine+len(currentLines)-1,
					chunkIndex,
				))
				chunkIndex++
			}

			// Start new chunk with overlap
			overlap := c.getOverlapText(currentChunk.String())
			currentChunk.Reset()
			currentLines = nil

			if overlap != "" {
				currentChunk.WriteString(overlap)
				currentChunk.WriteString("\n\n")
				// Adjust start line for overlap
				overlapLines := strings.Count(overlap, "\n") + 1
				currentStartLine = currentStartLine + len(currentLines) - overlapLines
				currentLines = strings.Split(overlap, "\n")
			} else {
				currentStartLine += len(currentLines)
			}
		}

		// Add paragraph to current chunk
		if currentChunk.Len() > 0 {
			currentChunk.WriteString("\n\n")
		}
		currentChunk.WriteString(para)
		paraLines := strings.Split(para, "\n")
		currentLines = append(currentLines, paraLines...)
	}

	// Add final chunk
	if currentChunk.Len() > 0 {
		chunkText := currentChunk.String()
		if c.tokenizer.Count(chunkText) >= c.config.MinChunkSize {
			chunks = append(chunks, c.createChunk(
				chunkText,
				metadata,
				currentStartLine,
				currentStartLine+len(currentLines)-1,
				chunkIndex,
			))
		}
	}

	if len(chunks) == 0 {
		return nil, ErrNoChunksCreated
	}

	return chunks, nil
}

// chunkBySentences splits text into chunks respecting sentence boundaries.
func (c *TextChunker) chunkBySentences(text string, metadata Metadata) ([]Chunk, error) {
	sentences := c.splitSentences(text)

	var chunks []Chunk
	var currentChunk strings.Builder
	startLine := metadata.StartLine
	if startLine == 0 {
		startLine = 1
	}
	currentStartLine := startLine
	currentLineCount := 0
	chunkIndex := 0

	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}

		sentenceTokens := c.tokenizer.Count(sentence)
		currentTokens := c.tokenizer.Count(currentChunk.String())

		// If adding this sentence exceeds chunk size, create a chunk
		if currentTokens > 0 && currentTokens+sentenceTokens > c.config.ChunkSize {
			// Create chunk from current content
			chunkText := currentChunk.String()
			if c.tokenizer.Count(chunkText) >= c.config.MinChunkSize {
				chunks = append(chunks, c.createChunk(
					chunkText,
					metadata,
					currentStartLine,
					currentStartLine+currentLineCount,
					chunkIndex,
				))
				chunkIndex++
			}

			// Start new chunk with overlap
			overlap := c.getOverlapText(currentChunk.String())
			currentChunk.Reset()

			if overlap != "" {
				currentChunk.WriteString(overlap)
				currentChunk.WriteString(" ")
				overlapLines := strings.Count(overlap, "\n")
				currentStartLine = currentStartLine + currentLineCount - overlapLines
				currentLineCount = overlapLines
			} else {
				currentStartLine += currentLineCount
				currentLineCount = 0
			}
		}

		// Add sentence to current chunk
		if currentChunk.Len() > 0 {
			currentChunk.WriteString(" ")
		}
		currentChunk.WriteString(sentence)
		currentLineCount += strings.Count(sentence, "\n")
	}

	// Add final chunk
	if currentChunk.Len() > 0 {
		chunkText := currentChunk.String()
		if c.tokenizer.Count(chunkText) >= c.config.MinChunkSize {
			chunks = append(chunks, c.createChunk(
				chunkText,
				metadata,
				currentStartLine,
				currentStartLine+currentLineCount,
				chunkIndex,
			))
		}
	}

	if len(chunks) == 0 {
		return nil, ErrNoChunksCreated
	}

	return chunks, nil
}

// chunkByTokens splits text using a sliding window based on token count.
func (c *TextChunker) chunkByTokens(text string, metadata Metadata) ([]Chunk, error) {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil, ErrEmptyContent
	}

	var chunks []Chunk
	startLine := metadata.StartLine
	if startLine == 0 {
		startLine = 1
	}
	chunkIndex := 0

	i := 0
	for i < len(words) {
		var currentChunk strings.Builder
		tokenCount := 0
		startIdx := i

		// Build chunk up to target size
		for i < len(words) && tokenCount < c.config.ChunkSize {
			if currentChunk.Len() > 0 {
				currentChunk.WriteString(" ")
			}
			currentChunk.WriteString(words[i])
			tokenCount = c.tokenizer.Count(currentChunk.String())
			i++
		}

		chunkText := currentChunk.String()
		if c.tokenizer.Count(chunkText) >= c.config.MinChunkSize {
			lineCount := strings.Count(chunkText, "\n")
			chunks = append(chunks, c.createChunk(
				chunkText,
				metadata,
				startLine,
				startLine+lineCount,
				chunkIndex,
			))
			chunkIndex++
			startLine += lineCount
		}

		// Calculate overlap for next chunk
		if i < len(words) && c.config.ChunkOverlap > 0 {
			// Move back by overlap amount
			overlapWords := c.calculateOverlapWords(words[startIdx:i])
			i -= overlapWords
			if i < startIdx {
				i = startIdx + 1 // Ensure we make progress
			}
		}
	}

	if len(chunks) == 0 {
		return nil, ErrNoChunksCreated
	}

	return chunks, nil
}

// createChunk creates a Chunk with proper metadata.
func (c *TextChunker) createChunk(content string, metadata Metadata, startLine, endLine, index int) Chunk {
	chunkMetadata := metadata.Clone()
	chunkMetadata.StartLine = startLine
	chunkMetadata.EndLine = endLine

	return Chunk{
		Content:  content,
		Metadata: chunkMetadata,
		Tokens:   c.tokenizer.Count(content),
		Index:    index,
	}
}

// getOverlapText extracts overlap text from the end of current chunk.
func (c *TextChunker) getOverlapText(text string) string {
	tokens := c.tokenizer.Count(text)
	if tokens <= c.config.ChunkOverlap {
		return text
	}

	// Estimate character position for overlap
	// This is approximate but works well in practice
	ratio := float64(c.config.ChunkOverlap) / float64(tokens)
	overlapStart := int(float64(len(text)) * (1.0 - ratio))

	if overlapStart < 0 {
		overlapStart = 0
	}
	if overlapStart >= len(text) {
		return ""
	}

	overlap := text[overlapStart:]

	// Try to start at a word boundary
	if idx := strings.Index(overlap, " "); idx > 0 && idx < len(overlap)/2 {
		overlap = overlap[idx+1:]
	}

	return strings.TrimSpace(overlap)
}

// calculateOverlapWords calculates how many words to overlap.
func (c *TextChunker) calculateOverlapWords(words []string) int {
	totalText := strings.Join(words, " ")
	totalTokens := c.tokenizer.Count(totalText)

	if totalTokens <= c.config.ChunkOverlap {
		return len(words) - 1 // Keep at least some overlap
	}

	// Calculate approximate word count for overlap
	ratio := float64(c.config.ChunkOverlap) / float64(totalTokens)
	overlapWords := int(float64(len(words)) * ratio)

	if overlapWords <= 0 {
		overlapWords = 1
	}
	if overlapWords >= len(words) {
		overlapWords = len(words) - 1
	}

	return overlapWords
}

// splitSentences splits text into sentences, handling common abbreviations.
func (c *TextChunker) splitSentences(text string) []string {
	// Replace common abbreviations to prevent splitting
	abbrevs := map[string]string{
		"Dr.":   "Dr",
		"Mr.":   "Mr",
		"Mrs.":  "Mrs",
		"Ms.":   "Ms",
		"Jr.":   "Jr",
		"Sr.":   "Sr",
		"vs.":   "vs",
		"etc.":  "etc",
		"i.e.":  "ie",
		"e.g.":  "eg",
		"Ph.D.": "PhD",
		"B.A.":  "BA",
		"M.A.":  "MA",
	}

	// Create a copy of text to work with
	processedText := text
	for abbrev, replacement := range abbrevs {
		processedText = strings.ReplaceAll(processedText, abbrev, replacement)
	}

	// Split into sentences using manual parsing
	var sentences []string
	var currentSentence strings.Builder
	runes := []rune(processedText)

	for i := 0; i < len(runes); i++ {
		r := runes[i]
		currentSentence.WriteRune(r)

		// Check if we're at a sentence boundary
		if r == '.' || r == '!' || r == '?' {
			// Look ahead to see if there's whitespace and a capital letter
			if i+1 < len(runes) {
				nextIdx := i + 1
				// Skip whitespace
				for nextIdx < len(runes) && unicode.IsSpace(runes[nextIdx]) {
					currentSentence.WriteRune(runes[nextIdx])
					nextIdx++
					i++
				}

				// Check if next non-whitespace character is uppercase (new sentence)
				if nextIdx < len(runes) && unicode.IsUpper(runes[nextIdx]) {
					sentence := strings.TrimSpace(currentSentence.String())
					if sentence != "" {
						sentences = append(sentences, sentence)
					}
					currentSentence.Reset()
				}
			} else {
				// End of text
				sentence := strings.TrimSpace(currentSentence.String())
				if sentence != "" {
					sentences = append(sentences, sentence)
				}
				currentSentence.Reset()
			}
		}
	}

	// Add any remaining content as final sentence
	if currentSentence.Len() > 0 {
		sentence := strings.TrimSpace(currentSentence.String())
		if sentence != "" {
			sentences = append(sentences, sentence)
		}
	}

	return sentences
}
