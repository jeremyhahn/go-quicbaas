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
	"crypto/rand"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// wipeTree destroys every regular file under root and then removes root.
// Anything that is neither a regular file nor a directory stops the wipe with
// an *UnexpectedEntryError. Symlinks are never followed.
//
// Overwriting a file in place destroys its contents on filesystems that
// rewrite blocks in place. Copy-on-write filesystems, SSD wear levelling and
// snapshots or backups taken earlier can still hold the old blocks. The wipe
// removes the live copy; it cannot reach those.
func wipeTree(root string) error {
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return &LegacyStoreError{Op: "walk", Path: path, Err: err}
		}
		switch {
		case d.IsDir():
			return nil
		case d.Type().IsRegular():
			return shredFile(path)
		default:
			return &UnexpectedEntryError{Path: path, Mode: d.Type()}
		}
	})
	if err != nil {
		return err
	}
	if err := os.RemoveAll(root); err != nil {
		return &LegacyStoreError{Op: "remove", Path: root, Err: err}
	}
	return nil
}

// shredFile overwrites path with random bytes, syncs it to stable storage,
// and unlinks it.
func shredFile(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return &LegacyStoreError{Op: "open", Path: path, Err: err}
	}
	if err := overwrite(f); err != nil {
		return &LegacyStoreError{Op: "overwrite", Path: path, Err: errors.Join(err, f.Close())}
	}
	if err := f.Close(); err != nil {
		return &LegacyStoreError{Op: "close", Path: path, Err: err}
	}
	if err := os.Remove(path); err != nil {
		return &LegacyStoreError{Op: "remove", Path: path, Err: err}
	}
	return nil
}

// overwrite replaces the full length of f with random bytes and syncs it.
func overwrite(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if _, err := io.CopyN(f, rand.Reader, info.Size()); err != nil {
		return err
	}
	return f.Sync()
}
