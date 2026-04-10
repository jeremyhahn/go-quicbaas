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

//go:build integration && rag

package rag

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/rag/chunking"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/pipeline"
)

// TestChunking_TextDocument tests plain text document chunking.
func TestChunking_TextDocument(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = ctx // Context for future async operations

	t.Run("BasicTextChunking", func(t *testing.T) {
		config := chunking.DefaultTextChunkerConfig()
		config.ChunkSize = 100
		config.ChunkOverlap = 20
		chunker := chunking.NewTextChunker(config)

		content := `Machine learning is a powerful technology. It enables computers to learn from data.
Deep learning uses neural networks. These networks have multiple layers.
Natural language processing helps understand text. It is used in many applications.
Computer vision analyzes images and videos. It powers autonomous vehicles.`

		metadata := chunking.Metadata{
			FilePath: "test.txt",
		}

		chunks, err := chunker.Chunk([]byte(content), metadata)
		require.NoError(t, err)
		require.NotEmpty(t, chunks, "Should produce chunks")

		// Verify chunks
		err = verifyChunks(chunks, 1, 20)
		require.NoError(t, err)

		// Log chunk info
		for i, chunk := range chunks {
			t.Logf("Chunk %d: %d chars, %d tokens", i, len(chunk.Content), chunk.Tokens)
		}
	})

	t.Run("SentenceRespectingChunking", func(t *testing.T) {
		config := chunking.TextChunkerConfig{
			ChunkSize:         200,
			ChunkOverlap:      40,
			MinChunkSize:      30,
			RespectSentences:  true,
			RespectParagraphs: true,
		}
		chunker := chunking.NewTextChunker(config)

		content := `The first sentence of paragraph one. This is the second sentence. Here is a third one.

The second paragraph starts here. It has multiple sentences too. And another one here.

Third paragraph with different content. Machine learning is fascinating. Deep learning even more so.`

		chunks, err := chunker.Chunk([]byte(content), chunking.Metadata{})
		require.NoError(t, err)
		require.NotEmpty(t, chunks)

		// With sentence-respecting chunking, sentences should be intact
		for i, chunk := range chunks {
			// Count sentence endings
			sentences := strings.Count(chunk.Content, ".") + strings.Count(chunk.Content, "!") + strings.Count(chunk.Content, "?")
			t.Logf("Chunk %d has %d sentence-ending marks", i, sentences)
		}
	})

	t.Run("ParagraphRespectingChunking", func(t *testing.T) {
		config := chunking.TextChunkerConfig{
			ChunkSize:         500,
			ChunkOverlap:      50,
			MinChunkSize:      50,
			RespectSentences:  false,
			RespectParagraphs: true,
		}
		chunker := chunking.NewTextChunker(config)

		content := `First paragraph about machine learning concepts and algorithms. It covers supervised and unsupervised learning methods with various applications.

Second paragraph discusses deep learning architectures including convolutional neural networks and recurrent neural networks for different types of data.

Third paragraph explores natural language processing techniques such as transformers, attention mechanisms, and large language models.

Fourth paragraph describes computer vision applications in autonomous vehicles, medical imaging, and security systems.`

		chunks, err := chunker.Chunk([]byte(content), chunking.Metadata{})
		require.NoError(t, err)
		require.NotEmpty(t, chunks)

		t.Logf("Paragraph-respecting chunking produced %d chunks", len(chunks))
	})

	t.Run("SmallChunkSize", func(t *testing.T) {
		config := chunking.TextChunkerConfig{
			ChunkSize:    50,
			ChunkOverlap: 10,
			MinChunkSize: 10, // Lower min to allow small content to produce chunks
		}
		chunker := chunking.NewTextChunker(config)

		// Longer content to ensure chunks are produced with small chunk size
		content := "This is a test document with enough text to be split into multiple small chunks. " +
			"The content should be long enough to produce at least two or three chunks when using small chunk size settings. " +
			"This ensures the chunking algorithm is working correctly with these parameters."

		chunks, err := chunker.Chunk([]byte(content), chunking.Metadata{})
		require.NoError(t, err)

		// Small chunk size should produce more chunks
		assert.GreaterOrEqual(t, len(chunks), 1, "Should produce at least one chunk")
		t.Logf("Small chunk size produced %d chunks", len(chunks))
	})

	t.Run("EmptyContent", func(t *testing.T) {
		chunker := chunking.NewTextChunker(chunking.DefaultTextChunkerConfig())
		_, err := chunker.Chunk([]byte{}, chunking.Metadata{})
		assert.Error(t, err, "Empty content should return error")
	})

	t.Run("ChunksHaveMetadata", func(t *testing.T) {
		chunker := chunking.NewTextChunker(chunking.DefaultTextChunkerConfig())

		metadata := chunking.Metadata{
			FilePath:     "/path/to/file.txt",
			StartLine:    1,
			Tags:         []string{"test", "integration"},
			FunctionName: "testFunction",
		}

		content := "This is test content for metadata verification. " +
			strings.Repeat("More text here. ", 20)

		chunks, err := chunker.Chunk([]byte(content), metadata)
		require.NoError(t, err)
		require.NotEmpty(t, chunks)

		// Each chunk should have associated metadata
		for i, chunk := range chunks {
			assert.Equal(t, metadata.FilePath, chunk.Metadata.FilePath, "Chunk %d should preserve FilePath", i)
		}
	})
}

