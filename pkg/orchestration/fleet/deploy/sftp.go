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
	"context"
	"io"
	"os"
	"path"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// UploadBinary copies localPath to remotePath on target and marks the
// resulting file executable (0o755), through the target's own Upload.
func UploadBinary(ctx context.Context, target Target, localPath, remotePath string) error {
	if target == nil {
		return &SFTPUploadError{LocalPath: localPath, RemotePath: remotePath, Cause: errClosed}
	}
	return target.Upload(ctx, localPath, remotePath)
}

// Upload streams localPath to remotePath and marks the resulting file
// executable (0o755). The transfer is performed over the SFTP subsystem
// negotiated on top of the SSH connection.
//
// Directories up to and including filepath.Dir(remotePath) are
// created on demand. Any partial transfer that fails midway leaves a
// best-effort cleanup: the partial file is removed before the error
// is returned so retries do not see a truncated binary.
//
// THE BINARY IS STAGED AND RENAMED, NEVER WRITTEN IN PLACE. A redeploy
// replaces the binary of an agent that is running, and the remote refuses
// to open a running executable for writing (SFTP answers a bare "Failure"),
// so an in-place upload fails on every host that already has an agent
// running (measured redeploying the fleet, 2026-09-23). The upload lands at
// remotePath+uploadSuffix and a POSIX rename swaps it in atomically: the
// running process keeps its old inode, and a restart runs the new one.
func (c *SSHClient) Upload(ctx context.Context, localPath, remotePath string) error {
	if c.UnderlyingClient() == nil {
		return &SFTPUploadError{LocalPath: localPath, RemotePath: remotePath, Cause: errClosed}
	}

	client, err := sftp.NewClient(c.UnderlyingClient())
	if err != nil {
		return &SFTPUploadError{LocalPath: localPath, RemotePath: remotePath, Cause: err}
	}
	defer client.Close()

	return uploadBinaryWith(ctx, sftpAdapter{client}, localPath, remotePath)
}

// sftpClient captures the subset of *sftp.Client we actually use. Tests
// substitute a fake implementation to exercise the transfer logic
// without needing a real SFTP server; the real path goes through
// sftpAdapter which wraps the vendor type.
type sftpClient interface {
	MkdirAll(dir string) error
	Create(path string) (sftpFile, error)
	Chmod(path string, mode os.FileMode) error
	Remove(path string) error
	// PosixRename replaces newPath with oldPath atomically, overwriting it.
	PosixRename(oldPath, newPath string) error
}

// uploadSuffix names the staging file an upload is written to before it is
// renamed over the destination.
const uploadSuffix = ".upload"

// sftpFile captures the subset of *sftp.File we use.
type sftpFile interface {
	io.Writer
	io.Closer
}

// sftpAdapter bridges the vendored *sftp.Client to the internal
// sftpClient interface. This keeps the primitive testable without
// pulling an SFTP server into unit tests.
type sftpAdapter struct{ c *sftp.Client }

func (a sftpAdapter) MkdirAll(dir string) error           { return a.c.MkdirAll(dir) }
func (a sftpAdapter) Chmod(p string, m os.FileMode) error { return a.c.Chmod(p, m) }
func (a sftpAdapter) Remove(p string) error               { return a.c.Remove(p) }
func (a sftpAdapter) PosixRename(o, n string) error       { return a.c.PosixRename(o, n) }
func (a sftpAdapter) Create(p string) (sftpFile, error) {
	f, err := a.c.Create(p)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// uploadBinaryWith is the testable core of UploadBinary. It assumes
// the caller has already opened an SFTP session and is responsible
// for closing it.
func uploadBinaryWith(ctx context.Context, client sftpClient, localPath, remotePath string) error {
	// Refuse up front if the caller has already cancelled. Without this the
	// select below is the only cancellation check, and once the copy goroutine
	// has finished both its case and ctx.Done() are ready -- select then picks
	// between them at random, so a cancelled upload of a small file completes
	// and reports success roughly half the time.
	if err := ctx.Err(); err != nil {
		return &SFTPUploadError{LocalPath: localPath, RemotePath: remotePath, Cause: err}
	}

	local, err := os.Open(localPath)
	if err != nil {
		return &SFTPUploadError{LocalPath: localPath, RemotePath: remotePath, Cause: err}
	}
	defer local.Close()

	if dir := path.Dir(remotePath); dir != "" && dir != "." && dir != "/" {
		if err := client.MkdirAll(dir); err != nil {
			return &SFTPUploadError{LocalPath: localPath, RemotePath: remotePath, Cause: err}
		}
	}

	staged := remotePath + uploadSuffix
	remote, err := client.Create(staged)
	if err != nil {
		return &SFTPUploadError{LocalPath: localPath, RemotePath: remotePath, Cause: err}
	}

	// Copy with context cancellation: we do the transfer on a worker
	// goroutine and wait for either completion or ctx.Done. A cancelled
	// transfer closes the remote file and removes the partial upload.
	done := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(remote, local)
		closeErr := remote.Close()
		if copyErr != nil {
			done <- copyErr
			return
		}
		done <- closeErr
	}()

	select {
	case <-ctx.Done():
		_ = remote.Close()
		_ = client.Remove(staged)
		return &SFTPUploadError{LocalPath: localPath, RemotePath: remotePath, Cause: ctx.Err()}
	case err := <-done:
		if err != nil {
			_ = client.Remove(staged)
			return &SFTPUploadError{LocalPath: localPath, RemotePath: remotePath, Cause: err}
		}
	}

	if err := client.Chmod(staged, 0o755); err != nil {
		_ = client.Remove(staged)
		return &SFTPUploadError{LocalPath: localPath, RemotePath: remotePath, Cause: err}
	}
	if err := client.PosixRename(staged, remotePath); err != nil {
		_ = client.Remove(staged)
		return &SFTPUploadError{LocalPath: localPath, RemotePath: remotePath, Cause: err}
	}
	return nil
}

// Compile-time assertion that *ssh.Client is available for the SFTP
// negotiation. This keeps the import from being dropped by tooling
// since we only reference it transitively via sshClient.UnderlyingClient().
var _ = (*ssh.Client)(nil)
