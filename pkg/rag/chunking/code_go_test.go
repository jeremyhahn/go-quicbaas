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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoCodeChunker_Name(t *testing.T) {
	chunker := NewGoCodeChunker(DefaultGoCodeChunkerConfig())
	assert.Equal(t, "go-code-ast", chunker.Name())
}

func TestGoCodeChunker_EmptyContent(t *testing.T) {
	chunker := NewGoCodeChunker(DefaultGoCodeChunkerConfig())
	chunks, err := chunker.Chunk([]byte{}, Metadata{})

	require.NoError(t, err)
	assert.Empty(t, chunks)
}

func TestGoCodeChunker_SingleFunction(t *testing.T) {
	chunker := NewGoCodeChunker(GoCodeChunkerConfig{
		IncludeImports:  true,
		IncludeComments: true,
		IncludePackage:  true,
	})

	content := []byte(`package main

import "fmt"

// Hello greets a person
func Hello(name string) string {
	return fmt.Sprintf("Hello, %s", name)
}`)

	chunks, err := chunker.Chunk(content, Metadata{FilePath: "test.go"})

	require.NoError(t, err)
	assert.Equal(t, 1, len(chunks))

	chunk := chunks[0]
	assert.Equal(t, "Hello", chunk.Metadata.FunctionName)
	// Line 5 is the doc comment, which is the start when IncludeComments is true
	assert.Equal(t, 5, chunk.Metadata.StartLine)
	assert.Equal(t, 8, chunk.Metadata.EndLine)
	assert.Contains(t, chunk.Content, "package main")
	assert.Contains(t, chunk.Content, "import \"fmt\"")
	assert.Contains(t, chunk.Content, "// Hello greets a person")
	assert.Contains(t, chunk.Content, "func Hello(name string) string")
	assert.Greater(t, chunk.Tokens, 0)
	assert.Equal(t, 0, chunk.Index)
	assert.Equal(t, "function", chunk.Metadata.Context["type"])
}

func TestGoCodeChunker_MultipleFunctions(t *testing.T) {
	chunker := NewGoCodeChunker(DefaultGoCodeChunkerConfig())

	content := []byte(`package main

import (
	"fmt"
	"strings"
)

// Hello greets a person
func Hello(name string) string {
	return fmt.Sprintf("Hello, %s", strings.Title(name))
}

// Goodbye says goodbye
func Goodbye(name string) string {
	return fmt.Sprintf("Goodbye, %s", name)
}`)

	chunks, err := chunker.Chunk(content, Metadata{})

	require.NoError(t, err)
	assert.Equal(t, 2, len(chunks))

	// Check first function (with doc comment, starts at line 8)
	assert.Equal(t, "Hello", chunks[0].Metadata.FunctionName)
	assert.Equal(t, 8, chunks[0].Metadata.StartLine) // Doc comment line
	assert.Contains(t, chunks[0].Content, "func Hello")
	assert.Contains(t, chunks[0].Content, "// Hello greets a person")
	assert.Equal(t, 0, chunks[0].Index)

	// Check second function (with doc comment, starts at line 13)
	assert.Equal(t, "Goodbye", chunks[1].Metadata.FunctionName)
	assert.Equal(t, 13, chunks[1].Metadata.StartLine) // Doc comment line
	assert.Contains(t, chunks[1].Content, "func Goodbye")
	assert.Contains(t, chunks[1].Content, "// Goodbye says goodbye")
	assert.Equal(t, 1, chunks[1].Index)
}

