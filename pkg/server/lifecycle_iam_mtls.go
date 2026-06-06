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

//go:build rest && iam

package server

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// appendOperatorClientCAsFn is the seam that wires the operator's CA pool (from
// iam.mtls.tls_client_auth_ca_file) into the REST server TLS ClientCAs pool.
// Defined here (rest && iam build) so that PKI-authenticated clients can complete
// the TLS handshake before reaching the token-exchange handler.
var appendOperatorClientCAsFn = func(s *Server, tlsConfig *tls.Config) error {
	return s.appendOperatorClientCAs(tlsConfig)
}

// appendPIVClientCAsFn is the seam that wires the PIV login CA pool (from
// iam.piv_login.ca_file) into the REST server TLS ClientCAs pool.
// When iam.piv_login.enabled is true the TLS listener solicits a client
// certificate from the browser so PIVLogin can read PeerCertificates[0].
var appendPIVClientCAsFn = func(s *Server, tlsConfig *tls.Config) error {
	return s.appendPIVClientCAs(tlsConfig)
}

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
//   - pkiClientAuth is wired only when tls_client_auth_ca_file is set.
//   - TPMAttestationResolver is wired only when at least one root pool file is set.
//   - Missing optional files produce a startup-log warning and a nil authenticator,
//     not a fatal error — the daemon boots successfully without that auth method.
//   - A non-empty file path that cannot be read or parsed IS a fatal error,
//     returned as MTLSConfigError so config typos fail fast at startup.
func (s *Server) wireIAMMTLSAuthenticators() error {
	if s.restServer == nil {
		return nil
	}

	h, ok := s.restServer.IAMHandler().(*restapi.IAMHandler)
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
		if cfg.TLSClientAuthCAFile != "" || cfg.TPMEKRootsFile != "" || cfg.XKeyRootsFile != "" {
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

	// 2. PKI authenticator — wired when tls_client_auth_ca_file is set.
	var pkiAuth iam.MTLSClientAuthenticator
	if cfg.TLSClientAuthCAFile != "" {
		pool, err := loadCertPool(cfg.TLSClientAuthCAFile)
		if err != nil {
			return &qberrors.QBError{
				Code: qberrors.ErrConfiguration,
				Op:   "server.wireIAMMTLSAuthenticators",
				Err:  &MTLSConfigError{Field: "tls_client_auth_ca_file", Cause: err},
			}
		}
		pkiAuth = iam.NewPKIClientAuth(pool, appStorage)
		s.logger.Infof("iam.mtls: tls_client_auth authenticator wired from %s", cfg.TLSClientAuthCAFile)
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
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &CertPoolReadError{Path: path, Cause: err}
	}

	pool := x509.NewCertPool()
	added := 0
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, &CertPoolParseError{Path: path, Cause: err}
		}
		pool.AddCert(cert)
		added++
	}

	if added == 0 {
		return nil, &CertPoolEmptyError{Path: path}
	}

	return pool, nil
}

// CertPoolReadError is returned when a CA pool file cannot be read.
type CertPoolReadError struct {
	Path  string
	Cause error
}

// Error implements the error interface.
func (e *CertPoolReadError) Error() string {
	return "failed to read CA pool file " + e.Path + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *CertPoolReadError) Unwrap() error {
	return e.Cause
}

// CertPoolParseError is returned when a certificate in a CA pool file cannot be parsed.
type CertPoolParseError struct {
	Path  string
	Cause error
}

// Error implements the error interface.
func (e *CertPoolParseError) Error() string {
	return "failed to parse certificate in " + e.Path + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *CertPoolParseError) Unwrap() error {
	return e.Cause
}

// CertPoolEmptyError is returned when a CA pool file contains no certificates.
type CertPoolEmptyError struct {
	Path string
}