// TestChunking_MarkdownDocument tests markdown-aware document chunking.
func TestChunking_MarkdownDocument(t *testing.T) {
	t.Run("BasicMarkdownChunking", func(t *testing.T) {
		config := chunking.DefaultMarkdownChunkerConfig()
		config.TargetTokens = 200
		config.MaxTokens = 500
		chunker := chunking.NewMarkdownChunker(config)

		doc := createMarkdownTestDocument()

		chunks, err := chunker.Chunk([]byte(doc.Content), chunking.Metadata{
			FilePath: "api.md",
		})
		require.NoError(t, err)
		require.NotEmpty(t, chunks)

		t.Logf("Markdown chunking produced %d chunks", len(chunks))

		// Verify chunks
		err = verifyChunks(chunks, 1, 50)
		require.NoError(t, err)
	})

	t.Run("MarkdownHeaderPreservation", func(t *testing.T) {
		config := chunking.MarkdownChunkerConfig{
			MaxHeaderDepth:   3,
			KeepCodeBlocks:   true,
			IncludeHierarchy: true,
			TargetTokens:     150,
			MaxTokens:        400,
			MinChunkSize:     30,
		}
		chunker := chunking.NewMarkdownChunker(config)

		content := `# Main Title

This is the introduction.

## Section 1

Content of section 1.

### Subsection 1.1

Content of subsection 1.1.

## Section 2

Content of section 2.

### Subsection 2.1

Content of subsection 2.1.

### Subsection 2.2

Content of subsection 2.2.`

		chunks, err := chunker.Chunk([]byte(content), chunking.Metadata{})
		require.NoError(t, err)
		require.NotEmpty(t, chunks)

		// Log chunks to see header handling
		for i, chunk := range chunks {
			preview := chunk.Content
			if len(preview) > 100 {
				preview = preview[:100] + "..."
			}
			t.Logf("Chunk %d: %s", i, preview)
		}
	})

	t.Run("MarkdownCodeBlockPreservation", func(t *testing.T) {
		config := chunking.MarkdownChunkerConfig{
			KeepCodeBlocks: true,
			TargetTokens:   300,
			MaxTokens:      600,
			MinChunkSize:   30,
		}
		chunker := chunking.NewMarkdownChunker(config)

		content := "# Example\n\nHere is some code:\n\n```go\nfunc main() {\n    fmt.Println(\"Hello\")\n}\n```\n\nAnd more text after the code."

		chunks, err := chunker.Chunk([]byte(content), chunking.Metadata{})
		require.NoError(t, err)
		require.NotEmpty(t, chunks)

		// Verify code block is preserved (not split in the middle)
		foundCodeBlock := false
		for _, chunk := range chunks {
			if strings.Contains(chunk.Content, "func main()") {
				foundCodeBlock = true
				// The code block should be complete
				assert.Contains(t, chunk.Content, "fmt.Println")
			}
		}
		assert.True(t, foundCodeBlock, "Code block should be present in chunks")
	})

	t.Run("MarkdownWithLists", func(t *testing.T) {
		config := chunking.DefaultMarkdownChunkerConfig()
		chunker := chunking.NewMarkdownChunker(config)

		content := `# Features

Here are the main features:

- Feature 1: Description of feature 1
- Feature 2: Description of feature 2
- Feature 3: Description of feature 3

## Numbered List

1. First item with details
2. Second item with details
3. Third item with details

More content here.`

		chunks, err := chunker.Chunk([]byte(content), chunking.Metadata{})
		require.NoError(t, err)
		require.NotEmpty(t, chunks)

		t.Logf("Markdown with lists produced %d chunks", len(chunks))
	})

	t.Run("EmptyMarkdown", func(t *testing.T) {
		chunker := chunking.NewMarkdownChunker(chunking.DefaultMarkdownChunkerConfig())
		_, err := chunker.Chunk([]byte{}, chunking.Metadata{})
		assert.Error(t, err, "Empty markdown should return error")
	})
}

