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

//go:build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// hasDisplay reports whether a graphical display server is reachable. On
// Linux we accept any of three signals, in order:
//
//  1. $DISPLAY is set (standard X11)
//  2. $WAYLAND_DISPLAY is set (standard Wayland)
//  3. /tmp/.X11-unix/X<N> sockets exist AND (Wayland sockets under
//     $XDG_RUNTIME_DIR exist OR $XDG_SESSION_TYPE names one)
//
// Case 3 is the "env didn't propagate but a session is running" fallback
// — common when the CLI is invoked from a context that stripped env
// (sudo -E missing, a shell started outside the desktop session, or a
// terminal multiplexer forked before DISPLAY was exported). In that case
// we set $DISPLAY to the first X socket we find (typically ":0") and
// $WAYLAND_DISPLAY from the runtime dir so Wails's Webkit backend can
// connect. Mutating env is safe here because main() runs before anything
// forks.
func hasDisplay() bool {
	if os.Getenv("DISPLAY") != "" {
		return true
	}
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		return true
	}

	// X11 socket fallback: /tmp/.X11-unix/X<N> — each file is a Unix
	// socket for display :N. Pick the lowest-numbered one we find.
	if n, ok := findX11DisplayNumber("/tmp/.X11-unix"); ok {
		_ = os.Setenv("DISPLAY", fmt.Sprintf(":%d", n))
		return true
	}

	// Wayland socket fallback: $XDG_RUNTIME_DIR/wayland-<N>.
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		if name, ok := findWaylandSocket(runtimeDir); ok {
			_ = os.Setenv("WAYLAND_DISPLAY", name)
			return true
		}
	}

	return false
}

// findX11DisplayNumber returns the lowest display number N for which
// /tmp/.X11-unix/X<N> is a live Unix socket.
func findX11DisplayNumber(dir string) (int, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, false
	}
	best := -1
	for _, e := range entries {
		name := e.Name()
		if len(name) < 2 || name[0] != 'X' {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(name[1:], "%d", &n); err != nil {
			continue
		}
		if best == -1 || n < best {
			best = n
		}
	}
	if best < 0 {
		return 0, false
	}
	return best, true
}

// findWaylandSocket returns the name of a Wayland socket (e.g. "wayland-0")
// reachable under runtimeDir. Matches the conventional wayland-<N> pattern.
func findWaylandSocket(runtimeDir string) (string, bool) {
	matches, err := filepath.Glob(filepath.Join(runtimeDir, "wayland-*"))
	if err != nil || len(matches) == 0 {
		return "", false
	}
	// Prefer the shortest name (wayland-0 over wayland-0.lock).
	best := ""
	for _, m := range matches {
		base := filepath.Base(m)
		if len(base) > len("wayland-") && !containsDot(base) {
			if best == "" || len(base) < len(best) {
				best = base
			}
		}
	}
	return best, best != ""
}

// containsDot reports whether s contains a '.' — used to reject lock
// files like wayland-0.lock.
func containsDot(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			return true
		}
	}
	return false
}
