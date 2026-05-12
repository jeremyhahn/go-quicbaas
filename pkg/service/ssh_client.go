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

package service

import (
	"path/filepath"
	"strings"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"
)

// defaultSSHStoreSubdir is the per-install directory name used for the
// embedded file-backed SSH store when the ServiceConfig.SSHStoreDir is
// not supplied. It is appended to ServiceConfig.DataDir when present
// and falls back to "./xkms-ssh" relative to the process cwd otherwise.
const defaultSSHStoreSubdir = "xkms-ssh"

// defaultSSHStoreRelative is the last-chance fallback for the file
// store when neither SSHStoreDir nor DataDir are configured. Kept as a
// named constant so the choice is obvious in review.
const defaultSSHStoreRelative = "xkms-ssh"

// SSHStoreRootMissingError is returned when the service cannot derive
// a filesystem root for the embedded SSH store because neither
// SSHStoreDir nor DataDir is configured. The typed error surfaces the
// misconfig plainly to callers (quicbaasd start, tests, embedding
// applications) rather than silently using a surprising path.
type SSHStoreRootMissingError struct{}

// Error implements the error interface.
func (e *SSHStoreRootMissingError) Error() string {
	return "service: ssh store root is required when XKMS.URL is empty"
}

// newSSHClient returns an xkms SSH client using the transport selected
// by cfg. When cfg.XKMS.URL is non-empty the REST transport is used;
// otherwise a file-backed embedded store is opened at cfg.SSHStoreDir
// (or a derived default under cfg.DataDir).
//
// When neither an XKMS URL nor a usable store root is configured the
// function returns (nil, nil) so the service starts up without an SSH
// surface — SSHClient() returns nil and handlers respond with 503.
// Deployments that want SSH enabled must set one of the two options.
//
// The returned xkmsssh.Client is owned by the caller — quicBaaS.Close
// invokes its Close method during shutdown.
func newSSHClient(cfg *ServiceConfig) (xkmsssh.Client, error) {
	if cfg == nil {
		return nil, nil
	}
	url := strings.TrimSpace(cfg.XKMS.URL)
	if url != "" {
		return xkmsssh.NewREST(xkmsssh.RESTOptions{
			BaseURL:   url,
			AuthToken: strings.TrimSpace(cfg.XKMS.Token),
		})
	}
	root := resolveSSHStoreRoot(cfg)
	if root == "" {
		return nil, nil
	}
	return xkmsssh.NewEmbeddedFile(xkmsssh.EmbeddedFileOptions{RootDir: root})
}

// resolveSSHStoreRoot returns the filesystem root for the embedded SSH
// store. Precedence:
//
//  1. cfg.SSHStoreDir — explicit operator choice.
//  2. cfg.DataDir + "/xkms-ssh" — the canonical default.
//  3. empty string — caller surfaces a typed error.
func resolveSSHStoreRoot(cfg *ServiceConfig) string {
	if cfg == nil {
		return ""
	}
	if dir := strings.TrimSpace(cfg.SSHStoreDir); dir != "" {
		return dir
	}
	if data := strings.TrimSpace(cfg.DataDir); data != "" {
		return filepath.Join(data, defaultSSHStoreSubdir)
	}
	return ""
}
