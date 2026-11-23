// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-trusted-platform.
//
// go-trusted-platform is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package containerruntime

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

// forbiddenImport is the package the runtime must not use: containers are run
// in-process through libcontainer, never by launching a host program.
const forbiddenImport = "os/exec"

func TestPackageNeverImportsOSExec(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)

	checked := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		imports, err := importsPackage(name, nil, forbiddenImport)
		require.NoError(t, err)
		assert.False(t, imports, "%s imports %s", name, forbiddenImport)
		checked++
	}
	require.NotZero(t, checked)
}

// runtimeDaemonClients are the client packages of runtimes that live behind a
// socket. The runtime drives containers in-process through libcontainer and
// pulls and unpacks images in-process, so it never dials one.
var runtimeDaemonClients = []string{
	"github.com/docker/docker/client",
	"github.com/moby/moby/client",
	"github.com/containerd/containerd",
	"github.com/containers/podman",
	"go.podman.io/podman",
	"k8s.io/cri-api",
	"github.com/opencontainers/runc/libcontainer/cgroups/systemd",
}

// TestPackageNeverDialsARuntimeDaemon proves the "no sockets" half of the
// container-runtime row: no non-test file imports a runtime daemon's client
// (Docker, containerd, Podman, CRI or systemd's bus) or the net package.
func TestPackageNeverDialsARuntimeDaemon(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)

	checked := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		imports, err := importsPackage(name, nil, "net")
		require.NoError(t, err)
		assert.False(t, imports, "%s imports net", name)
		for _, prefix := range runtimeDaemonClients {
			imported, err := importsPackagePrefix(name, nil, prefix)
			require.NoError(t, err)
			assert.Empty(t, imported, "%s imports runtime daemon client %s", name, imported)
		}
		checked++
	}
	require.NotZero(t, checked)
}

func TestImportsPackagePrefix(t *testing.T) {
	src := "package p\n\nimport _ \"github.com/docker/docker/client/sub\"\n"
	imported, err := importsPackagePrefix("p.go", src, "github.com/docker/docker/client")
	require.NoError(t, err)
	assert.Equal(t, "github.com/docker/docker/client/sub", imported)

	imported, err = importsPackagePrefix("p.go", src, "github.com/docker/docker/clientx")
	require.NoError(t, err)
	assert.Empty(t, imported)

	_, err = importsPackagePrefix("p.go", "not go", "x")
	require.Error(t, err)
}

// importsPackagePrefix returns the first import of the file that is prefix or
// lies under it, or "" if there is none.
func importsPackagePrefix(name string, src any, prefix string) (string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly)
	if err != nil {
		return "", err
	}
	for _, spec := range file.Imports {
		imported, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return "", err
		}
		if imported == prefix || strings.HasPrefix(imported, prefix+"/") {
			return imported, nil
		}
	}
	return "", nil
}

func TestImportsPackage(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want bool
	}{
		{"plain import", `import "os/exec"`, true},
		{"named import", `import run "os/exec"`, true},
		{"grouped import", "import (\n\"os\"\n\"os/exec\"\n)", true},
		{"other package", `import "os"`, false},
		{"no imports", ``, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := importsPackage("src.go", "package p\n"+tt.src, forbiddenImport)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestImportsPackage_InvalidSource(t *testing.T) {
	_, err := importsPackage("src.go", "not go", forbiddenImport)
	require.Error(t, err)
}

// importsPackage reports whether the Go file name (parsed from src when src
// is not nil) imports path.
func importsPackage(name string, src any, path string) (bool, error) {
	file, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly)
	if err != nil {
		return false, err
	}
	for _, spec := range file.Imports {
		imported, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return false, err
		}
		if imported == path {
			return true, nil
		}
	}
	return false, nil
}
