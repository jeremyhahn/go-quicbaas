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

package orchestration

import (
	"os"
	"path/filepath"
)

// =============================================================================
// Account credential materialization.
//
// Two isolation modes:
//
//   - Env-based credentials (Account.Env / the OAuth AccessToken) need no
//     disk footprint at all: they are merged directly into SpawnRequest.Env,
//     which every existing spawn path (spawnCLI, spawnJSONRPC,
//     spawnOneShotCLI, spawnContainerJSONRPC) already forwards verbatim into
//     the subprocess/container environment. The credential lives only in
//     that one process's environment and disappears when it exits — there is
//     nothing to clean up and nothing shared between concurrent sessions.
//
//   - File-based credentials (e.g. OpenCode's auth.json) are written into a
//     FRESH per-session temporary directory created here, never into the
//     operator's real ~/.config. HOST mode points the subprocess's isolated
//     HOME at that directory (see spawnJSONRPC's existing "acp-session-*"
//     isolation, which this extends rather than replaces). SANDBOX/CONTAINER
//     mode bind-mounts that same directory read-only at the container's
//     expected config path INSTEAD OF the host's shared config directory
//     (see spawnContainerJSONRPC). Two concurrent sessions never share a
//     directory, so one identity's files can never leak into another
//     session even when both use the same provider.
// =============================================================================

// accountFilesDirPrefix names the temp directory created to hold one
// session's materialized account credential files.
const accountFilesDirPrefix = "quicbaas-account-*"

// materializeAccountFiles writes files (relative path -> content) into a
// fresh temporary directory and returns its root plus a cleanup function
// that removes it. A subdirectory implied by a file's relative path is
// created as needed. Returns ("", nil, nil) when files is empty — no
// directory is created for providers that carry their credential entirely
// in the environment.
func materializeAccountFiles(files map[string][]byte) (dir string, cleanup func(), err error) {
	if len(files) == 0 {
		return "", nil, nil
	}

	root, mkErr := os.MkdirTemp("", accountFilesDirPrefix)
	if mkErr != nil {
		return "", nil, &AccountStoreError{Message: "failed to create account materialization dir: " + mkErr.Error()}
	}
	cleanup = func() { _ = os.RemoveAll(root) }

	for relPath, content := range files {
		target := filepath.Join(root, filepath.FromSlash(relPath))
		if !isWithinDir(root, target) {
			cleanup()
			return "", nil, &AccountInvalidError{Field: "files", Message: "path escapes materialization directory: " + relPath}
		}
		if mkdirErr := os.MkdirAll(filepath.Dir(target), 0o700); mkdirErr != nil {
			cleanup()
			return "", nil, &AccountStoreError{Message: "failed to create account materialization subdir: " + mkdirErr.Error()}
		}
		if writeErr := os.WriteFile(target, content, 0o600); writeErr != nil {
			cleanup()
			return "", nil, &AccountStoreError{Message: "failed to write account materialization file: " + writeErr.Error()}
		}
	}
	return root, cleanup, nil
}

// isWithinDir reports whether target is root itself or a descendant of root,
// guarding against a crafted relative path (e.g. "../../etc/passwd") in a
// credential's Files map escaping the per-session directory.
func isWithinDir(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !hasDotDotPrefix(rel)
}

// hasDotDotPrefix reports whether a filepath.Rel result climbs above its
// base (starts with "../" or is exactly "..", already handled by the caller).
func hasDotDotPrefix(rel string) bool {
	sep := string(filepath.Separator)
	return len(rel) >= 3 && rel[:3] == ".."+sep
}