// Error implements the error interface.
func (e *CertPoolEmptyError) Error() string {
	return "no certificates found in " + e.Path
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

// PIVClientCAsError is returned when the PIV CA pool file is malformed or
// unreadable during startup. The operator set iam.piv_login.enabled:true with
// a bad ca_file path — that is a fatal misconfiguration.
type PIVClientCAsError struct {
	Cause error
}

// Error implements the error interface.
func (e *PIVClientCAsError) Error() string {
	return "iam.piv_login.ca_file: failed to load PIV CA for REST ClientCAs: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *PIVClientCAsError) Unwrap() error { return e.Cause }

// PIVLoginDisabledError is returned by the PIV handler when a request arrives
// at /api/v1/auth/login/piv but iam.piv_login.enabled is false.
type PIVLoginDisabledError struct{}

// Error implements the error interface.
func (e *PIVLoginDisabledError) Error() string {
	return "iam.piv_login: piv_login_disabled — set iam.piv_login.enabled: true to enable PIV smartcard login"
}

// appendPIVClientCAs extends tlsConfig.ClientCAs with the PIV CA bundle from
// iam.piv_login.ca_file when iam.piv_login.enabled is true.
//
// Behaviour:
//   - If ca_file is empty but enabled is true, VerifyClientCertIfGiven is still
//     set so the browser is asked for a cert. The handler performs the cert
//     chain check against the server CA pool. No error is returned.
//   - If ca_file is set but unreadable or empty, startup fails with PIVClientCAsError.
//   - ClientAuth is set to VerifyClientCertIfGiven (not RequireAnyClientCert)
//     so non-PIV clients are not blocked.
func (s *Server) appendPIVClientCAs(tlsConfig *tls.Config) error {
	if tlsConfig == nil {
		return nil
	}

	// Always set VerifyClientCertIfGiven so the TLS handshake solicits a cert.
	tlsConfig.ClientAuth = tls.VerifyClientCertIfGiven

	caFile := s.config.IAM.PIVLogin.CAFile
	if caFile == "" {
		s.logger.Info("iam.piv_login: enabled without ca_file; using server CA pool for PIV cert validation")
		return nil
	}

	pemData, err := os.ReadFile(caFile)
	if err != nil {
		return &qberrors.QBError{
			Code: qberrors.ErrConfiguration,
			Op:   "server.appendPIVClientCAs",
			Err:  &PIVClientCAsError{Cause: &CertPoolReadError{Path: caFile, Cause: err}},
		}
	}
	if tlsConfig.ClientCAs == nil {
		tlsConfig.ClientCAs = x509.NewCertPool()
	}
	if !tlsConfig.ClientCAs.AppendCertsFromPEM(pemData) {
		return &qberrors.QBError{
			Code: qberrors.ErrConfiguration,
			Op:   "server.appendPIVClientCAs",
			Err:  &PIVClientCAsError{Cause: &CertPoolEmptyError{Path: caFile}},
		}
	}
	s.logger.Infof("iam.piv_login: PIV CA appended to REST ClientCAs from %s", caFile)
	return nil
}

// MTLSClientCAsError is returned when the operator's CA pool file is malformed
// or unreadable. Distinct from MTLSConfigError (which wraps file-load errors for
// individual fields) so callers can distinguish the REST-TLS-level failure from
// the IAM-handler-level failure.
type MTLSClientCAsError struct {
	Cause error
}

// Error implements the error interface.
func (e *MTLSClientCAsError) Error() string {
	return "iam.mtls.tls_client_auth_ca_file: failed to load operator CA for REST ClientCAs: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *MTLSClientCAsError) Unwrap() error { return e.Cause }

// appendOperatorClientCAs extends tlsConfig.ClientCAs with the operator's CA
// bundle when iam.mtls.tls_client_auth_ca_file is set. The TLS listener must
// trust the operator's CA at the handshake level so PKI-authenticated clients
// can present their certificates before reaching the token-exchange handler.
//
// Behaviour:
//   - If tls_client_auth_ca_file is empty, this is a no-op.
//   - If the file is set but unreadable or contains no certificates, startup
//     fails with a typed MTLSClientCAsError (operator typo, not degraded mode).
//   - ClientAuth is set to VerifyClientCertIfGiven so presenting a client cert
//     is optional — the token-exchange handler enforces the requirement for the
//     mTLS-specific grant types.
func (s *Server) appendOperatorClientCAs(tlsConfig *tls.Config) error {
	caFile := s.config.IAM.MTLS.TLSClientAuthCAFile
	if caFile == "" {
		return nil
	}
	pemData, err := os.ReadFile(caFile)
	if err != nil {
		return &qberrors.QBError{
			Code: qberrors.ErrConfiguration,
			Op:   "server.appendOperatorClientCAs",
			Err:  &MTLSClientCAsError{Cause: &CertPoolReadError{Path: caFile, Cause: err}},
		}
	}
	if tlsConfig.ClientCAs == nil {
		tlsConfig.ClientCAs = x509.NewCertPool()
	}
	if !tlsConfig.ClientCAs.AppendCertsFromPEM(pemData) {
		return &qberrors.QBError{
			Code: qberrors.ErrConfiguration,
			Op:   "server.appendOperatorClientCAs",
			Err:  &MTLSClientCAsError{Cause: &CertPoolEmptyError{Path: caFile}},
		}
	}
	tlsConfig.ClientAuth = tls.VerifyClientCertIfGiven
	s.logger.Infof("iam.mtls: operator CA appended to REST ClientCAs from %s", caFile)
	return nil
}
