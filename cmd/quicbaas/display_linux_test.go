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
	"os"
	"path/filepath"
	"testing"
)

// withCleanDisplayEnv unsets DISPLAY / WAYLAND_DISPLAY / XDG_RUNTIME_DIR
// for the test body and restores them afterwards.
func withCleanDisplayEnv(t *testing.T, body func()) {
	t.Helper()
	origDisplay, _ := os.LookupEnv("DISPLAY")
	origWayland, _ := os.LookupEnv("WAYLAND_DISPLAY")
	origRuntime, _ := os.LookupEnv("XDG_RUNTIME_DIR")
	t.Cleanup(func() {
		os.Setenv("DISPLAY", origDisplay)
		os.Setenv("WAYLAND_DISPLAY", origWayland)
		os.Setenv("XDG_RUNTIME_DIR", origRuntime)
	})
	os.Unsetenv("DISPLAY")
	os.Unsetenv("WAYLAND_DISPLAY")
	os.Unsetenv("XDG_RUNTIME_DIR")
	body()
}

func TestHasDisplay_X11(t *testing.T) {
	withCleanDisplayEnv(t, func() {
		os.Setenv("DISPLAY", ":0")
		if !hasDisplay() {
			t.Error("expected hasDisplay() to return true when DISPLAY is set")
		}
	})
}

func TestHasDisplay_Wayland(t *testing.T) {
	withCleanDisplayEnv(t, func() {
		os.Setenv("WAYLAND_DISPLAY", "wayland-0")
		if !hasDisplay() {
			t.Error("expected hasDisplay() to return true when WAYLAND_DISPLAY is set")
		}
	})
}

// TestHasDisplay_NoEnvAndNoSockets — only passes when there's no fallback
// socket. On a desktop with an X server this is noisy, so the test is
// scoped: we don't assert it here. We assert the fallback helpers below.
func TestHasDisplay_NoEnvAndNoSockets(t *testing.T) {
	withCleanDisplayEnv(t, func() {
		// Point XDG_RUNTIME_DIR at an empty dir so the Wayland fallback
		// finds nothing. The X11 fallback still checks /tmp/.X11-unix;
		// on a host with an active X server that returns true, which is
		// correct behaviour — the GUI CAN launch. So we only assert the
		// NEGATIVE case on hosts where no X socket exists.
		os.Setenv("XDG_RUNTIME_DIR", t.TempDir())
		if _, err := os.Stat("/tmp/.X11-unix/X0"); os.IsNotExist(err) {
			if hasDisplay() {
				t.Error("expected hasDisplay() to return false when no env vars and no X11/Wayland sockets exist")
			}
		} else {
			t.Skip("X11 socket present on this host; negative test is not applicable")
		}
	})
}

// TestFindX11DisplayNumber_EmptyDir returns (0,false) when no X sockets.
func TestFindX11DisplayNumber_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	if _, ok := findX11DisplayNumber(dir); ok {
		t.Error("expected no display number when dir is empty")
	}
}

// TestFindX11DisplayNumber_PicksLowest verifies we pick the lowest X<N>
// when multiple sockets are present — matches what an X client would do.
func TestFindX11DisplayNumber_PicksLowest(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"X5", "X0", "X2"} {
		f, err := os.Create(filepath.Join(dir, n))
		if err != nil {
			t.Fatalf("create fake socket %s: %v", n, err)
		}
		f.Close()
	}
	got, ok := findX11DisplayNumber(dir)
	if !ok || got != 0 {
		t.Errorf("want 0,true; got %d,%v", got, ok)
	}
}

// TestFindX11DisplayNumber_IgnoresJunk verifies non-X entries are skipped.
func TestFindX11DisplayNumber_IgnoresJunk(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"Xlock", "README", "X2"} {
		f, err := os.Create(filepath.Join(dir, n))
		if err != nil {
			t.Fatalf("create %s: %v", n, err)
		}
		f.Close()
	}
	got, ok := findX11DisplayNumber(dir)
	if !ok || got != 2 {
		t.Errorf("want 2,true; got %d,%v", got, ok)
	}
}

// TestFindWaylandSocket_Basic picks a wayland-<N> socket over .lock files.
func TestFindWaylandSocket_Basic(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"wayland-0", "wayland-0.lock", "other-file"} {
		f, err := os.Create(filepath.Join(dir, n))
		if err != nil {
			t.Fatalf("create %s: %v", n, err)
		}
		f.Close()
	}
	got, ok := findWaylandSocket(dir)
	if !ok || got != "wayland-0" {
		t.Errorf("want wayland-0,true; got %q,%v", got, ok)
	}
}

// TestHasDisplay_X11SocketFallback — the direct regression for the user's
// report: "hasDisplay() returns false even though I have a display."
// Simulate by unsetting env but ensuring /tmp/.X11-unix has a socket;
// hasDisplay should pick up the socket and set DISPLAY=:0.
func TestHasDisplay_X11SocketFallback(t *testing.T) {
	// Only meaningful on hosts that actually have /tmp/.X11-unix/X0.
	if _, err := os.Stat("/tmp/.X11-unix/X0"); os.IsNotExist(err) {
		t.Skip("no /tmp/.X11-unix/X0 on this host; cannot exercise fallback")
	}
	withCleanDisplayEnv(t, func() {
		if !hasDisplay() {
			t.Fatal("expected hasDisplay() to return true via X11 socket fallback when env was stripped but socket exists")
		}
		if got := os.Getenv("DISPLAY"); got == "" {
			t.Error("fallback should have set DISPLAY for downstream Wails process")
		}
	})
}
