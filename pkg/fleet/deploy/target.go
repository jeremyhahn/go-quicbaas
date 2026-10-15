package deploy

import (
	"context"
	"net"
	"os"
)

// Target is the host an install runs on. The install primitives
// (UploadBinary, InstallAndStart, StartBackground, EnsureLinger, the
// lifecycle verbs and RunPreflight) need only this, so one install path
// serves two transports: *SSHClient reaches a remote host over ssh, and
// *LocalTarget installs on the host this process runs on, with no sshd.
//
// Paths may begin with "$HOME", which the target resolves to its login
// user's home directory.
type Target interface {
	// Host names the target in errors and reports.
	Host() string

	// User is the login user commands run as.
	User() string

	// Exec runs cmd under the target's POSIX shell and returns its
	// stdout and stderr. A non-zero exit is an error.
	Exec(ctx context.Context, cmd string) (stdout, stderr string, err error)

	// WriteFile writes data to path with the given mode, creating the
	// parent directories.
	WriteFile(ctx context.Context, path string, data []byte, mode os.FileMode) error

	// Upload copies the local file at localPath to path, executable
	// (0o755), staged and renamed into place so a running binary is never
	// written in place.
	Upload(ctx context.Context, localPath, path string) error

	// DialTCP opens a TCP connection to addr as seen from the target.
	DialTCP(ctx context.Context, addr string) (net.Conn, error)
}

// Both transports satisfy Target.
var (
	_ Target = (*SSHClient)(nil)
	_ Target = (*LocalTarget)(nil)
)