func TestGoCodeChunker_Methods(t *testing.T) {
	chunker := NewGoCodeChunker(DefaultGoCodeChunkerConfig())

	content := []byte(`package main

// Server represents a server
type Server struct {
	addr string
}

// Start starts the server
func (s *Server) Start() error {
	return nil
}

// Stop stops the server
func (s Server) Stop() error {
	return nil
}`)

	chunks, err := chunker.Chunk(content, Metadata{})

	require.NoError(t, err)
	assert.Equal(t, 3, len(chunks)) // Type + 2 methods

	// Check type definition
	assert.Equal(t, "Server", chunks[0].Metadata.ClassName)
	assert.Contains(t, chunks[0].Content, "type Server struct")
	assert.Equal(t, "type_definition", chunks[0].Metadata.Context["type"])

	// Check pointer receiver method
	assert.Equal(t, "Start", chunks[1].Metadata.FunctionName)
	assert.Equal(t, "Server", chunks[1].Metadata.ClassName)
	assert.Contains(t, chunks[1].Content, "func (s *Server) Start()")
	assert.Equal(t, "method", chunks[1].Metadata.Context["type"])

	// Check value receiver method
	assert.Equal(t, "Stop", chunks[2].Metadata.FunctionName)
	assert.Equal(t, "Server", chunks[2].Metadata.ClassName)
	assert.Contains(t, chunks[2].Content, "func (s Server) Stop()")
	assert.Equal(t, "method", chunks[2].Metadata.Context["type"])
}

func TestGoCodeChunker_TypeDefinitions(t *testing.T) {
	chunker := NewGoCodeChunker(DefaultGoCodeChunkerConfig())

	content := []byte(`package main

// Status represents operation status
type Status int

// Handler is a function type
type Handler func(string) error

// Config holds configuration
type Config struct {
	Host string
	Port int
}`)

	chunks, err := chunker.Chunk(content, Metadata{})

	require.NoError(t, err)
	assert.Equal(t, 3, len(chunks))

	// Check type alias
	assert.Equal(t, "Status", chunks[0].Metadata.ClassName)
	assert.Contains(t, chunks[0].Content, "type Status int")

	// Check function type
	assert.Equal(t, "Handler", chunks[1].Metadata.ClassName)
	assert.Contains(t, chunks[1].Content, "type Handler func(string) error")

	// Check struct type
	assert.Equal(t, "Config", chunks[2].Metadata.ClassName)
	assert.Contains(t, chunks[2].Content, "type Config struct")
}

func TestGoCodeChunker_WithoutImports(t *testing.T) {
	chunker := NewGoCodeChunker(GoCodeChunkerConfig{
		IncludeImports:  false,
		IncludeComments: true,
		IncludePackage:  true,
	})

	content := []byte(`package main

import "fmt"

func Hello() {
	fmt.Println("Hello")
}`)

	chunks, err := chunker.Chunk(content, Metadata{})

	require.NoError(t, err)
	assert.Equal(t, 1, len(chunks))
	assert.NotContains(t, chunks[0].Content, "import")
}

func TestGoCodeChunker_WithoutComments(t *testing.T) {
	chunker := NewGoCodeChunker(GoCodeChunkerConfig{
		IncludeImports:  true,
		IncludeComments: false,
		IncludePackage:  true,
	})

	content := []byte(`package main

// Hello greets a person
func Hello() {
}`)

	chunks, err := chunker.Chunk(content, Metadata{})

	require.NoError(t, err)
	assert.Equal(t, 1, len(chunks))
	// When IncludeComments is false, comments should not be in output
	assert.NotContains(t, chunks[0].Content, "// Hello greets a person")
	// But function should still be there
	assert.Contains(t, chunks[0].Content, "func Hello()")
}

func TestGoCodeChunker_WithoutPackage(t *testing.T) {
	chunker := NewGoCodeChunker(GoCodeChunkerConfig{
		IncludeImports:  true,
		IncludeComments: true,
		IncludePackage:  false,
	})

	content := []byte(`package main

func Hello() {
}`)

	chunks, err := chunker.Chunk(content, Metadata{})

	require.NoError(t, err)
	assert.Equal(t, 1, len(chunks))
	assert.NotContains(t, chunks[0].Content, "package main")
}

