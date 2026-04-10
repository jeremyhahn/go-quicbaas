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
	"fmt"
	"regexp"
	"strings"
)

// ErrNoChunksCreated is returned when no chunks could be created from the content.
var ErrNoChunksCreated = errors.New("no chunks created from markdown content")

// MarkdownChunkerConfig configures the markdown chunking behavior.
type MarkdownChunkerConfig struct {
	MaxHeaderDepth   int  // Maximum header depth to consider (1-6)
	KeepCodeBlocks   bool // Keep code blocks together
	IncludeHierarchy bool // Include parent headers for context
	TargetTokens     int  // Target chunk size in tokens
	MaxTokens        int  // Maximum tokens per chunk
	MinChunkSize     int  // Minimum chunk size in tokens
}

// DefaultMarkdownChunkerConfig returns sensible defaults.
func DefaultMarkdownChunkerConfig() MarkdownChunkerConfig {
	return MarkdownChunkerConfig{
		MaxHeaderDepth:   3,
		KeepCodeBlocks:   true,
		IncludeHierarchy: true,
		TargetTokens:     512,
		MaxTokens:        2000,
		MinChunkSize:     50,
	}
}

// MarkdownChunker implements markdown-aware chunking.
type MarkdownChunker struct {
	config    MarkdownChunkerConfig
	tokenizer Tokenizer

	// Pre-compiled regex patterns
	headerPattern    *regexp.Regexp
	codeBlockPattern *regexp.Regexp
	listItemPattern  *regexp.Regexp
}

// NewMarkdownChunker creates a new markdown chunker.
func NewMarkdownChunker(config MarkdownChunkerConfig) *MarkdownChunker {
	// Validate and set defaults
	if config.TargetTokens <= 0 {
		config.TargetTokens = 512
	}
	if config.MaxTokens <= 0 {
		config.MaxTokens = 2000
	}
	if config.MaxTokens < config.TargetTokens {
		config.MaxTokens = config.TargetTokens * 2
	}
	if config.MinChunkSize <= 0 {
		config.MinChunkSize = 50
	}
	if config.MaxHeaderDepth <= 0 || config.MaxHeaderDepth > 6 {
		config.MaxHeaderDepth = 3
	}

	return &MarkdownChunker{
		config:           config,
		tokenizer:        NewSimpleTokenizer(),
		headerPattern:    regexp.MustCompile(`^(#{1,6})\s+(.+)$`),
		codeBlockPattern: regexp.MustCompile("^```"),
		listItemPattern:  regexp.MustCompile(`^(\s*)([-*+]|\d+\.)\s+`),
	}
}

// Name returns the chunker name.
func (c *MarkdownChunker) Name() string {
	return "markdown"
}

// Chunk splits markdown content into semantic chunks.
func (c *MarkdownChunker) Chunk(content []byte, metadata Metadata) ([]Chunk, error) {
	if len(content) == 0 {
		return nil, ErrEmptyContent
	}

	text := string(content)
	lines := strings.Split(text, "\n")

	// Parse document structure
	sections := c.parseStructure(lines)

	// Build chunks from sections
	chunks := c.buildChunks(sections, metadata)

	// Number chunks
	for i := range chunks {
		chunks[i].Index = i
	}

	if len(chunks) == 0 {
		return nil, ErrNoChunksCreated
	}

	return chunks, nil
}

// section represents a markdown section.
type section struct {
	headerLevel int      // 0 for no header, 1-6 for h1-h6
	headerText  string   // Header text (without # markers)
	startLine   int      // Starting line number (1-based)
	endLine     int      // Ending line number (1-based)
	content     []string // Content lines (excluding header)
	hierarchy   []string // Parent headers
}

