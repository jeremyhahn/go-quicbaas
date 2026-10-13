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
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strings"
)

// GoCodeChunkerConfig configures the Go code chunker.
type GoCodeChunkerConfig struct {
	MaxFunctionTokens int  // Max tokens per function chunk (warning threshold)
	IncludeImports    bool // Include imports in each chunk
	IncludeComments   bool // Include doc comments
	IncludePackage    bool // Include package declaration
}

// DefaultGoCodeChunkerConfig returns sensible defaults.
func DefaultGoCodeChunkerConfig() GoCodeChunkerConfig {
	return GoCodeChunkerConfig{
		MaxFunctionTokens: 2000,
		IncludeImports:    true,
		IncludeComments:   true,
		IncludePackage:    true,
	}
}

// GoCodeChunker chunks Go code by semantic units using AST parsing.
type GoCodeChunker struct {
	config    GoCodeChunkerConfig
	tokenizer Tokenizer
}

// NewGoCodeChunker creates a new Go code chunker.
func NewGoCodeChunker(config GoCodeChunkerConfig) *GoCodeChunker {
	return &GoCodeChunker{
		config:    config,
		tokenizer: NewSimpleTokenizer(),
	}
}

// Name returns the chunker name.
func (c *GoCodeChunker) Name() string {
	return "go-code-ast"
}

// GoParseError is returned when Go source code cannot be parsed.
type GoParseError struct {
	Cause error
}

// Error implements the error interface.
func (e *GoParseError) Error() string {
	return "failed to parse Go source: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *GoParseError) Unwrap() error {
	return e.Cause
}

// GoRenderError is returned when a parsed Go syntax node cannot be printed
// back to source.
type GoRenderError struct {
	Cause error
}

// Error implements the error interface.
func (e *GoRenderError) Error() string {
	return "failed to render Go source: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *GoRenderError) Unwrap() error {
	return e.Cause
}

// writeNode writes node into buf as gofmt-formatted Go source.
func writeNode(buf *bytes.Buffer, fset *token.FileSet, node any) error {
	if err := format.Node(buf, fset, node); err != nil {
		return &GoRenderError{Cause: err}
	}
	return nil
}

// Chunk splits Go source code into semantic chunks using AST parsing.
func (c *GoCodeChunker) Chunk(content []byte, metadata Metadata) ([]Chunk, error) {
	if len(content) == 0 {
		return []Chunk{}, nil
	}

	// Parse the Go source file
	fset := token.NewFileSet()
	parseMode := parser.Mode(0)
	if c.config.IncludeComments {
		parseMode = parser.ParseComments
	}

	file, err := parser.ParseFile(fset, metadata.FilePath, content, parseMode)
	if err != nil {
		// Return error for invalid syntax
		return nil, &GoParseError{Cause: err}
	}

	// Extract package declaration and imports
	pkgDecl := c.extractPackageDecl(file, fset)
	imports, err := c.extractImports(file, fset)
	if err != nil {
		return nil, err
	}

	// Extract all chunks (functions, methods, types)
	chunks, err := c.extractChunks(file, fset, pkgDecl, imports, metadata)
	if err != nil {
		return nil, err
	}

	// Number chunks
	for i := range chunks {
		chunks[i].Index = i
	}

	return chunks, nil
}

// extractPackageDecl extracts the package declaration.
func (c *GoCodeChunker) extractPackageDecl(file *ast.File, fset *token.FileSet) string {
	if !c.config.IncludePackage || file.Name == nil {
		return ""
	}

	var buf bytes.Buffer
	buf.WriteString("package ")
	buf.WriteString(file.Name.Name)
	buf.WriteString("\n")
	return buf.String()
}

// extractImports extracts import declarations.
func (c *GoCodeChunker) extractImports(file *ast.File, fset *token.FileSet) ([]importInfo, error) {
	if !c.config.IncludeImports {
		return nil, nil
	}

	var imports []importInfo
	for _, imp := range file.Imports {
		var buf bytes.Buffer
		if err := writeNode(&buf, fset, imp); err != nil {
			return nil, err
		}

		// Parse import path and name
		path := strings.Trim(imp.Path.Value, `"`)
		name := ""
		if imp.Name != nil {
			name = imp.Name.Name
		}

		imports = append(imports, importInfo{
			Path:   path,
			Name:   name,
			Source: buf.String(),
		})
	}
	return imports, nil
}

// importInfo holds import declaration information.
type importInfo struct {
	Path   string // Import path (e.g., "fmt")
	Name   string // Import name (e.g., "f" for "import f 'fmt'")
	Source string // Full source (e.g., `import "fmt"`)
}

