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

package initmarker

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWriteReadRoundTrip proves the writer and parser agree on one format.
func TestWriteReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := &Marker{
		BarrierStrategy:    "shamir",
		CredentialStrategy: "barrier",
		Hostname:           "node.example",
		Shares:             3,
		Threshold:          2,
		CompletedAt:        time.Now().UTC().Truncate(time.Second),
	}
	require.NoError(t, Write(dir, in))

	out, err := Read(dir)
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, in.BarrierStrategy, out.BarrierStrategy)
	assert.Equal(t, in.CredentialStrategy, out.CredentialStrategy)
	assert.Equal(t, in.Hostname, out.Hostname)
	assert.Equal(t, in.Shares, out.Shares)
	assert.Equal(t, in.Threshold, out.Threshold)
	assert.True(t, in.CompletedAt.Equal(out.CompletedAt))
}

// TestRead_MissingReturnsNil verifies a fresh data dir reads as (nil, nil).
func TestRead_MissingReturnsNil(t *testing.T) {
	m, err := Read(t.TempDir())
	require.NoError(t, err)
	assert.Nil(t, m)
}

// TestRead_EmptyDataDirReturnsNil verifies an empty dataDir reads as (nil, nil).
func TestRead_EmptyDataDirReturnsNil(t *testing.T) {
	m, err := Read("")
	require.NoError(t, err)
	assert.Nil(t, m)
}

// TestRead_MalformedFailsTyped verifies a corrupt marker surfaces a typed
// ReadError rather than silently falling through.
func TestRead_MalformedFailsTyped(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(Path(dir), []byte("{not json"), 0o600))

	m, err := Read(dir)
	require.Error(t, err)
	assert.Nil(t, m)
	var typed *ReadError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, "parse", typed.Stage)
}

// TestRead_UnreadableFailsTyped covers the read (non-ENOENT) error branch.
func TestRead_UnreadableFailsTyped(t *testing.T) {
	dir := t.TempDir()
	// A directory at the marker path yields a read error that is not ENOENT.
	require.NoError(t, os.Mkdir(Path(dir), 0o750))

	_, err := Read(dir)
	require.Error(t, err)
	var typed *ReadError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, "read", typed.Stage)
}

// TestWrite_UnwritableFailsTyped covers the write error branch.
func TestWrite_UnwritableFailsTyped(t *testing.T) {
	err := Write("/proc/nonexistent/dir", &Marker{BarrierStrategy: "none"})
	require.Error(t, err)
	var typed *WriteError
	require.ErrorAs(t, err, &typed)
}

// TestWrite_AtomicRename verifies no temp file survives a successful write.
func TestWrite_AtomicRename(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, Write(dir, &Marker{BarrierStrategy: "none"}))
	assert.NoFileExists(t, Path(dir)+".tmp")
	assert.FileExists(t, Path(dir))
}

// TestExists reports marker presence.
func TestExists(t *testing.T) {
	dir := t.TempDir()
	assert.False(t, Exists(dir))
	assert.False(t, Exists(""))
	require.NoError(t, Write(dir, &Marker{BarrierStrategy: "none"}))
	assert.True(t, Exists(dir))
}

// TestPath joins the canonical filename.
func TestPath(t *testing.T) {
	assert.Equal(t, filepath.Join("/x", FileName), Path("/x"))
}

// TestErrorMessages verifies both typed errors render descriptive text.
func TestErrorMessages(t *testing.T) {
	assert.Contains(t, (&WriteError{Stage: "marshal"}).Error(), "marshal")
	assert.Contains(t, (&ReadError{Stage: "parse", Cause: os.ErrInvalid}).Error(), "parse")
	assert.ErrorIs(t, (&ReadError{Stage: "read", Cause: os.ErrPermission}).Unwrap(), os.ErrPermission)
	assert.ErrorIs(t, (&WriteError{Stage: "write", Cause: os.ErrPermission}).Unwrap(), os.ErrPermission)
}