// parseStructure parses the markdown document into sections.
func (c *MarkdownChunker) parseStructure(lines []string) []section {
	var sections []section
	var currentSection *section
	headerStack := make([]headerInfo, 0, 6)

	inCodeBlock := false

	for i, line := range lines {
		lineNum := i + 1

		// Track code blocks
		if c.codeBlockPattern.MatchString(line) {
			if !inCodeBlock {
				inCodeBlock = true
			} else {
				inCodeBlock = false
				// Code block is already added line by line
			}
		}

		// Check for header
		if matches := c.headerPattern.FindStringSubmatch(line); matches != nil && !inCodeBlock {
			level := len(matches[1]) // Number of # characters
			text := strings.TrimSpace(matches[2])

			// Only process headers up to max depth
			if level <= c.config.MaxHeaderDepth {
				// Save current section if it exists
				if currentSection != nil {
					currentSection.endLine = lineNum - 1
					sections = append(sections, *currentSection)
				}

				// Update header stack
				headerStack = c.updateHeaderStack(headerStack, level, text)

				// Create new section
				hierarchy := make([]string, 0, len(headerStack))
				for _, h := range headerStack {
					hierarchy = append(hierarchy, h.text)
				}

				currentSection = &section{
					headerLevel: level,
					headerText:  text,
					startLine:   lineNum,
					content:     make([]string, 0),
					hierarchy:   hierarchy,
				}

				// Don't add the header line itself to content
				continue
			}
		}

		// Add line to current section
		if currentSection != nil {
			currentSection.content = append(currentSection.content, line)
		} else {
			// Content before first header - create an implicit section
			currentSection = &section{
				headerLevel: 0,
				startLine:   1,
				content:     []string{line},
				hierarchy:   []string{},
			}
		}
	}

	// Save final section
	if currentSection != nil {
		currentSection.endLine = len(lines)
		sections = append(sections, *currentSection)
	}

	return sections
}

// headerInfo tracks header hierarchy.
type headerInfo struct {
	level int
	text  string
}

// updateHeaderStack maintains the header hierarchy stack.
func (c *MarkdownChunker) updateHeaderStack(stack []headerInfo, level int, text string) []headerInfo {
	// Remove headers at same or deeper level
	newStack := make([]headerInfo, 0, level)
	for _, h := range stack {
		if h.level < level {
			newStack = append(newStack, h)
		}
	}

	// Add current header
	newStack = append(newStack, headerInfo{level: level, text: text})

	return newStack
}

// buildChunks creates chunks from sections.
func (c *MarkdownChunker) buildChunks(sections []section, metadata Metadata) []Chunk {
	var chunks []Chunk
	chunkIndex := 0

	for _, sec := range sections {
		// Build chunk content
		var content strings.Builder

		// Add header hierarchy if enabled
		if c.config.IncludeHierarchy && len(sec.hierarchy) > 0 {
			// Add parent headers for context
			for i, h := range sec.hierarchy {
				level := i + 1
				if sec.headerLevel > 0 && i == len(sec.hierarchy)-1 {
					// This is the current section's header
					level = sec.headerLevel
				}
				content.WriteString(strings.Repeat("#", level))
				content.WriteString(" ")
				content.WriteString(h)
				content.WriteString("\n")
			}
			if len(sec.hierarchy) > 0 {
				content.WriteString("\n")
			}
		} else if sec.headerLevel > 0 {
			// Just add the section header
			content.WriteString(strings.Repeat("#", sec.headerLevel))
			content.WriteString(" ")
			content.WriteString(sec.headerText)
			content.WriteString("\n\n")
		}

		// Add section content
		sectionContent := strings.TrimSpace(strings.Join(sec.content, "\n"))
		if sectionContent != "" {
			content.WriteString(sectionContent)
		}

		contentStr := strings.TrimSpace(content.String())
		if contentStr == "" {
			continue
		}

		tokens := c.tokenizer.Count(contentStr)

		// If section exceeds max tokens, split it
		if tokens > c.config.MaxTokens {
			subChunks := c.splitLargeSection(sec, metadata)
			chunks = append(chunks, subChunks...)
			continue
		}

		// Check minimum size
		if tokens < c.config.MinChunkSize {
			// Try to merge with previous chunk if possible
			if len(chunks) > 0 {
				lastChunk := &chunks[len(chunks)-1]
				combinedContent := lastChunk.Content + "\n\n" + contentStr
				combinedTokens := c.tokenizer.Count(combinedContent)

				if combinedTokens <= c.config.MaxTokens {
					lastChunk.Content = combinedContent
					lastChunk.Tokens = combinedTokens
					lastChunk.Metadata.EndLine = sec.endLine
					continue
				}
			}

			// If we can't merge and it's too small, skip unless it's the only content
			if len(chunks) > 0 && tokens < c.config.MinChunkSize/2 {
				continue
			}
		}

		// Create chunk metadata
		chunkMeta := metadata.Clone()
		chunkMeta.StartLine = sec.startLine
		chunkMeta.EndLine = sec.endLine

		// Add markdown-specific metadata
		if chunkMeta.Context == nil {
			chunkMeta.Context = make(map[string]string)
		}
		if sec.headerLevel > 0 {
			chunkMeta.Context["header_level"] = fmt.Sprintf("%d", sec.headerLevel)
			chunkMeta.Context["header_text"] = sec.headerText
		}
		if len(sec.hierarchy) > 0 {
			chunkMeta.Context["hierarchy"] = strings.Join(sec.hierarchy, " > ")
		}
		chunkMeta.Context["type"] = "markdown_section"

		// Add hierarchy to tags
		if chunkMeta.Tags == nil {
			chunkMeta.Tags = make([]string, 0, len(sec.hierarchy))
		}
		chunkMeta.Tags = append(chunkMeta.Tags, sec.hierarchy...)

		chunk := Chunk{
			Content:  contentStr,
			Metadata: chunkMeta,
			Tokens:   tokens,
			Index:    chunkIndex,
		}
		chunkIndex++

		chunks = append(chunks, chunk)
	}

	return chunks
}

