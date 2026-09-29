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

package sshstore

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestShredFile_OverwritesBeforeUnlinking reads the file's blocks through a
// second hard link, which outlives the unlink, to prove the contents were
// destroyed and not merely unlinked.
func TestShredFile_OverwritesBeforeUnlinking(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key")
	witness := filepath.Join(dir, "witness")
	secret := bytes.Repeat([]byte("PRIVATE KEY MATERIAL "), 64)
	require.NoError(t, os.WriteFile(path, secret, 0o600))
	require.NoError(t, os.Link(path, witness))

	require.NoError(t, shredFile(path))

	assert.NoFileExists(t, path)
	remains, err := os.ReadFile(witness)
	require.NoError(t, err)
	assert.Len(t, remains, len(secret), "the overwrite covers the whole file")
	assert.NotEqual(t, secret, remains)
	assert.False(t, bytes.Contains(remains, []byte("PRIVATE KEY")))
}

func TestShredFile_MissingFile(t *testing.T) {
	err := shredFile(filepath.Join(t.TempDir(), "absent"))
	var storeErr *LegacyStoreError
	require.ErrorAs(t, err, &storeErr)
	assert.Equal(t, "open", storeErr.Op)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestWipeTree_RemovesNestedFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tree")
	nested := filepath.Join(root, "ssh", "keys", "user", "alice")
	require.NoError(t, os.MkdirAll(nested, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(nested, "a"), []byte("a"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "b"), []byte("b"), 0o600))

	require.NoError(t, wipeTree(root))
	assert.NoDirExists(t, root)
}

// TestWipeTree_RefusesSymlink proves the wipe never follows a link out of the
// legacy tree: the link target keeps its contents and the tree is left for an
// operator to inspect.
func TestWipeTree_RefusesSymlink(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "outside")
	require.NoError(t, os.WriteFile(target, []byte("keep me"), 0o600))
	root := filepath.Join(base, "tree")
	require.NoError(t, os.MkdirAll(root, 0o700))
	require.NoError(t, os.Symlink(target, filepath.Join(root, "link")))

	err := wipeTree(root)
	var unexpected *UnexpectedEntryError
	require.ErrorAs(t, err, &unexpected)
	assert.Equal(t, filepath.Join(root, "link"), unexpected.Path)

	kept, readErr := os.ReadFile(target)
	require.NoError(t, readErr)
	assert.Equal(t, "keep me", string(kept))
	assert.DirExists(t, root)
}

func TestWipeTree_MissingRoot(t *testing.T) {
	err := wipeTree(filepath.Join(t.TempDir(), "absent"))
	var storeErr *LegacyStoreError
	require.ErrorAs(t, err, &storeErr)
	assert.Equal(t, "walk", storeErr.Op)
	assert.True(t, errors.Is(err, os.ErrNotExist))
}
