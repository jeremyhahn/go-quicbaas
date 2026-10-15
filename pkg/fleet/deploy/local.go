package deploy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
)

// localShell is the POSIX shell LocalTarget runs commands under, as an ssh
// session runs a command under the login user's shell.
var localShell = []string{"/bin/sh", "-c"}

// LocalTarget is the install Target for the host this process runs on: a
// fleet node co-located with quicbaasd installs through it with no sshd.
// Commands run under /bin/sh through os/exec, and files are staged beside
// their destination and renamed into place, as the sftp upload does.
type LocalTarget struct {
	host  string
	user  string
	home  string
	shell []string
}

// NewLocalTarget returns the local install target for the current user.
// host is the name this host is advertised under; it appears in errors
// and reports.
func NewLocalTarget(host string) (*LocalTarget, error) {
	current, err := user.Current()
	if err != nil {
		return nil, &LocalTargetError{Cause: err}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, &LocalTargetError{Cause: err}
	}
	return &LocalTarget{host: host, user: current.Username, home: home, shell: localShell}, nil
}

// Host returns the name this host is advertised under.
func (t *LocalTarget) Host() string { return t.host }

// User returns the user the install runs as.
func (t *LocalTarget) User() string { return t.user }

// Exec runs cmd under the local shell and returns its stdout and stderr.
func (t *LocalTarget) Exec(ctx context.Context, cmd string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	args := append(append([]string(nil), t.shell[1:]...), cmd)
	run := exec.CommandContext(ctx, t.shell[0], args...)
	run.Stdout = &stdout
	run.Stderr = &stderr
	if err := run.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			err = ctxErr
		}
		return stdout.String(), stderr.String(), &LocalExecError{
			Command: cmd,
			Stderr:  strings.TrimSpace(stderr.String()),
			Cause:   err,
		}
	}
	return stdout.String(), stderr.String(), nil
}

// WriteFile writes data to path with mode, creating its parent
// directories. The file is staged and renamed into place, so a reader
// never sees it half written.
func (t *LocalTarget) WriteFile(ctx context.Context, path string, data []byte, mode os.FileMode) error {
	dest := t.expandHome(path)
	if err := writeAtomic(ctx, dest, bytes.NewReader(data), mode); err != nil {
		return &LocalWriteError{Path: dest, Cause: err}
	}
	return nil
}

// Upload copies the local file at localPath to path, executable. Like the
// sftp upload, the copy is staged and renamed into place: a redeploy
// replaces the binary of an agent that is running, which keeps its inode.
func (t *LocalTarget) Upload(ctx context.Context, localPath, path string) error {
	dest := t.expandHome(path)
	src, err := os.Open(localPath)
	if err != nil {
		return &LocalWriteError{Path: dest, Source: localPath, Cause: err}
	}
	defer src.Close()
	if err := writeAtomic(ctx, dest, src, 0o755); err != nil {
		return &LocalWriteError{Path: dest, Source: localPath, Cause: err}
	}
	return nil
}

// DialTCP opens a TCP connection to addr from this host.
func (t *LocalTarget) DialTCP(ctx context.Context, addr string) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, "tcp", addr)
}

// expandHome resolves a leading "$HOME", the form the install primitives
// write paths in for the remote shell to expand.
func (t *LocalTarget) expandHome(path string) string {
	if path == "$HOME" {
		return t.home
	}
	if rest, ok := strings.CutPrefix(path, "$HOME/"); ok {
		return filepath.Join(t.home, rest)
	}
	return path
}

// installDirMode is the mode of a directory writeAtomic creates. Every
// install directory (the binary, agent.yaml and identity, the systemd user
// unit directory) is read only by the user the agent runs as, so no other
// user needs to enter it.
const installDirMode os.FileMode = 0o750

// writeAtomic copies src to a temporary file beside dest, sets mode, and
// renames it over dest. On any failure the temporary file is removed and
// dest is left as it was.
func writeAtomic(ctx context.Context, dest string, src io.Reader, mode os.FileMode) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, installDirMode); err != nil {
		return err
	}
	staged, err := os.CreateTemp(dir, "."+filepath.Base(dest)+".*"+uploadSuffix)
	if err != nil {
		return err
	}
	closed, committed := false, false
	defer func() {
		if committed {
			return
		}
		// The failure that stopped the write leads; a failed cleanup
		// joins it rather than hiding it.
		if !closed {
			err = errors.Join(err, staged.Close())
		}
		if removeErr := os.Remove(staged.Name()); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			err = errors.Join(err, removeErr)
		}
	}()

	if _, err := io.Copy(staged, ctxReader{ctx: ctx, r: src}); err != nil {
		return err
	}
	if err := staged.Chmod(mode); err != nil {
		return err
	}
	closed = true
	if err := staged.Close(); err != nil {
		return err
	}
	if err := os.Rename(staged.Name(), dest); err != nil {
		return err
	}
	committed = true
	return nil
}

// ctxReader stops a copy once ctx is done.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