func TestGoCodeChunker_ImportUsageFiltering(t *testing.T) {
	chunker := NewGoCodeChunker(DefaultGoCodeChunkerConfig())

	content := []byte(`package main

import (
	"fmt"
	"strings"
	"os"
)

// Hello uses fmt and strings, but not os
func Hello(name string) string {
	return fmt.Sprintf("Hello, %s", strings.Title(name))
}

// ReadFile uses os, but not fmt or strings
func ReadFile(path string) error {
	_, err := os.Open(path)
	return err
}`)

	chunks, err := chunker.Chunk(content, Metadata{})

	require.NoError(t, err)
	assert.Equal(t, 2, len(chunks))

	// First function should only have fmt and strings
	assert.Contains(t, chunks[0].Content, "\"fmt\"")
	assert.Contains(t, chunks[0].Content, "\"strings\"")
	assert.NotContains(t, chunks[0].Content, "\"os\"")

	// Second function should only have os
	assert.Contains(t, chunks[1].Content, "\"os\"")
	assert.NotContains(t, chunks[1].Content, "\"fmt\"")
	assert.NotContains(t, chunks[1].Content, "\"strings\"")
}

func TestGoCodeChunker_ConstAndVarDeclarations(t *testing.T) {
	chunker := NewGoCodeChunker(DefaultGoCodeChunkerConfig())

	content := []byte(`package main

import "sync"

// Version is the application version
const Version = "1.0.0"

// Multiple constants
const (
	MaxRetries = 3
	Timeout    = 30
)

var singleton = "value"

// Important global variables
var (
	GlobalCache map[string]string
	GlobalLock  sync.Mutex
)

func Hello() {}`)

	chunks, err := chunker.Chunk(content, Metadata{})

	require.NoError(t, err)

	// Should include: const Version (has doc), const block (multiple), var block (multiple), function
	// Should NOT include: singleton var (no doc, single value)
	assert.Equal(t, 4, len(chunks))

	// Check that documented const is included
	foundVersionConst := false
	foundConstBlock := false
	foundVarBlock := false

	for _, chunk := range chunks {
		if chunk.Metadata.Context["type"] == "const_declaration" {
			// First const with doc at line 5
			if chunk.Metadata.StartLine == 5 {
				foundVersionConst = true
				assert.Contains(t, chunk.Content, "// Version is the application version")
			} else if chunk.Metadata.StartLine == 8 {
				// Second const block at line 8
				foundConstBlock = true
				assert.Contains(t, chunk.Content, "MaxRetries")
			}
		}
		if chunk.Metadata.Context["type"] == "var_declaration" {
			foundVarBlock = true
			assert.Contains(t, chunk.Content, "GlobalCache")
		}
	}

	assert.True(t, foundVersionConst, "Should include documented const")
	assert.True(t, foundConstBlock, "Should include const block")
	assert.True(t, foundVarBlock, "Should include var block")
}

func TestGoCodeChunker_LargeFunctionWarning(t *testing.T) {
	chunker := NewGoCodeChunker(GoCodeChunkerConfig{
		MaxFunctionTokens: 10, // Very low threshold for testing
		IncludeImports:    true,
		IncludeComments:   true,
		IncludePackage:    true,
	})

	content := []byte(`package main

func LargeFunction() {
	// This function has many lines to exceed token threshold
	x := 1
	y := 2
	z := 3
	a := 4
	b := 5
	c := 6
	d := 7
	e := 8
	f := 9
}`)

	chunks, err := chunker.Chunk(content, Metadata{})

	require.NoError(t, err)
	assert.Equal(t, 1, len(chunks))

	chunk := chunks[0]
	assert.Equal(t, "true", chunk.Metadata.Context["too_large"])
	assert.NotEmpty(t, chunk.Metadata.Context["token_count"])
}

