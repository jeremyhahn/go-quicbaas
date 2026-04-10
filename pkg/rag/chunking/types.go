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

// Strategy defines the chunking interface.
type Strategy interface {
	Chunk(content []byte, metadata Metadata) ([]Chunk, error)
	Name() string
}

// Chunk represents a document chunk.
type Chunk struct {
	Content  string
	Metadata Metadata
	Tokens   int
	Index    int // Chunk index in document
}

// Metadata holds chunk context.
type Metadata struct {
	FilePath     string
	Language     int // Will use language.Language
	StartLine    int
	EndLine      int
	FunctionName string
	ClassName    string
	Tags         []string
	Context      map[string]string
}

// Clone creates a deep copy of metadata.
func (m Metadata) Clone() Metadata {
	clone := m
	if m.Tags != nil {
		clone.Tags = make([]string, len(m.Tags))
		copy(clone.Tags, m.Tags)
	}
	if m.Context != nil {
		clone.Context = make(map[string]string, len(m.Context))
		for k, v := range m.Context {
			clone.Context[k] = v
		}
	}
	return clone
}