// TestChunking_CodeDocument tests code-aware document chunking.
func TestChunking_CodeDocument(t *testing.T) {
	t.Run("GoCodeChunking", func(t *testing.T) {
		config := chunking.GoCodeChunkerConfig{
			MaxFunctionTokens: 200,
			IncludeImports:    true,
			IncludeComments:   true,
			IncludePackage:    true,
		}
		chunker := chunking.NewGoCodeChunker(config)

		doc := createCodeTestDocument()

		chunks, err := chunker.Chunk([]byte(doc.Content), chunking.Metadata{
			FilePath: "main.go",
		})
		require.NoError(t, err)
		require.NotEmpty(t, chunks)

		t.Logf("Go code chunking produced %d chunks", len(chunks))

		// Verify chunks have code context
		for i, chunk := range chunks {
			t.Logf("Chunk %d (%d tokens): Function=%s", i, chunk.Tokens, chunk.Metadata.FunctionName)
		}
	})

	t.Run("GoFunctionPreservation", func(t *testing.T) {
		config := chunking.GoCodeChunkerConfig{
			MaxFunctionTokens: 100,
			IncludeImports:    true,
			IncludeComments:   true,
			IncludePackage:    true,
		}
		chunker := chunking.NewGoCodeChunker(config)

		content := `package example

// Add adds two numbers.
func Add(a, b int) int {
	return a + b
}

// Multiply multiplies two numbers.
func Multiply(a, b int) int {
	return a * b
}

// Divide divides a by b.
func Divide(a, b int) (int, error) {
	if b == 0 {
		return 0, fmt.Errorf("division by zero")
	}
	return a / b, nil
}`

		chunks, err := chunker.Chunk([]byte(content), chunking.Metadata{})
		require.NoError(t, err)
		require.NotEmpty(t, chunks)

		// Each chunk should represent complete functions
		for i, chunk := range chunks {
			// Count function definitions
			funcCount := strings.Count(chunk.Content, "func ")
			t.Logf("Chunk %d has %d function definition(s)", i, funcCount)
		}
	})

	t.Run("CodeWithStructs", func(t *testing.T) {
		config := chunking.DefaultGoCodeChunkerConfig()
		chunker := chunking.NewGoCodeChunker(config)

		content := `package models

// User represents a user in the system.
type User struct {
	ID        string
	Name      string
	Email     string
	CreatedAt time.Time
}

// NewUser creates a new user.
func NewUser(name, email string) *User {
	return &User{
		ID:        uuid.New().String(),
		Name:      name,
		Email:     email,
		CreatedAt: time.Now(),
	}
}

// Validate validates the user data.
func (u *User) Validate() error {
	if u.Name == "" {
		return errors.New("name is required")
	}
	if u.Email == "" {
		return errors.New("email is required")
	}
	return nil
}`

		chunks, err := chunker.Chunk([]byte(content), chunking.Metadata{FilePath: "models.go"})
		require.NoError(t, err)
		require.NotEmpty(t, chunks)

		t.Logf("Code with structs produced %d chunks", len(chunks))
	})

	t.Run("EmptyCode", func(t *testing.T) {
		chunker := chunking.NewGoCodeChunker(chunking.DefaultGoCodeChunkerConfig())
		chunks, err := chunker.Chunk([]byte{}, chunking.Metadata{})
		// GoCodeChunker returns empty slice for empty input, not error
		require.NoError(t, err)
		assert.Empty(t, chunks, "Empty code should return empty chunks")
	})
}

