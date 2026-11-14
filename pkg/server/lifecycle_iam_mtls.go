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

//go:build rest

package server

import (
	"crypto/x509"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// MTLSConfigError is returned when an operator-provided mTLS CA file cannot
// be loaded. It includes the configuration field name so operators can quickly
// locate the offending entry in their configuration file.
type MTLSConfigError struct {
	Field string
	Cause error
}

// Error implements the error interface.
func (e *MTLSConfigError) Error() string {
	return "iam.mtls." + e.Field + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *MTLSConfigError) Unwrap() error {
	return e.Cause
}

// wireIAMMTLSAuthenticators loads operator-provided CA pools from the config
// and wires them into the IAMHandler on the REST server. It is called from
// startRESTServer after the REST server and IAM subsystem are both ready.
//
// Startup behaviour:
//   - selfSignedClientAuth is always wired; it uses the IAM app store directly.
//     A nil IAM components or nil AppStorage causes a hard startup failure —
//     the operator explicitly enabled mTLS, so a misconfiguration must be surfaced.
//   - pkiClientAuth is wired only when security.tls_ca_file yields client
//     trust anchors; it trusts the same set the TLS handshake verifies against.
//   - TPMAttestationResolver is wired only when at least one root pool file is set.
//   - Missing optional files produce a startup-log warning and a nil authenticator,
//     not a fatal error — the daemon boots successfully without that auth method.
//   - A non-empty file path that cannot be read or parsed IS a fatal error,
//     returned as MTLSConfigError so config typos fail fast at startup.
func (s *Server) wireIAMMTLSAuthenticators() error {
	restServer := s.getRESTServer()
	if restServer == nil {
		return nil
	}

	h, ok := restServer.IAMHandler().(*restapi.IAMHandler)
	if !ok || h == nil {
		// IAM handler not wired — nothing to do.
		return nil
	}

	cfg := s.config.IAM.MTLS

	// Resolve AppStorage from the stored IAMComponents reference.
	// This eliminates the non-deterministic runtime assertion on quicBaaSService.
	// A nil iamComponents at this point means the IAM subsystem failed to
	// initialize, which is a hard error when mTLS config is present.
	var appStorage iam.AppStorage
	if s.iamComponents != nil {
		appStorage = s.iamComponents.GetAppStorage()
	}
	if appStorage == nil {
		if s.clientTrustAnchors != nil || cfg.TPMEKRootsFile != "" || cfg.XKeyRootsFile != "" {
			return &qberrors.QBError{
				Code: qberrors.ErrConfiguration,
				Op:   "server.wireIAMMTLSAuthenticators",
				Err:  &MTLSAppStorageError{},
			}
		}
		// No mTLS config and no app storage: nothing to wire.
		return nil
	}

	// 1. Self-signed authenticator — always wired when app storage is available.
	selfSignedAuth := iam.NewSelfSignedClientAuth(appStorage)
	s.logger.Debug("iam.mtls: self_signed_tls_client_auth authenticator wired")

	// 2. PKI authenticator — wired when security.tls_ca_file is set. It
	// trusts the same anchor set the TLS handshake verifies against.
	var pkiAuth iam.MTLSClientAuthenticator
	if pool := s.clientTrustAnchors.certPool(); pool != nil {
		pkiAuth = iam.NewPKIClientAuth(pool, appStorage)
		s.logger.Info("iam.mtls: tls_client_auth authenticator wired to security.tls_ca_file")
	}

	h.SetMTLSAuthenticators(pkiAuth, selfSignedAuth)

	// 3. TPM attestation resolver — wired when at least one roots file is set.
	var tpmPool, xkeyPool *x509.CertPool

	if cfg.TPMEKRootsFile != "" {
		pool, err := loadCertPool(cfg.TPMEKRootsFile)
		if err != nil {
			return &qberrors.QBError{
				Code: qberrors.ErrConfiguration,
				Op:   "server.wireIAMMTLSAuthenticators",
				Err:  &MTLSConfigError{Field: "tpm_ek_roots_file", Cause: err},
			}
		}
		tpmPool = pool
		s.logger.Infof("iam.mtls: TPM EK roots loaded from %s", cfg.TPMEKRootsFile)
	}

	if cfg.XKeyRootsFile != "" {
		pool, err := loadCertPool(cfg.XKeyRootsFile)
		if err != nil {
			return &qberrors.QBError{
				Code: qberrors.ErrConfiguration,
				Op:   "server.wireIAMMTLSAuthenticators",
				Err:  &MTLSConfigError{Field: "xkey_roots_file", Cause: err},
			}
		}
		xkeyPool = pool
		s.logger.Infof("iam.mtls: xkey roots loaded from %s", cfg.XKeyRootsFile)
	}

	if tpmPool != nil || xkeyPool != nil {
		resolver := iam.NewTPMAttestationResolver(iam.TPMConfig{
			EKRoots:   tpmPool,
			XKeyRoots: xkeyPool,
		})
		h.SetTPMAttestationResolver(resolver)
		s.logger.Debug("iam.mtls: TPM attestation resolver wired")
	}

	return nil
}

// loadCertPool reads a PEM file and returns a *x509.CertPool populated with
// all certificate blocks found in the file.
func loadCertPool(path string) (*x509.CertPool, error) {
	certs, err := loadCertificates(path)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	for _, cert := range certs {
		pool.AddCert(cert)
	}
	return pool, nil
}

// MTLSAppStorageError is returned during startup when mTLS authenticators are
// configured but the IAM app storage is not available. This indicates the IAM
// subsystem failed to initialize while mTLS authentication is explicitly enabled,
// which is a fatal misconfiguration rather than a degraded-mode scenario.
type MTLSAppStorageError struct{}

// Error implements the error interface.
func (e *MTLSAppStorageError) Error() string {
	return "iam.mtls: IAM app storage not available — cannot wire mTLS authenticators; ensure IAM subsystem initialized correctly"
}
