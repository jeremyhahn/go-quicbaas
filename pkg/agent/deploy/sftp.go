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

// UploadBinary streams localPath to remotePath on the SSH connection
// and marks the resulting file executable (0o755). The transfer is
// performed over the SFTP subsystem negotiated on top of the SSH
// connection owned by sshClient.
//
// Directories up to and including filepath.Dir(remotePath) are
// created on demand. Any partial transfer that fails midway leaves a
// best-effort cleanup: the partial file is removed before the error
// is returned so retries do not see a truncated binary.
func UploadBinary(ctx context.Context, sshClient *SSHClient, localPath, remotePath string) error {
	if sshClient == nil || sshClient.UnderlyingClient() == nil {
		return &SFTPUploadError{LocalPath: localPath, RemotePath: remotePath, Cause: errClosed}
	}

	client, err := sftp.NewClient(sshClient.UnderlyingClient())
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
}

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

	remote, err := client.Create(remotePath)
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
		_ = client.Remove(remotePath)
		return &SFTPUploadError{LocalPath: localPath, RemotePath: remotePath, Cause: ctx.Err()}
	case err := <-done:
		if err != nil {
			_ = client.Remove(remotePath)
			return &SFTPUploadError{LocalPath: localPath, RemotePath: remotePath, Cause: err}
		}
	}

	if err := client.Chmod(remotePath, 0o755); err != nil {
		return &SFTPUploadError{LocalPath: localPath, RemotePath: remotePath, Cause: err}
	}
	return nil
}

// Compile-time assertion that *ssh.Client is available for the SFTP
// negotiation. This keeps the import from being dropped by tooling
// since we only reference it transitively via sshClient.UnderlyingClient().
var _ = (*ssh.Client)(nil)