func TestGoCodeChunker_SyntaxError(t *testing.T) {
	chunker := NewGoCodeChunker(DefaultGoCodeChunkerConfig())

	content := []byte(`package main

func Hello( {
	// Invalid syntax - missing parameter
}`)

	chunks, err := chunker.Chunk(content, Metadata{})

	assert.Error(t, err)
	assert.Nil(t, chunks)
	assert.Contains(t, err.Error(), "failed to parse Go source")
}

func TestGoCodeChunker_ComplexRealWorld(t *testing.T) {
	chunker := NewGoCodeChunker(DefaultGoCodeChunkerConfig())

	// Simulate a real Go file with various constructs
	content := []byte(`package server

import (
	"context"
	"fmt"
	"net/http"
	"sync"
)

// Config holds server configuration
type Config struct {
	Host string
	Port int
}

// Server represents an HTTP server
type Server struct {
	config Config
	mux    *http.ServeMux
	mu     sync.Mutex
}

// NewServer creates a new server instance
func NewServer(config Config) *Server {
	return &Server{
		config: config,
		mux:    http.NewServeMux(),
	}
}

// Start starts the HTTP server
func (s *Server) Start(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	return http.ListenAndServe(addr, s.mux)
}

// Stop gracefully stops the server
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Cleanup logic here
	return nil
}

// HandleFunc registers a handler function
func (s *Server) HandleFunc(pattern string, handler http.HandlerFunc) {
	s.mux.HandleFunc(pattern, handler)
}`)

	chunks, err := chunker.Chunk(content, Metadata{FilePath: "server.go"})

	require.NoError(t, err)
	// Should have: Config type, Server type, NewServer func, Start method, Stop method, HandleFunc method
	assert.Equal(t, 6, len(chunks))

	// Verify chunks are properly indexed
	for i, chunk := range chunks {
		assert.Equal(t, i, chunk.Index)
	}

	// Check that each chunk has proper metadata
	for _, chunk := range chunks {
		assert.Greater(t, chunk.Tokens, 0)
		assert.Greater(t, chunk.Metadata.StartLine, 0)
		assert.Greater(t, chunk.Metadata.EndLine, 0)
		assert.NotEmpty(t, chunk.Metadata.Context["type"])
	}

	// Verify Server methods have correct receiver type
	for _, chunk := range chunks {
		if chunk.Metadata.FunctionName == "Start" ||
			chunk.Metadata.FunctionName == "Stop" ||
			chunk.Metadata.FunctionName == "HandleFunc" {
			assert.Equal(t, "Server", chunk.Metadata.ClassName)
			assert.Equal(t, "method", chunk.Metadata.Context["type"])
		}
	}
}

func TestGoCodeChunker_InterfaceType(t *testing.T) {
	chunker := NewGoCodeChunker(DefaultGoCodeChunkerConfig())

	content := []byte(`package main

// Reader defines the read interface
type Reader interface {
	Read(p []byte) (n int, err error)
	Close() error
}`)

	chunks, err := chunker.Chunk(content, Metadata{})

	require.NoError(t, err)
	assert.Equal(t, 1, len(chunks))

	chunk := chunks[0]
	assert.Equal(t, "Reader", chunk.Metadata.ClassName)
	assert.Contains(t, chunk.Content, "type Reader interface")
	assert.Contains(t, chunk.Content, "Read(p []byte)")
}

func TestGoCodeChunker_MetadataPreservation(t *testing.T) {
	chunker := NewGoCodeChunker(DefaultGoCodeChunkerConfig())

	content := []byte(`package main

func Hello() {}
func World() {}`)

	originalMeta := Metadata{
		FilePath: "/path/to/file.go",
		Tags:     []string{"tag1", "tag2"},
		Context:  map[string]string{"key": "value"},
	}

	chunks, err := chunker.Chunk(content, originalMeta)

	require.NoError(t, err)
	assert.Equal(t, 2, len(chunks))

	// Verify original metadata is preserved and not mutated
	for _, chunk := range chunks {
		assert.Equal(t, originalMeta.FilePath, chunk.Metadata.FilePath)
		assert.Equal(t, originalMeta.Tags, chunk.Metadata.Tags)

		// Original context should be preserved (not the same reference)
		assert.Equal(t, "value", chunk.Metadata.Context["key"])
	}

	// Verify modifications don't affect original
	chunks[0].Metadata.Tags = append(chunks[0].Metadata.Tags, "new-tag")
	assert.Len(t, originalMeta.Tags, 2, "Original metadata should not be modified")
}

