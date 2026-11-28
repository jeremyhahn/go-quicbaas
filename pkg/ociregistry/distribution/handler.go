// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-trusted-platform.
//
// go-trusted-platform is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package distribution

import (
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/jeremyhahn/go-quicbaas/pkg/ociregistry"
)

// RegistryHandlerConfig is what NewRegistryHandler composes into the /v2/
// surface.
type RegistryHandlerConfig struct {
	// Logger logs the distribution handlers; nil uses slog.Default().
	Logger *slog.Logger
	// Backend stores blobs, manifests and uploads.
	Backend OCIDistBackend
	// Identity returns the caller's workload identity.
	Identity IdentityFunc
	// Auth verifies attestation evidence and holds sessions. It may be
	// nil only when Attestation.RequireAttestation is false; with a nil
	// Auth the challenge route is not mounted.
	Auth *ociregistry.RegistryAuthHandler
	// Attestation says whether the gates require attestation.
	Attestation ociregistry.RegistryAttestationConfig
	// Limits bounds uploads and listings.
	Limits OCIDistLimits
}

// NewRegistryHandler returns the /v2/ surface as one http.Handler: the
// attestation challenge route (when cfg.Auth is set), and the OCI
// distribution routes behind the identity and attestation gates. The
// handler routes on the full request path (/v2/...), so a parent router
// must forward requests to it without stripping the /v2 prefix.
func NewRegistryHandler(cfg RegistryHandlerConfig) (http.Handler, error) {
	if cfg.Backend == nil {
		return nil, &OCIDistBackendRequiredError{}
	}
	gates, err := NewAttestedGates(cfg.Identity, cfg.Auth, cfg.Attestation)
	if err != nil {
		return nil, err
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	root := mux.NewRouter()
	if cfg.Auth != nil {
		if err := RegisterAttestationChallengeRoute(root, cfg.Identity, cfg.Auth); err != nil {
			return nil, err
		}
	}
	if err := RegisterOCIDistRoutes(root, logger, cfg.Backend, gates, cfg.Limits); err != nil {
		return nil, err
	}
	return root, nil
}