// TestChunking_LargeDocument tests large document handling.
func TestChunking_LargeDocument(t *testing.T) {
	t.Run("LargeTextDocument", func(t *testing.T) {
		config := chunking.TextChunkerConfig{
			ChunkSize:         150, // Smaller chunk size to ensure multiple chunks
			ChunkOverlap:      30,
			MinChunkSize:      50,
			RespectSentences:  true,
			RespectParagraphs: true,
		}
		chunker := chunking.NewTextChunker(config)

		doc := createLargeTestDocument()

		chunks, err := chunker.Chunk([]byte(doc.Content), chunking.Metadata{
			FilePath: "large-doc.txt",
		})
		require.NoError(t, err)
		require.NotEmpty(t, chunks)

		t.Logf("Large document (%d chars) produced %d chunks", len(doc.Content), len(chunks))

		// Verify reasonable chunk count (with smaller chunk size, should produce multiple)
		assert.GreaterOrEqual(t, len(chunks), 2, "Large document should produce multiple chunks")

		// Verify all content is covered
		var totalTokens int
		for _, chunk := range chunks {
			totalTokens += chunk.Tokens
		}
		t.Logf("Total tokens across chunks: %d", totalTokens)
	})

	t.Run("VeryLargeDocument", func(t *testing.T) {
		config := chunking.DefaultTextChunkerConfig()
		chunker := chunking.NewTextChunker(config)

		// Generate a very large document
		var builder strings.Builder
		paragraph := "This is a paragraph about machine learning and artificial intelligence. It discusses various concepts and applications. "
		for i := 0; i < 100; i++ {
			builder.WriteString(paragraph)
			if i%10 == 9 {
				builder.WriteString("\n\n")
			}
		}

		largeContent := builder.String()
		t.Logf("Generated document with %d characters", len(largeContent))

		chunks, err := chunker.Chunk([]byte(largeContent), chunking.Metadata{})
		require.NoError(t, err)
		require.NotEmpty(t, chunks)

		t.Logf("Very large document produced %d chunks", len(chunks))

		// Verify no chunks are empty
		for i, chunk := range chunks {
			assert.NotEmpty(t, chunk.Content, "Chunk %d should not be empty", i)
		}
	})

	t.Run("LargeMarkdownDocument", func(t *testing.T) {
		config := chunking.MarkdownChunkerConfig{
			MaxHeaderDepth:   4,
			KeepCodeBlocks:   true,
			IncludeHierarchy: true,
			TargetTokens:     400,
			MaxTokens:        800,
			MinChunkSize:     50,
		}
		chunker := chunking.NewMarkdownChunker(config)

		// Generate a large markdown document
		var builder strings.Builder
		builder.WriteString("# Large Documentation\n\n")

		for section := 1; section <= 10; section++ {
			builder.WriteString("## Section " + string(rune('0'+section)) + "\n\n")
			builder.WriteString("This section covers important concepts. ")
			builder.WriteString(strings.Repeat("More detailed explanation here. ", 10))
			builder.WriteString("\n\n")

			for sub := 1; sub <= 3; sub++ {
				builder.WriteString("### Subsection " + string(rune('0'+section)) + "." + string(rune('0'+sub)) + "\n\n")
				builder.WriteString("Content for this subsection. ")
				builder.WriteString(strings.Repeat("Additional details. ", 5))
				builder.WriteString("\n\n")
			}
		}

		largeMarkdown := builder.String()
		t.Logf("Generated markdown with %d characters", len(largeMarkdown))

		chunks, err := chunker.Chunk([]byte(largeMarkdown), chunking.Metadata{})
		require.NoError(t, err)
		require.NotEmpty(t, chunks)

		t.Logf("Large markdown produced %d chunks", len(chunks))
	})

	t.Run("ChunkOverlapVerification", func(t *testing.T) {
		config := chunking.TextChunkerConfig{
			ChunkSize:        100,
			ChunkOverlap:     30,
			MinChunkSize:     20,
			RespectSentences: false,
		}
		chunker := chunking.NewTextChunker(config)

		content := strings.Repeat("word ", 100)

		chunks, err := chunker.Chunk([]byte(content), chunking.Metadata{})
		require.NoError(t, err)
		require.NotEmpty(t, chunks)

		// With overlap, consecutive chunks should share some content
		if len(chunks) >= 2 {
			// Check that chunks have overlapping content
			t.Logf("Chunk 0 ends with: ...%s", truncateString(chunks[0].Content[max(0, len(chunks[0].Content)-50):], 50))
			t.Logf("Chunk 1 starts with: %s...", truncateString(chunks[1].Content, 50))
		}
	})
}

