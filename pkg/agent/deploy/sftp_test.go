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

package deploy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// fakeSFTPClient implements sftpClient for unit tests. It keeps every
// uploaded file in memory so assertions can verify exact bytes.
type fakeSFTPClient struct {
	mu         sync.Mutex
	mkdirs     []string
	created    map[string]*fakeSFTPFile
	removed    []string
	chmods     map[string]os.FileMode
	failMkdir  bool
	failCreate bool
	failChmod  bool
	failRemove bool
}

func newFakeSFTP() *fakeSFTPClient {
	return &fakeSFTPClient{
		created: make(map[string]*fakeSFTPFile),
		chmods:  make(map[string]os.FileMode),
	}
}

func (f *fakeSFTPClient) MkdirAll(dir string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failMkdir {
		return errors.New("mkdir denied")
	}
	f.mkdirs = append(f.mkdirs, dir)
	return nil
}

func (f *fakeSFTPClient) Create(p string) (sftpFile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failCreate {
		return nil, errors.New("create denied")
	}
	file := &fakeSFTPFile{}
	f.created[p] = file
	return file, nil
}

func (f *fakeSFTPClient) Chmod(p string, m os.FileMode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failChmod {
		return errors.New("chmod denied")
	}
	f.chmods[p] = m
	return nil
}

func (f *fakeSFTPClient) Remove(p string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failRemove {
		return errors.New("remove denied")
	}
	f.removed = append(f.removed, p)
	return nil
}

// fakeSFTPFile is a byte buffer that satisfies sftpFile.
type fakeSFTPFile struct {
	bytes.Buffer
	closed bool
}

func (f *fakeSFTPFile) Close() error {
	f.closed = true
	return nil
}

// writeLocal creates a small local file for upload tests.
func writeLocal(t *testing.T, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "bin")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("write local: %v", err)
	}
	return p
}

func TestUploadBinaryWith_Success(t *testing.T) {
	ctx := context.Background()
	fake := newFakeSFTP()
	payload := []byte("hello-binary")
	local := writeLocal(t, payload)

	if err := uploadBinaryWith(ctx, fake, local, "/remote/bin/quicbaas"); err != nil {
		t.Fatalf("upload: %v", err)
	}

	if len(fake.mkdirs) != 1 || fake.mkdirs[0] != "/remote/bin" {
		t.Fatalf("mkdirs=%v", fake.mkdirs)
	}
	uploaded := fake.created["/remote/bin/quicbaas"]
	if uploaded == nil || !uploaded.closed {
		t.Fatal("expected created and closed file")
	}
	if !bytes.Equal(uploaded.Bytes(), payload) {
		t.Fatalf("payload mismatch: %q", uploaded.Bytes())
	}
	if mode, ok := fake.chmods["/remote/bin/quicbaas"]; !ok || mode != 0o755 {
		t.Fatalf("expected chmod 0755, got %v ok=%v", mode, ok)
	}
}

func TestUploadBinaryWith_MkdirFails(t *testing.T) {
	ctx := context.Background()
	fake := newFakeSFTP()
	fake.failMkdir = true
	local := writeLocal(t, []byte("x"))

	err := uploadBinaryWith(ctx, fake, local, "/remote/bin/q")
	if err == nil {
		t.Fatal("expected error")
	}
	var uploadErr *SFTPUploadError
	if !errors.As(err, &uploadErr) {
		t.Fatalf("want SFTPUploadError, got %T", err)
	}
}

func TestUploadBinaryWith_CreateFails(t *testing.T) {
	ctx := context.Background()
	fake := newFakeSFTP()
	fake.failCreate = true
	local := writeLocal(t, []byte("x"))
	err := uploadBinaryWith(ctx, fake, local, "/remote/q")
	var uploadErr *SFTPUploadError
	if !errors.As(err, &uploadErr) {
		t.Fatalf("want SFTPUploadError, got %v", err)
	}
}

func TestUploadBinaryWith_ChmodFails(t *testing.T) {
	ctx := context.Background()
	fake := newFakeSFTP()
	fake.failChmod = true
	local := writeLocal(t, []byte("x"))
	err := uploadBinaryWith(ctx, fake, local, "/remote/q")
	var uploadErr *SFTPUploadError
	if !errors.As(err, &uploadErr) {
		t.Fatalf("want SFTPUploadError, got %v", err)
	}
}

func TestUploadBinaryWith_MissingLocalFile(t *testing.T) {
	ctx := context.Background()
	fake := newFakeSFTP()
	err := uploadBinaryWith(ctx, fake, "/does/not/exist", "/remote/q")
	if err == nil {
		t.Fatal("expected error")
	}
	var uploadErr *SFTPUploadError
	if !errors.As(err, &uploadErr) {
		t.Fatalf("want SFTPUploadError, got %v", err)
	}
}

func TestUploadBinaryWith_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Use a slow reader so the Copy takes long enough to observe the
	// cancellation; pipe's Write blocks until Read consumes bytes.
	pr, pw := io.Pipe()
	local := filepath.Join(t.TempDir(), "src")
	if err := os.WriteFile(local, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = pw
	_ = pr

	fake := newFakeSFTP()
	// With the already-cancelled context we expect the select to fire
	// immediately and surface a StartTimeoutError via SFTPUploadError.
	err := uploadBinaryWith(ctx, fake, local, "/remote/q")
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	var uploadErr *SFTPUploadError
	if !errors.As(err, &uploadErr) {
		t.Fatalf("want SFTPUploadError, got %v", err)
	}
}

func TestUploadBinary_NilClient(t *testing.T) {
	err := UploadBinary(context.Background(), nil, "/a", "/b")
	if err == nil {
		t.Fatal("expected error")
	}
	var uploadErr *SFTPUploadError
	if !errors.As(err, &uploadErr) {
		t.Fatalf("want SFTPUploadError, got %v", err)
	}
}
