//go:build !integration

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

// file_kv_store_errors_test.go exercises the filesystem error-handling
// branches of FileKVStore that the happy-path tests do not reach: directory
// creation failures, write/rename failures, and stat/read errors that are not
// "not found". These verify the QBError wrapping (code + op) used throughout
// the store so regressions in error reporting are caught.

package server

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/stretchr/testify/require"
)

// newFileFKV makes a FileKVStore rooted at a fresh temp dir.
func newFileFKV(t *testing.T) *FileKVStore {
	t.Helper()
	s, err := NewFileKVStore(t.TempDir())
	require.NoError(t, err)
	return s
}

func TestFileKVStore_New_MkdirAllFails(t *testing.T) {
	// Create a regular file, then ask for a data dir *under* that file.
	// MkdirAll cannot create a directory beneath a non-directory, so it errors.
	base := t.TempDir()
	fileAsParent := filepath.Join(base, "not-a-dir")
	require.NoError(t, os.WriteFile(fileAsParent, []byte("x"), 0600))

	_, err := NewFileKVStore(filepath.Join(fileAsParent, "child"))
	require.Error(t, err)
	var qb *dberrors.QBError
	require.ErrorAs(t, err, &qb)
	require.Equal(t, dberrors.ErrInternal, qb.Code)
	require.Equal(t, "file_kv_store.new", qb.Op)
}

func TestFileKVStore_Put_WriteFileFails(t *testing.T) {
	// Replace the data dir with a regular file so the temp-file WriteFile
	// inside Put cannot create its target.
	base := t.TempDir()
	dataDir := filepath.Join(base, "store")
	require.NoError(t, os.WriteFile(dataDir, []byte("x"), 0600))

	s := &FileKVStore{dataDir: dataDir}
	err := s.Put(context.Background(), "k", []byte("v"))
	require.Error(t, err)
	var qb *dberrors.QBError
	require.ErrorAs(t, err, &qb)
	require.Equal(t, dberrors.ErrInternal, qb.Code)
	require.Equal(t, "file_kv_store.put", qb.Op)
}

func TestFileKVStore_Get_ReadError_NotNotFound(t *testing.T) {
	s := newFileFKV(t)
	// Make the on-disk "file" for the key actually a directory so ReadFile
	// fails with EISDIR rather than IsNotExist.
	keyPath := filepath.Join(s.dataDir, hex.EncodeToString([]byte("k")))
	require.NoError(t, os.Mkdir(keyPath, 0750))

	_, err := s.Get(context.Background(), "k")
	require.Error(t, err)
	var qb *dberrors.QBError
	require.ErrorAs(t, err, &qb)
	require.Equal(t, dberrors.ErrInternal, qb.Code)
	require.Equal(t, "file_kv_store.get", qb.Op)
}

func TestFileKVStore_Delete_RemoveError_NotNotFound(t *testing.T) {
	s := newFileFKV(t)
	// A non-empty directory at the key path makes os.Remove fail with ENOTEMPTY,
	// which is neither nil nor IsNotExist.
	keyPath := filepath.Join(s.dataDir, hex.EncodeToString([]byte("k")))
	require.NoError(t, os.Mkdir(keyPath, 0750))
	require.NoError(t, os.WriteFile(filepath.Join(keyPath, "occupant"), []byte("x"), 0600))

	err := s.Delete(context.Background(), "k")
	require.Error(t, err)
	var qb *dberrors.QBError
	require.ErrorAs(t, err, &qb)
	require.Equal(t, dberrors.ErrInternal, qb.Code)
	require.Equal(t, "file_kv_store.delete", qb.Op)
}

