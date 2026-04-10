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
	"testing"
)

func TestHasDisplay_X11(t *testing.T) {
	// Save and restore environment
	origDisplay := os.Getenv("DISPLAY")
	origWayland := os.Getenv("WAYLAND_DISPLAY")
	defer func() {
		os.Setenv("DISPLAY", origDisplay)
		os.Setenv("WAYLAND_DISPLAY", origWayland)
	}()

	os.Setenv("DISPLAY", ":0")
	os.Unsetenv("WAYLAND_DISPLAY")

	if !hasDisplay() {
		t.Error("expected hasDisplay() to return true when DISPLAY is set")
	}
}

func TestHasDisplay_Wayland(t *testing.T) {
	origDisplay := os.Getenv("DISPLAY")
	origWayland := os.Getenv("WAYLAND_DISPLAY")
	defer func() {
		os.Setenv("DISPLAY", origDisplay)
		os.Setenv("WAYLAND_DISPLAY", origWayland)
	}()

	os.Unsetenv("DISPLAY")
	os.Setenv("WAYLAND_DISPLAY", "wayland-0")

	if !hasDisplay() {
		t.Error("expected hasDisplay() to return true when WAYLAND_DISPLAY is set")
	}
}

func TestHasDisplay_NoDisplay(t *testing.T) {
	origDisplay := os.Getenv("DISPLAY")
	origWayland := os.Getenv("WAYLAND_DISPLAY")
	defer func() {
		os.Setenv("DISPLAY", origDisplay)
		os.Setenv("WAYLAND_DISPLAY", origWayland)
	}()

	os.Unsetenv("DISPLAY")
	os.Unsetenv("WAYLAND_DISPLAY")

	if hasDisplay() {
		t.Error("expected hasDisplay() to return false when no display env vars set")
	}
}