// splitLargeSection splits a section that exceeds max tokens.
func (c *MarkdownChunker) splitLargeSection(sec section, metadata Metadata) []Chunk {
	var chunks []Chunk
	chunkIndex := 0

	// Try to split by subsections (lists, code blocks, paragraphs)
	blocks := c.splitIntoBlocks(sec.content)

	var currentContent strings.Builder
	var currentLines []int
	startLine := sec.startLine

	// Add header prefix
	var headerPrefix string
	if c.config.IncludeHierarchy && len(sec.hierarchy) > 0 {
		var hb strings.Builder
		for i, h := range sec.hierarchy {
			level := i + 1
			if sec.headerLevel > 0 && i == len(sec.hierarchy)-1 {
				level = sec.headerLevel
			}
			hb.WriteString(strings.Repeat("#", level))
			hb.WriteString(" ")
			hb.WriteString(h)
			hb.WriteString("\n")
		}
		hb.WriteString("\n")
		headerPrefix = hb.String()
	} else if sec.headerLevel > 0 {
		headerPrefix = strings.Repeat("#", sec.headerLevel) + " " + sec.headerText + "\n\n"
	}

	for _, block := range blocks {
		blockContent := strings.TrimSpace(block.content)
		if blockContent == "" {
			continue
		}

		blockTokens := c.tokenizer.Count(headerPrefix + blockContent)
		currentTokens := c.tokenizer.Count(headerPrefix + currentContent.String())

		// If adding this block exceeds target, create a chunk
		if currentContent.Len() > 0 && currentTokens+blockTokens > c.config.TargetTokens {
			content := headerPrefix + strings.TrimSpace(currentContent.String())
			tokens := c.tokenizer.Count(content)

			if tokens >= c.config.MinChunkSize {
				chunkMeta := metadata.Clone()
				chunkMeta.StartLine = startLine
				chunkMeta.EndLine = startLine + len(currentLines) - 1

				if chunkMeta.Context == nil {
					chunkMeta.Context = make(map[string]string)
				}
				if sec.headerLevel > 0 {
					chunkMeta.Context["header_level"] = fmt.Sprintf("%d", sec.headerLevel)
					chunkMeta.Context["header_text"] = sec.headerText
				}
				if len(sec.hierarchy) > 0 {
					chunkMeta.Context["hierarchy"] = strings.Join(sec.hierarchy, " > ")
				}
				chunkMeta.Context["type"] = "markdown_section_split"

				chunks = append(chunks, Chunk{
					Content:  content,
					Metadata: chunkMeta,
					Tokens:   tokens,
					Index:    chunkIndex,
				})
				chunkIndex++
			}

			// Start new chunk
			currentContent.Reset()
			currentLines = nil
			startLine = block.startLine
		}

		// Add block to current chunk
		if currentContent.Len() > 0 {
			currentContent.WriteString("\n\n")
		}
		currentContent.WriteString(blockContent)
		currentLines = append(currentLines, block.startLine)
	}

	// Add final chunk
	if currentContent.Len() > 0 {
		content := headerPrefix + strings.TrimSpace(currentContent.String())
		tokens := c.tokenizer.Count(content)

		if tokens >= c.config.MinChunkSize {
			chunkMeta := metadata.Clone()
			chunkMeta.StartLine = startLine
			chunkMeta.EndLine = sec.endLine

			if chunkMeta.Context == nil {
				chunkMeta.Context = make(map[string]string)
			}
			if sec.headerLevel > 0 {
				chunkMeta.Context["header_level"] = fmt.Sprintf("%d", sec.headerLevel)
				chunkMeta.Context["header_text"] = sec.headerText
			}
			if len(sec.hierarchy) > 0 {
				chunkMeta.Context["hierarchy"] = strings.Join(sec.hierarchy, " > ")
			}
			chunkMeta.Context["type"] = "markdown_section_split"

			chunks = append(chunks, Chunk{
				Content:  content,
				Metadata: chunkMeta,
				Tokens:   tokens,
				Index:    chunkIndex,
			})
		}
	}

	return chunks
}