func TestFileKVStore_Exists_StatError_NotNotFound(t *testing.T) {
	s := newFileFKV(t)
	// Put a key, then make the *parent* dataDir unreadable so Stat fails with
	// EACCES (not IsNotExist). Restored by t.TempDir cleanup via chmod back.
	require.NoError(t, s.Put(context.Background(), "k", []byte("v")))

	// Use a path component that is a file to force a stat traversal error.
	bad := &FileKVStore{dataDir: filepath.Join(s.dataDir, hex.EncodeToString([]byte("k")))}
	_, err := bad.Exists(context.Background(), "anything")
	require.Error(t, err)
	var qb *dberrors.QBError
	require.ErrorAs(t, err, &qb)
	require.Equal(t, dberrors.ErrInternal, qb.Code)
	require.Equal(t, "file_kv_store.exists", qb.Op)
}

func TestFileKVStore_List_ReadDirError(t *testing.T) {
	// dataDir points at a regular file → os.ReadDir fails.
	base := t.TempDir()
	asFile := filepath.Join(base, "file")
	require.NoError(t, os.WriteFile(asFile, []byte("x"), 0600))

	s := &FileKVStore{dataDir: asFile}
	_, err := s.List(context.Background(), "")
	require.Error(t, err)
	var qb *dberrors.QBError
	require.ErrorAs(t, err, &qb)
	require.Equal(t, dberrors.ErrInternal, qb.Code)
	require.Equal(t, "file_kv_store.list", qb.Op)
}

func TestFileKVStore_Scan_ReadDirError(t *testing.T) {
	base := t.TempDir()
	asFile := filepath.Join(base, "file")
	require.NoError(t, os.WriteFile(asFile, []byte("x"), 0600))

	s := &FileKVStore{dataDir: asFile}
	err := s.Scan(context.Background(), "", func(string, []byte) error { return nil })
	require.Error(t, err)
	var qb *dberrors.QBError
	require.ErrorAs(t, err, &qb)
	require.Equal(t, dberrors.ErrInternal, qb.Code)
	require.Equal(t, "file_kv_store.scan", qb.Op)
}

func TestFileKVStore_Scan_SkipsNonHexAndDirs(t *testing.T) {
	s := newFileFKV(t)
	require.NoError(t, s.Put(context.Background(), "real", []byte("v")))
	// A non-hex filename must be skipped by filenameToKey.
	require.NoError(t, os.WriteFile(filepath.Join(s.dataDir, "not-hex-name.zz"), []byte("x"), 0600))
	// A subdirectory must be skipped by the entry.IsDir() guard.
	require.NoError(t, os.Mkdir(filepath.Join(s.dataDir, "subdir"), 0750))

	seen := map[string][]byte{}
	err := s.Scan(context.Background(), "", func(k string, v []byte) error {
		seen[k] = v
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, map[string][]byte{"real": []byte("v")}, seen)
}

// TestFileKVStore_Put_RenameFails covers the os.Rename error branch of Put: the
// temp file is written successfully, but the final destination path is a
// non-empty directory, so the atomic rename fails and Put returns a typed
// QBError (after cleaning up the temp file).
func TestFileKVStore_Put_RenameFails(t *testing.T) {
	s := newFileFKV(t)

	// Make the destination filename a non-empty directory so Rename(tmp, dest)
	// fails with ENOTEMPTY / EISDIR rather than succeeding.
	destPath := filepath.Join(s.dataDir, hex.EncodeToString([]byte("k")))
	require.NoError(t, os.Mkdir(destPath, 0750))
	require.NoError(t, os.WriteFile(filepath.Join(destPath, "occupant"), []byte("x"), 0600))

	err := s.Put(context.Background(), "k", []byte("v"))
	require.Error(t, err, "rename onto a non-empty directory must fail")
	var qb *dberrors.QBError
	require.ErrorAs(t, err, &qb)
	require.Equal(t, dberrors.ErrInternal, qb.Code)
	require.Equal(t, "file_kv_store.put", qb.Op)

	// The temp file must have been cleaned up on the failure path.
	tmpPath := destPath + ".tmp"
	if _, statErr := os.Stat(tmpPath); !os.IsNotExist(statErr) {
		t.Fatalf("temp file %q must be removed after a failed rename", tmpPath)
	}
}
