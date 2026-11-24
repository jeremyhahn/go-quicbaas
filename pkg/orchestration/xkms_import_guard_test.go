// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package orchestration

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// goXKMSPkgPrefix is the import-path prefix of go-xkms's implementation
// tree. go-xkms is consumed through go-xkms/sdk/go only; its pkg/ tree is the
// implementation.
const goXKMSPkgPrefix = "github.com/jeremyhahn/go-xkms/pkg/"

// TestNoGoXKMSPkgImports checks that the files moved onto go-xkms/sdk/go
// import no go-xkms pkg/ path.
func TestNoGoXKMSPkgImports(t *testing.T) {
	patterns := []string{
		"*.go",
		"../../sdk/go/orchestration/*.go",
		"../service/attestationtest/*.go",
		"../service/attestation_verifier.go",
	}
	for _, pattern := range patterns {
		files, err := filepath.Glob(pattern)
		require.NoError(t, err)
		require.NotEmpty(t, files, "no files match %s", pattern)
		for _, name := range files {
			found, err := goXKMSPkgImports(name, nil)
			require.NoError(t, err)
			assert.Empty(t, found, "%s imports go-xkms pkg/", name)
		}
	}
}

func TestGoXKMSPkgImports(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{"plain pkg import", `import "github.com/jeremyhahn/go-xkms/pkg/tpm2"`, []string{"github.com/jeremyhahn/go-xkms/pkg/tpm2"}},
		{"named pkg import", `import xkmstpm2 "github.com/jeremyhahn/go-xkms/pkg/tpm2"`, []string{"github.com/jeremyhahn/go-xkms/pkg/tpm2"}},
		{"sdk root", `import xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"`, nil},
		{"sdk evidence", `import "github.com/jeremyhahn/go-xkms/sdk/go/evidence"`, nil},
		{"no imports", ``, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := goXKMSPkgImports("src.go", "package p\n"+tt.src)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGoXKMSPkgImports_InvalidSource(t *testing.T) {
	_, err := goXKMSPkgImports("src.go", "not go")
	require.Error(t, err)
}

// goXKMSPkgImports returns the go-xkms pkg/ import paths of the Go source in
// name (or src, when non-nil).
func goXKMSPkgImports(name string, src any) ([]string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	var found []string
	for _, spec := range file.Imports {
		imported, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(imported, goXKMSPkgPrefix) {
			found = append(found, imported)
		}
	}
	return found, nil
}
