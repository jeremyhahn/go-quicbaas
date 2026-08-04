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
	"errors"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// This file registers the build-tag-free TPM 2.0 backend factory. TPM2 is always
// compiled in (no cgo, no external library), mirroring the tag-free TPM2 seal
// strategy in seal_factories.go.

// buildTPM2ServiceBackend constructs the TPM 2.0 ServiceBackend from
// cfg.Seal.TPM2. It composes go-xkms: a tpm2backend.Backend is built from the
// operator config and, on a freshly-reset / emulated TPM (ErrNotInitialized),
// provisioned via the shared provisionTPM2Backend helper before being wrapped
// as a ServiceBackend. No TPM crypto is reimplemented here.
//
// keyStore is accepted for signature uniformity but not used for key material:
// TPM 2.0 keys live in the TPM and their wrapped blobs persist via
// cfg.Seal.TPM2.KeyDir, not in a go-xkms StorageBackend.
func buildTPM2ServiceBackend(cfg *config.Config, spec BackendSpec, keyStore xkmssdk.StorageBackend) (xkmssdk.ServiceBackend, error) {
	_ = keyStore // TPM keys persist in hardware / KeyDir, not in keyStore.
	tpmCfg := cfg.Seal.TPM2
	backendCfg := &xkmssdk.TPM2BackendConfig{
		Device:          tpmCfg.Device,
		KeyDir:          tpmCfg.KeyDir,
		UseSimulator:    tpmCfg.UseSimulator,
		SimulatorHost:   tpmCfg.SimulatorHost,
		SimulatorPort:   tpmCfg.SimulatorPort,
		SRKHandle:       tpmCfg.SRKHandle,
		PlatformPolicy:  tpmCfg.PlatformPolicy,
		PlatformPCR:     tpmCfg.PCR,
		PlatformPCRBank: tpmCfg.PCRBank,
	}

	backend, err := xkmssdk.NewTPM2Backend(backendCfg)
	if err != nil {
		if !errors.Is(err, xkmssdk.ErrTPM2BackendNotInitialized) {
			return nil, &BackendConstructError{Backend: config.BackendTypeTPM2, Cause: err}
		}
		// First-boot path: provision the uninitialised TPM and retry. Shared
		// with the TPM2 seal strategy (provisionTPM2Backend in seal_factories.go).
		backend, err = provisionTPM2Backend(backendCfg)
		if err != nil {
			return nil, &BackendConstructError{Backend: config.BackendTypeTPM2, Cause: err}
		}
	}
	return wrapServiceBackend(config.BackendTypeTPM2, backend)
}

// init registers the TPM2 factory unconditionally.
func init() {
	registerBackendFactory(config.BackendTypeTPM2, buildTPM2ServiceBackend)
}