// extractChunks extracts all semantic chunks from the AST.
func (c *GoCodeChunker) extractChunks(file *ast.File, fset *token.FileSet, pkgDecl string, imports []importInfo, metadata Metadata) ([]Chunk, error) {
	var chunks []Chunk

	// Track used imports per declaration
	usedImports := make(map[ast.Node]map[string]bool)
	if c.config.IncludeImports {
		c.analyzeImportUsage(file, usedImports)
	}

	// Process all declarations
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			// Function or method
			chunk, err := c.buildFunctionChunk(d, pkgDecl, imports, usedImports[d], fset, metadata)
			if err != nil {
				return nil, err
			}
			chunks = append(chunks, chunk)

		case *ast.GenDecl:
			// Type, const, var, or import declaration
			switch d.Tok {
			case token.TYPE:
				// Type definitions
				for _, spec := range d.Specs {
					if typeSpec, ok := spec.(*ast.TypeSpec); ok {
						chunk, err := c.buildTypeChunk(d, typeSpec, pkgDecl, imports, usedImports[d], fset, metadata)
						if err != nil {
							return nil, err
						}
						chunks = append(chunks, chunk)
					}
				}
			case token.CONST, token.VAR:
				// Only include significant const/var blocks (with doc comments or multiple values)
				if d.Doc != nil || len(d.Specs) > 1 {
					chunk, err := c.buildDeclChunk(d, pkgDecl, imports, usedImports[d], fset, metadata)
					if err != nil {
						return nil, err
					}
					if chunk.Content != "" {
						chunks = append(chunks, chunk)
					}
				}
			}
		}
	}

	return chunks, nil
}

// buildFunctionChunk creates a chunk for a function or method.
func (c *GoCodeChunker) buildFunctionChunk(fn *ast.FuncDecl, pkgDecl string, imports []importInfo, usedImports map[string]bool, fset *token.FileSet, metadata Metadata) (Chunk, error) {
	var buf bytes.Buffer

	// Add package declaration
	if c.config.IncludePackage && pkgDecl != "" {
		buf.WriteString(pkgDecl)
		buf.WriteString("\n")
	}

	// Add imports
	c.writeImports(&buf, imports, usedImports)

	// Add function declaration; the printer emits its doc comment with it.
	if err := writeNode(&buf, fset, fn); err != nil {
		return Chunk{}, err
	}
	buf.WriteString("\n")

	content := buf.String()
	tokens := c.tokenizer.Count(content)

	// Extract metadata
	chunkMeta := metadata.Clone()

	// Get line numbers based on whether we include comments
	if c.config.IncludeComments && fn.Doc != nil {
		chunkMeta.StartLine = fset.Position(fn.Doc.Pos()).Line
	} else {
		chunkMeta.StartLine = fset.Position(fn.Pos()).Line
	}
	chunkMeta.EndLine = fset.Position(fn.End()).Line
	chunkMeta.FunctionName = fn.Name.Name

	// Add receiver type for methods
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		if typ := fn.Recv.List[0].Type; typ != nil {
			var recvBuf bytes.Buffer
			if err := writeNode(&recvBuf, fset, typ); err != nil {
				return Chunk{}, err
			}
			chunkMeta.ClassName = strings.Trim(recvBuf.String(), "*")
		}
	}

	// Add context information
	if chunkMeta.Context == nil {
		chunkMeta.Context = make(map[string]string)
	}
	if fn.Recv != nil {
		chunkMeta.Context["type"] = "method"
	} else {
		chunkMeta.Context["type"] = "function"
	}

	// Check if function is too large
	if c.config.MaxFunctionTokens > 0 && tokens > c.config.MaxFunctionTokens {
		chunkMeta.Context["too_large"] = "true"
		chunkMeta.Context["token_count"] = fmt.Sprintf("%d", tokens)
	}

	return Chunk{
		Content:  content,
		Metadata: chunkMeta,
		Tokens:   tokens,
	}, nil
}

// buildTypeChunk creates a chunk for a type definition.
func (c *GoCodeChunker) buildTypeChunk(decl *ast.GenDecl, typeSpec *ast.TypeSpec, pkgDecl string, imports []importInfo, usedImports map[string]bool, fset *token.FileSet, metadata Metadata) (Chunk, error) {
	var buf bytes.Buffer

	// Add package declaration
	if c.config.IncludePackage && pkgDecl != "" {
		buf.WriteString(pkgDecl)
		buf.WriteString("\n")
	}

	// Add imports
	c.writeImports(&buf, imports, usedImports)

	// Add doc comment. It belongs to the enclosing GenDecl, so printing the
	// TypeSpec alone would drop it.
	if c.config.IncludeComments && decl.Doc != nil {
		for _, comment := range decl.Doc.List {
			buf.WriteString(comment.Text)
			buf.WriteString("\n")
		}
	}

	// Add type declaration
	buf.WriteString("type ")
	if err := writeNode(&buf, fset, typeSpec); err != nil {
		return Chunk{}, err
	}
	buf.WriteString("\n")

	content := buf.String()
	tokens := c.tokenizer.Count(content)

	// Extract metadata
	chunkMeta := metadata.Clone()

	// Get line numbers based on whether we include comments
	if c.config.IncludeComments && decl.Doc != nil {
		chunkMeta.StartLine = fset.Position(decl.Doc.Pos()).Line
	} else {
		chunkMeta.StartLine = fset.Position(typeSpec.Pos()).Line
	}
	chunkMeta.EndLine = fset.Position(typeSpec.End()).Line
	chunkMeta.ClassName = typeSpec.Name.Name

	// Add context information
	if chunkMeta.Context == nil {
		chunkMeta.Context = make(map[string]string)
	}
	chunkMeta.Context["type"] = "type_definition"

	return Chunk{
		Content:  content,
		Metadata: chunkMeta,
		Tokens:   tokens,
	}, nil
}