// block represents a logical block within a section.
type block struct {
	content   string
	startLine int
	blockType string // "code", "list", "paragraph"
}

// splitIntoBlocks splits section content into logical blocks.
func (c *MarkdownChunker) splitIntoBlocks(lines []string) []block {
	var blocks []block
	var currentBlock *block
	inCodeBlock := false
	inList := false

	for i, line := range lines {
		lineNum := i + 1

		// Check for code block
		if c.codeBlockPattern.MatchString(line) {
			if !inCodeBlock {
				// Start of code block
				if currentBlock != nil && currentBlock.blockType != "code" {
					// Save previous block
					blocks = append(blocks, *currentBlock)
					currentBlock = nil
				}
				inCodeBlock = true
				if currentBlock == nil {
					currentBlock = &block{
						content:   line,
						startLine: lineNum,
						blockType: "code",
					}
				} else {
					currentBlock.content += "\n" + line
				}
			} else {
				// End of code block
				currentBlock.content += "\n" + line
				blocks = append(blocks, *currentBlock)
				currentBlock = nil
				inCodeBlock = false
			}
			continue
		}

		// Inside code block
		if inCodeBlock {
			currentBlock.content += "\n" + line
			continue
		}

		// Check for list item
		isListItem := c.listItemPattern.MatchString(line)

		if isListItem {
			if !inList || currentBlock == nil {
				// Start new list block
				if currentBlock != nil {
					blocks = append(blocks, *currentBlock)
				}
				currentBlock = &block{
					content:   line,
					startLine: lineNum,
					blockType: "list",
				}
				inList = true
			} else {
				// Continue list
				currentBlock.content += "\n" + line
			}
			continue
		}

		// Empty line
		if strings.TrimSpace(line) == "" {
			if currentBlock != nil && inList {
				// End list on empty line
				blocks = append(blocks, *currentBlock)
				currentBlock = nil
				inList = false
			}
			// Skip empty lines between blocks
			continue
		}

		// Regular paragraph
		if inList {
			// End list, start paragraph
			if currentBlock != nil {
				blocks = append(blocks, *currentBlock)
			}
			inList = false
			currentBlock = &block{
				content:   line,
				startLine: lineNum,
				blockType: "paragraph",
			}
		} else if currentBlock == nil || currentBlock.blockType != "paragraph" {
			// Start new paragraph
			if currentBlock != nil {
				blocks = append(blocks, *currentBlock)
			}
			currentBlock = &block{
				content:   line,
				startLine: lineNum,
				blockType: "paragraph",
			}
		} else {
			// Continue paragraph
			currentBlock.content += "\n" + line
		}
	}

	// Add final block
	if currentBlock != nil {
		blocks = append(blocks, *currentBlock)
	}

	return blocks
}