func TestGoCodeChunker_ImportWithAlias(t *testing.T) {
	chunker := NewGoCodeChunker(DefaultGoCodeChunkerConfig())

	content := []byte(`package main

import (
	f "fmt"
	. "strings"
)

func Hello() {
	f.Println(Title("hello"))
}`)

	chunks, err := chunker.Chunk(content, Metadata{})

	require.NoError(t, err)
	assert.Equal(t, 1, len(chunks))

	// Should include both imports - fmt is used via alias 'f', strings is dot import
	assert.Contains(t, chunks[0].Content, "import")
	assert.Contains(t, chunks[0].Content, "\"fmt\"")
	assert.Contains(t, chunks[0].Content, "\"strings\"") // Dot import should be included
}

func TestGoCodeChunker_NoDeclarations(t *testing.T) {
	chunker := NewGoCodeChunker(DefaultGoCodeChunkerConfig())

	content := []byte(`package main

// Just a package with imports but no declarations
import "fmt"
`)

	chunks, err := chunker.Chunk(content, Metadata{})

	require.NoError(t, err)
	assert.Empty(t, chunks, "Should have no chunks when there are no declarations")
}

func TestDefaultGoCodeChunkerConfig(t *testing.T) {
	config := DefaultGoCodeChunkerConfig()

	assert.Equal(t, 2000, config.MaxFunctionTokens)
	assert.True(t, config.IncludeImports)
	assert.True(t, config.IncludeComments)
	assert.True(t, config.IncludePackage)
}

func TestGoCodeChunker_LineNumbersWithoutComments(t *testing.T) {
	chunker := NewGoCodeChunker(GoCodeChunkerConfig{
		IncludeImports:  true,
		IncludeComments: false,
		IncludePackage:  true,
	})

	content := []byte(`package main

// This comment should not affect line numbers when IncludeComments is false
func Hello() {
}`)

	chunks, err := chunker.Chunk(content, Metadata{})

	require.NoError(t, err)
	assert.Equal(t, 1, len(chunks))

	// Without comments, start line should be function declaration (line 4)
	assert.Equal(t, 4, chunks[0].Metadata.StartLine)
}

func TestGoCodeChunker_EmptyFunction(t *testing.T) {
	chunker := NewGoCodeChunker(DefaultGoCodeChunkerConfig())

	content := []byte(`package main

func Empty() {}
`)

	chunks, err := chunker.Chunk(content, Metadata{})

	require.NoError(t, err)
	assert.Equal(t, 1, len(chunks))
	assert.Equal(t, "Empty", chunks[0].Metadata.FunctionName)
	assert.Contains(t, chunks[0].Content, "func Empty()")
}

func TestGoCodeChunker_MultipleTypesInSingleDecl(t *testing.T) {
	chunker := NewGoCodeChunker(DefaultGoCodeChunkerConfig())

	content := []byte(`package main

type (
	// First type
	TypeA int

	// Second type
	TypeB string
)`)

	chunks, err := chunker.Chunk(content, Metadata{})

	require.NoError(t, err)
	// Each type in a type block is extracted separately
	assert.Equal(t, 2, len(chunks))
	assert.Equal(t, "TypeA", chunks[0].Metadata.ClassName)
	assert.Equal(t, "TypeB", chunks[1].Metadata.ClassName)
}