// buildDeclChunk creates a chunk for const/var declarations.
func (c *GoCodeChunker) buildDeclChunk(decl *ast.GenDecl, pkgDecl string, imports []importInfo, usedImports map[string]bool, fset *token.FileSet, metadata Metadata) (Chunk, error) {
	var buf bytes.Buffer

	// Add package declaration
	if c.config.IncludePackage && pkgDecl != "" {
		buf.WriteString(pkgDecl)
		buf.WriteString("\n")
	}

	// Add imports
	c.writeImports(&buf, imports, usedImports)

	// Add declaration; the printer emits its doc comment with it.
	if err := writeNode(&buf, fset, decl); err != nil {
		return Chunk{}, err
	}
	buf.WriteString("\n")

	content := buf.String()
	tokens := c.tokenizer.Count(content)

	// Extract metadata
	chunkMeta := metadata.Clone()

	// Get line numbers based on whether we include comments
	if c.config.IncludeComments && decl.Doc != nil {
		chunkMeta.StartLine = fset.Position(decl.Doc.Pos()).Line
	} else {
		chunkMeta.StartLine = fset.Position(decl.Pos()).Line
	}
	chunkMeta.EndLine = fset.Position(decl.End()).Line

	// Add context information
	if chunkMeta.Context == nil {
		chunkMeta.Context = make(map[string]string)
	}
	if decl.Tok == token.CONST {
		chunkMeta.Context["type"] = "const_declaration"
	} else {
		chunkMeta.Context["type"] = "var_declaration"
	}

	return Chunk{
		Content:  content,
		Metadata: chunkMeta,
		Tokens:   tokens,
	}, nil
}

// writeImports writes import declarations to the buffer.
func (c *GoCodeChunker) writeImports(buf *bytes.Buffer, imports []importInfo, usedImports map[string]bool) {
	if !c.config.IncludeImports || len(imports) == 0 {
		return
	}

	// Filter to used imports if available
	var toWrite []importInfo
	if len(usedImports) > 0 {
		for _, imp := range imports {
			if usedImports[imp.Path] {
				toWrite = append(toWrite, imp)
			}
		}
	} else {
		toWrite = imports
	}

	if len(toWrite) == 0 {
		return
	}

	// Write imports
	if len(toWrite) == 1 {
		buf.WriteString("import ")
		buf.WriteString(toWrite[0].Source)
		buf.WriteString("\n\n")
	} else {
		buf.WriteString("import (\n")
		for _, imp := range toWrite {
			buf.WriteString("\t")
			buf.WriteString(imp.Source)
			buf.WriteString("\n")
		}
		buf.WriteString(")\n\n")
	}
}

// analyzeImportUsage analyzes which imports are used by each declaration.
func (c *GoCodeChunker) analyzeImportUsage(file *ast.File, result map[ast.Node]map[string]bool) {
	// Build map of package names to import paths
	pkgToPath := make(map[string]string)
	dotImports := make([]string, 0)

	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)

		// Handle dot imports - they make all exports available without qualification
		if imp.Name != nil && imp.Name.Name == "." {
			dotImports = append(dotImports, path)
			continue
		}

		pkgName := path

		// Get package name from path
		if idx := strings.LastIndex(path, "/"); idx >= 0 {
			pkgName = path[idx+1:]
		}

		// Handle explicit package names
		if imp.Name != nil {
			pkgName = imp.Name.Name
		}

		pkgToPath[pkgName] = path
	}

	// Analyze each declaration
	for _, decl := range file.Decls {
		used := make(map[string]bool)

		// Walk the AST to find selector expressions (package.Identifier)
		ast.Inspect(decl, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if ident, ok := sel.X.(*ast.Ident); ok {
					if path, exists := pkgToPath[ident.Name]; exists {
						used[path] = true
					}
				}
			}

			// For dot imports, we can't reliably detect usage through AST alone
			// since identifiers are not qualified. Mark as used if function contains
			// any identifier that could be from the dot import.
			// This is conservative but safe.
			if len(dotImports) > 0 {
				if _, ok := n.(*ast.Ident); ok {
					for _, path := range dotImports {
						used[path] = true
					}
				}
			}

			return true
		})

		result[decl] = used
	}
}
