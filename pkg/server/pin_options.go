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

package server

import (
	"log/slog"
	"os"
	"path/filepath"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// pinDirName is the sub-directory of the data dir that holds the persistent
// software PIN records (Argon2id/PBKDF2 hashes). Matches the xKey convention.
const pinDirName = "pin"

// PINBackendError describes a failure constructing the daemon PIN backend.
// Stage names the step that failed; Cause is the underlying error.
type PINBackendError struct {
	Stage string
	Cause error
}

// Error implements the error interface.
func (e *PINBackendError) Error() string {
	if e.Cause == nil {
		return "quicbaas/server: pin backend " + e.Stage + " failed"
	}
	return "quicbaas/server: pin backend " + e.Stage + " failed: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is / errors.As traversal.
func (e *PINBackendError) Unwrap() error { return e.Cause }

// softwarePINProvider supplies the persistent storage + hash parameters for the
// software PIN fallback. When the data dir is empty (tests, no persistent
// backend) it yields a nil store so qrdb constructs an in-memory-only backend.
//
// The software backend is the ONLY PIN backend that persists a hash, and it
// does so with Argon2id/PBKDF2 (salted) under <dataDir>/pin — because it has no
// hardware to delegate verification + lockout to. Hardware backends (TPM2,
// PKCS#11) verify in-device and never persist a hash.
type softwarePINProvider struct {
	dataDir string
}

// Storage opens the persistent PIN store, or returns a nil store (in-memory)
// when no data dir is configured.
func (p softwarePINProvider) Storage() (qrdbsdk.PINStorage, error) {
	if p.dataDir == "" {
		return nil, nil
	}
	pinDir := filepath.Join(p.dataDir, pinDirName)
	if err := os.MkdirAll(pinDir, 0o700); err != nil {
		return nil, &PINBackendError{Stage: "mkdir pin dir", Cause: err}
	}
	fs, err := xkmssdk.NewFileStorageBackend(pinDir)
	if err != nil {
		return nil, &PINBackendError{Stage: "open pin file storage", Cause: err}
	}
	adapter, err := xkmssdk.NewPINStoreAdapter(fs)
	if err != nil {
		return nil, &PINBackendError{Stage: "wrap pin storage adapter", Cause: err}
	}
	return adapter, nil
}

// HashConfig returns the auto-detected hash config (PBKDF2 under GOFIPS140,
// Argon2id otherwise), matching qrdb's software backend default.
func (p softwarePINProvider) HashConfig() qrdbsdk.PINHashConfig {
	return qrdbsdk.AutoDetectHashConfig()
}

// buildPINBackend selects the daemon PIN backend via the shared go-xkms
// pinselect decision point. Today it supplies only the persistent software
// provider; the TPM provider — which shares the active TPM2 seal strategy's
// PlatformKeyStore so the PIN verifies in-hardware with native lockout — is
// wired by the TPM keystore-sharing follow-on. Until then the software backend
// is selected, now PERSISTENT across restarts (previously in-memory only).
func buildPINBackend(cfg *config.Config, logger *slog.Logger) (qrdbsdk.PINBackend, error) {
	if logger == nil {
		logger = slog.Default()
	}
	var dataDir string
	if cfg != nil {
		dataDir = cfg.Storage.DataDir
	}
	backend, err := xkmssdk.SelectPINBackend(xkmssdk.PINSelectConfig{
		Software: softwarePINProvider{dataDir: dataDir},
		Logger:   logger,
	})
	if err != nil {
		return nil, &PINBackendError{Stage: "select pin backend", Cause: err}
	}
	return backend, nil
}
