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

// Package initmarker defines the single on-disk init-done marker format shared
// by the `quicbaasd init` writer (cmd/quicbaasd) and the daemon-side reader
// (pkg/server). There is exactly ONE marker format; both ends import this
// package so the writer and the parser can never drift.
package initmarker

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// FileName is the filename of the marker dropped into the data dir after a
// successful `quicbaasd init`. Its presence is the canonical "this data dir
// has already been initialised" signal.
const FileName = ".quicbaasd_init_done"

// Marker is the on-disk record dropped after a successful init. Operators and
// tooling can inspect it to confirm the init parameters without rerunning the
// command; the daemon reads it at start so the running data-plane barrier
// matches the init decision.
type Marker struct {
	BarrierStrategy    string    `json:"barrier_strategy"`
	CredentialStrategy string    `json:"credential_strategy"`
	Hostname           string    `json:"hostname,omitempty"`
	Shares             int       `json:"shares,omitempty"`
	Threshold          int       `json:"threshold,omitempty"`
	CompletedAt        time.Time `json:"completed_at"`
}

// WriteError describes a failure persisting the init-done marker.
type WriteError struct {
	Stage string
	Cause error
}

// Error implements the error interface.
func (e *WriteError) Error() string {
	if e.Cause == nil {
		return "init marker: " + e.Stage + " failed"
	}
	return "init marker: " + e.Stage + " failed: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is/As traversal.
func (e *WriteError) Unwrap() error { return e.Cause }

// ReadError describes a failure reading or parsing the init-done marker.
type ReadError struct {
	Stage string
	Cause error
}

// Error implements the error interface.
func (e *ReadError) Error() string {
	if e.Cause == nil {
		return "init marker: " + e.Stage + " failed"
	}
	return "init marker: " + e.Stage + " failed: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is/As traversal.
func (e *ReadError) Unwrap() error { return e.Cause }

// Path returns the marker's location inside dataDir.
func Path(dataDir string) string {
	return filepath.Join(dataDir, FileName)
}

// Exists reports whether dataDir already holds an init-done marker.
func Exists(dataDir string) bool {
	if dataDir == "" {
		return false
	}
	_, err := os.Stat(Path(dataDir))
	return err == nil
}

// Write persists the marker into dataDir with 0600 permissions using an
// atomic rename so an interrupted init cannot leave a half-written marker
// that fools the next invocation.
func Write(dataDir string, m *Marker) error {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return &WriteError{Stage: "marshal", Cause: err}
	}
	target := Path(dataDir)
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return &WriteError{Stage: "write " + tmp, Cause: err}
	}
	if err := os.Rename(tmp, target); err != nil {
		return &WriteError{Stage: "rename to " + target, Cause: err}
	}
	return nil
}

// Read loads the marker from dataDir. It returns (nil, nil) when no marker is
// present (a fresh, never-initialised data dir) so callers can fall back to
// their config default. A malformed marker surfaces a ReadError so the
// operator sees a precise failure rather than a silent fall-through to the
// wrong strategy.
func Read(dataDir string) (*Marker, error) {
	if dataDir == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(Path(dataDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, &ReadError{Stage: "read", Cause: err}
	}
	var m Marker
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, &ReadError{Stage: "parse", Cause: err}
	}
	return &m, nil
}