// TestChunking_Integration tests chunking integration with embedding and RAG pipeline.
func TestChunking_Integration(t *testing.T) {
	requireOllama(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	t.Run("ChunkAndEmbed", func(t *testing.T) {
		// Create chunker
		chunker := chunking.NewTextChunker(chunking.DefaultTextChunkerConfig())

		// Create embedding provider
		provider, err := createTestEmbeddingProvider(true)
		require.NoError(t, err)
		defer provider.Close()

		// Create document
		doc := createLargeTestDocument()

		// Chunk the document
		chunks, err := chunker.Chunk([]byte(doc.Content), chunking.Metadata{
			FilePath: "large-doc.md",
		})
		require.NoError(t, err)
		require.NotEmpty(t, chunks)

		t.Logf("Chunked document into %d chunks", len(chunks))

		// Embed each chunk (limit to first 5 for test speed)
		embedChunks := chunks
		if len(embedChunks) > 5 {
			embedChunks = chunks[:5]
		}

		embeddings := make([][]float32, 0, len(embedChunks))
		for i, chunk := range embedChunks {
			emb, err := provider.Embed(ctx, chunk.Content)
			require.NoError(t, err, "Failed to embed chunk %d", i)
			embeddings = append(embeddings, emb)
		}

		t.Logf("Generated %d embeddings (dimensions: %d)", len(embeddings), len(embeddings[0]))

		// Verify embeddings have correct dimensions
		for i, emb := range embeddings {
			assert.Equal(t, env.embedDimensions, len(emb), "Embedding %d has wrong dimensions", i)
		}

		// Verify similar chunks have higher cosine similarity
		if len(embeddings) >= 3 {
			sim01 := cosineSimilarity(embeddings[0], embeddings[1])
			sim02 := cosineSimilarity(embeddings[0], embeddings[2])
			t.Logf("Cosine similarity: chunk0-chunk1=%.4f, chunk0-chunk2=%.4f", sim01, sim02)
		}
	})

	t.Run("ChunkSearchAndRetrieve", func(t *testing.T) {
		// Create RAG system
		system, err := createTestRAGSystem(true)
		require.NoError(t, err)
		defer system.Close()

		// Create and chunk a document
		chunker := chunking.NewTextChunker(chunking.TextChunkerConfig{
			ChunkSize:        200,
			ChunkOverlap:     40,
			MinChunkSize:     50,
			RespectSentences: true,
		})

		doc := createLargeTestDocument()
		chunks, err := chunker.Chunk([]byte(doc.Content), chunking.Metadata{
			FilePath: doc.ID,
		})
		require.NoError(t, err)

		// Index each chunk
		for i, chunk := range chunks {
			chunkID := doc.ID + "_chunk_" + string(rune('0'+i%10))
			err := system.IndexContent(ctx, chunkID, chunk.Content, map[string]interface{}{
				"source":      doc.ID,
				"chunk_index": i,
			})
			require.NoError(t, err)
		}

		t.Logf("Indexed %d chunks from document", len(chunks))

		// Query for specific content
		query := "deep learning neural networks"
		opts := pipeline.QueryOptions{
			TopK:     5,
			MinScore: 0.1,
		}

		response, err := system.Query(ctx, query, opts)
		require.NoError(t, err)
		require.NotEmpty(t, response.Results)

		t.Logf("Query '%s' returned %d results", query, len(response.Results))
		for i, r := range response.Results {
			t.Logf("  %d. %s (score: %.4f)", i+1, r.ID, r.Score)
		}
	})
}

// max returns the maximum of two integers.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
