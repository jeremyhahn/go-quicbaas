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

// canonical_auth_login.go implements the per-method credential
// gathering required by docs/08-bootstrap-auth-architecture.md §5e for
// the canonical "auth login" subcommand. The dispatcher has three
// branches (mTLS / FIDO2 / PIV) plus auto-discovery; vendor-agnostic
// per §5c (NO xkey-specific code paths).
package main

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	xkmsfido2 "github.com/jeremyhahn/go-xkms/sdk/go/fido2"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// canonicalAuthLoginOptions consolidates the canonical "auth login"
// flag set so the cobra builder and run handler stay aligned.
type canonicalAuthLoginOptions struct {
	method         string
	factorID       string
	username       string
	tenantID       string
	clientCertPath string
	device         string
	origin         string
	pivBackend     string
	pivDir         string
	pkcs11Module   string
	pkcs11Token    string
	slot           string
	pin            string
	saveToken      bool
	tokenPath      string
}

// defaultCanonicalClientCertEnvs are the env-var fallbacks probed by
// auto-discovery when --client-cert is not supplied.
var defaultCanonicalClientCertEnvs = []string{
	"QUICBAAS_CLIENT_CERT",
	"PLATFORMCTL_CLIENT_CERT",
	"XKMSCTL_CLIENT_CERT",
	"QRDB_CLIENT_CERT",
}

// defaultCanonicalClientCertPaths are the filesystem fallbacks probed
// by auto-discovery.
var defaultCanonicalClientCertPaths = []string{
	"~/.quicbaas/admin.pem",
	"~/.platformctl/admin.pem",
	"~/.config/platformctl/admin.pem",
}

// ---------------------------------------------------------------------------
// Canonical WebAuthn challenge / proof metadata keys.
//
// These are matched EXACTLY against the keys the server's tenant WebAuthn
// verifier reads and writes:
//
//   - BeginChallenge (go-xkms/pkg/auth_factors/webauthn.go) sets the assertion
//     request options under "options" and the relying-party session id under
//     "session_id" in the BeginLoginResponse.ChallengeMetadata map.
//   - Verify (same file) reads the WebAuthn session id from
//     MethodMetadata["session_id"], the credential id from
//     MethodMetadata["credential_id"], and the assertion JSON either from
//     MethodMetadata["assertion"] or from the FinishLoginRequest.Signature
//     field.
//
// Do NOT rename these without changing the server verifier in lockstep.
// ---------------------------------------------------------------------------.
const (
	// canonicalFIDO2OptionsKey is the ChallengeMetadata key carrying the
	// JSON-marshaled WebAuthn PublicKeyCredentialRequestOptions.
	canonicalFIDO2OptionsKey = "options"

	// canonicalFIDO2SessionIDKey is the ChallengeMetadata/MethodMetadata key
	// carrying the relying-party WebAuthn session id.
	canonicalFIDO2SessionIDKey = "session_id"

	// canonicalFIDO2CredentialIDKey is the MethodMetadata key carrying the
	// base64url-encoded WebAuthn credential id the assertion was produced with.
	canonicalFIDO2CredentialIDKey = "credential_id"
)

// ---------------------------------------------------------------------------
// Typed errors
// ---------------------------------------------------------------------------

// MissingLoginTenantError is returned when the login ceremony is started
// without a tenant scope. The tenant comes from the global --tenant flag,
// which defaults to the qrdb system tenant, so an empty value means the
// operator explicitly cleared it.
type MissingLoginTenantError struct{}

// Error returns a human-readable description.
func (e *MissingLoginTenantError) Error() string {
	return "auth login: tenant is required (set --tenant; the login relying party is per-tenant)"
}

// MissingClientCertError is returned when --method mtls cannot resolve
// a client certificate from --client-cert or any well-known fallback.
type MissingClientCertError struct{}

// Error returns a human-readable description.
func (e *MissingClientCertError) Error() string {
	return "auth login: client certificate is required for --method mtls (set --client-cert or place a cert at ~/.quicbaas/admin.pem)"
}

// NoFactorsAvailableError is returned by auto-discovery when nothing
// is reachable from the client environment.
type NoFactorsAvailableError struct{}

// Error returns a human-readable description.
func (e *NoFactorsAvailableError) Error() string {
	return "auth login: no auth method available (no client cert, no FIDO2 device, no PIV reader)"
}

// AmbiguousAutoDiscoveryError is returned by auto-discovery when more
// than one factor type is reachable.
type AmbiguousAutoDiscoveryError struct {
	Methods []string
}

// Error returns a human-readable description.
func (e *AmbiguousAutoDiscoveryError) Error() string {
	return "auth login: multiple auth methods available (" +
		strings.Join(e.Methods, ", ") + "); pass --method to pick one"
}

// InvalidPIVBackendError is returned when --piv-backend is not one of the
// go-xkms PIV backends (file | pkcs11 | tpm2).
type InvalidPIVBackendError struct {
	Backend string
}

// Error returns a human-readable description.
func (e *InvalidPIVBackendError) Error() string {
	return "invalid --piv-backend " + strconv.Quote(e.Backend) +
		": must be one of file | pkcs11 | tpm2"
}

// PIVLocalBackendUnsupportedError is returned when the selected local PIV
// backend cannot be used by this binary (missing build tag or an upstream
// go-xkms capability gap).
type PIVLocalBackendUnsupportedError struct {
	Backend string
	Reason  string
}

// Error returns a human-readable description.
func (e *PIVLocalBackendUnsupportedError) Error() string {
	return "piv backend " + e.Backend + " is unavailable: " + e.Reason
}

// PIVLocalSessionError wraps failures from the client-local PIV session
// (storage construction, certificate reads, nonce signing).
type PIVLocalSessionError struct {
	Op    string
	Cause error
}

// Error returns a human-readable description.
func (e *PIVLocalSessionError) Error() string {
	return "local piv: " + e.Op + " failed: " + e.Cause.Error()
}

// Unwrap returns the wrapped cause.
func (e *PIVLocalSessionError) Unwrap() error { return e.Cause }

// Sentinel causes for PIVLocalSessionError.
var (
	errHomeDirUnavailable = errors.New("cannot resolve home directory")
	errXKMSServiceInUse   = errors.New("the in-process xkms service is already initialized by another component; local PIV cannot share it")
	errEmptyPIVSignature  = errors.New("empty signature")
	//nolint:unused // referenced only by the pkcs11-tagged build (piv_local_pkcs11.go)
	errPKCS11ModuleRequired = errors.New("--pkcs11-module is required for the pkcs11 backend")
)

// PIVPINRequiredError is returned when --method piv on the pkcs11 backend
// requires a PIN but none was supplied.
type PIVPINRequiredError struct{}

// Error returns a human-readable description.
func (e *PIVPINRequiredError) Error() string {
	return "auth login --method piv: PKCS#11 User PIN is required (set --pin or QUICBAAS_USER_PIN)"
}

// FIDO2OperationError wraps errors from FIDO2 hardware operations.
type FIDO2OperationError struct {
	Op    string
	Cause error
}

// Error returns a human-readable description.
func (e *FIDO2OperationError) Error() string {
	return "auth login --method fido2: " + e.Op + " failed: " + e.Cause.Error()
}

// Unwrap returns the wrapped cause.
func (e *FIDO2OperationError) Unwrap() error { return e.Cause }

// MissingFIDO2ChallengeError is returned when the BeginLogin response for a
// FIDO2 login carries no WebAuthn assertion options. This means the server did
// not issue a WebAuthn challenge (e.g. the tenant WebAuthn relying party is not
// configured) and the local authenticator cannot be driven.
type MissingFIDO2ChallengeError struct{}

// Error returns a human-readable description.
func (e *MissingFIDO2ChallengeError) Error() string {
	return "auth login --method fido2: server did not issue a WebAuthn challenge (no " +
		canonicalFIDO2OptionsKey + " in begin-login metadata)"
}

// FIDO2AssertionParseError is returned when the WebAuthn assertion produced by
// the authenticator cannot be parsed to recover the credential id used as the
// factor lookup key.
type FIDO2AssertionParseError struct {
	Cause error
}

// Error returns a human-readable description.
func (e *FIDO2AssertionParseError) Error() string {
	return "auth login --method fido2: failed to parse WebAuthn assertion: " + e.Cause.Error()
}

// Unwrap returns the wrapped cause.
func (e *FIDO2AssertionParseError) Unwrap() error { return e.Cause }

// PIVOperationError wraps errors from PIV hardware operations.
type PIVOperationError struct {
	Op    string
	Cause error
}

// Error returns a human-readable description.
func (e *PIVOperationError) Error() string {
	return "auth login --method piv: " + e.Op + " failed: " + e.Cause.Error()
}

// Unwrap returns the wrapped cause.
func (e *PIVOperationError) Unwrap() error { return e.Cause }

// CanonicalCertReadError is the typed error returned when a PEM
// certificate cannot be read or parsed during login.
type CanonicalCertReadError struct {
	Path  string
	Cause error
}

// Error returns a human-readable description.
func (e *CanonicalCertReadError) Error() string {
	if e.Path == "" {
		return "auth login: cert read error: " + e.Cause.Error()
	}
	return "auth login: cert read error (" + e.Path + "): " + e.Cause.Error()
}

// Unwrap returns the wrapped cause.
func (e *CanonicalCertReadError) Unwrap() error { return e.Cause }

// TokenWriteError wraps errors raised while writing the issued session
// token to disk.
type TokenWriteError struct {
	Path  string
	Cause error
}

// Error returns a human-readable description.
func (e *TokenWriteError) Error() string {
	return "auth login: failed to write session token to " + e.Path + ": " + e.Cause.Error()
}

// Unwrap returns the wrapped cause.
func (e *TokenWriteError) Unwrap() error { return e.Cause }

// ---------------------------------------------------------------------------
// Login command builder (replaces the legacy newCanonicalAuthLoginCmd).
// ---------------------------------------------------------------------------

// newCanonicalAuthLoginCmdV2 builds the new "auth login" command with
// the full per-method flag set. The legacy newCanonicalAuthLoginCmd is
// retained for backwards compatibility but is replaced at root wire-up
// time.
func newCanonicalAuthLoginCmdV2() *cobra.Command {
	opts := &canonicalAuthLoginOptions{}
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate the active session",
		Long: `Authenticate using one of the enrolled factors.

Auto-discovery probes for a client cert at the default path, then for
a connected FIDO2 device, then for a certificate in the local PIV
backend slot (--piv-backend / --piv-dir / --slot). Passing --method
skips auto-discovery.

Per docs/08-bootstrap-auth-architecture.md §5c the login flow is
vendor-agnostic: YubiKey, SoloKey, Nitrokey, TouchID, Windows Hello,
or any CTAP2 / NIST SP 800-73-4 device works.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// The login ceremony is tenant-scoped (see canonicalLoginTenant);
			// the scope comes from the global --tenant flag, so no per-command
			// tenant flag is defined.
			opts.tenantID = globalTenantID
			return runCanonicalAuthLoginV2(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.method, "method", "", "Auth method: mtls | fido2 | piv (default: auto-discover)")
	cmd.Flags().StringVar(&opts.factorID, "factor", "", "Enrolled factor id")
	cmd.Flags().StringVar(&opts.username, "username", "", "Username (subject) for login")
	cmd.Flags().StringVar(&opts.clientCertPath, "client-cert", "", "Client certificate PEM path (--method mtls)")
	cmd.Flags().StringVar(&opts.device, "device", "", "FIDO2 local HID device path (--method fido2; default: first connected authenticator)")
	// No --rp-id flag: the relying-party id is NOT a client choice. It is
	// carried by the server-issued PublicKeyCredentialRequestOptions the
	// begin step returns, and the authenticator binds the assertion to it.
	cmd.Flags().StringVar(&opts.origin, "origin", "",
		"WebAuthn origin (--method fido2; default: https://<server-issued rpId>)")
	cmd.Flags().StringVar(&opts.pivBackend, "piv-backend", localPIVBackendFile, "Local PIV backend: file | pkcs11 | tpm2 (--method piv)")
	cmd.Flags().StringVar(&opts.pivDir, "piv-dir", defaultLocalPIVDir, "Local PIV key/certificate directory (--method piv, file backend)")
	cmd.Flags().StringVar(&opts.pkcs11Module, "pkcs11-module", "", "PKCS#11 library path (--method piv, pkcs11 backend)")
	cmd.Flags().StringVar(&opts.pkcs11Token, "pkcs11-token", "", "PKCS#11 token label (--method piv, pkcs11 backend)")
	cmd.Flags().StringVar(&opts.slot, "slot", "9a", "PIV slot id (--method piv; default 9a)")
	cmd.Flags().StringVar(&opts.pin, "pin", "", "PKCS#11 User PIN (--method piv, pkcs11 backend; falls back to QUICBAAS_USER_PIN)")
	cmd.Flags().BoolVar(&opts.saveToken, "save-token", false, "Persist the issued session token to --token-path")
	cmd.Flags().StringVar(&opts.tokenPath, "token-path", "", "Where to write the session token (default: ~/.quicbaas/session.token)")
	return cmd
}

// runCanonicalAuthLoginV2 runs the canonical login flow.
func runCanonicalAuthLoginV2(ctx context.Context, opts *canonicalAuthLoginOptions) error {
	if opts == nil {
		opts = &canonicalAuthLoginOptions{}
	}
	resolveCanonicalAuthLoginEnv(opts)

	method := opts.method
	if method == "" {
		discovered, derr := autoDiscoverCanonicalAuthMethod(ctx, opts)
		if derr != nil {
			return derr
		}
		method = discovered
	}
	if _, ok := canonicalValidAuthMethods[method]; !ok {
		return &InvalidAuthMethodError{Method: method}
	}

	factorType, ftErr := canonicalFactorType(method)
	if ftErr != nil {
		return ftErr
	}
	auth, err := canonicalAuthService("auth login")
	if err != nil {
		return err
	}

	// 1. Begin the unified login ceremony. qrdb's LoginClient issues the
	//    method-specific challenge via the registered factor authenticator
	//    (PIV/mtls server nonce, WebAuthn RP options). The tenant scope is
	//    mandatory for WebAuthn — the relying party is per-tenant.
	tenantID, tErr := canonicalLoginTenant(opts.tenantID)
	if tErr != nil {
		return tErr
	}
	begin, err := auth.BeginLogin(ctx, qrdbtransport.BeginLoginRequest{
		Type:     factorType,
		Hint:     opts.username,
		TenantID: tenantID,
	})
	if err != nil {
		return err
	}

	// 2. Produce the proof from the challenge using the local factor device.
	var (
		lookupKey     []byte
		altLookupKeys [][]byte
		signature     []byte
		meta          map[string]string
	)
	switch method {
	case "mtls":
		// The server derives the mtls lookup keys (SPKI-first, cert.Raw
		// fallback) from the VERIFIED peer certificate; the client value is
		// advisory at best.
		lookupKey, signature, meta, err = proveCanonicalMTLS(ctx, opts, begin)
	case "fido2":
		lookupKey, signature, meta, err = proveCanonicalFIDO2(ctx, opts, begin)
	case "piv":
		lookupKey, altLookupKeys, signature, meta, err = proveCanonicalPIV(ctx, opts, begin)
	default:
		return &InvalidAuthMethodError{Method: method}
	}
	if err != nil {
		return err
	}

	// 3. Finish: the server verifies the proof against the challenge via the
	//    registered factor authenticator, then issues the bearer.
	resp, err := auth.FinishLogin(ctx, qrdbtransport.FinishLoginRequest{
		SessionID:      begin.SessionID,
		Type:           factorType,
		LookupKey:      lookupKey,
		AltLookupKeys:  altLookupKeys,
		Signature:      signature,
		MethodMetadata: meta,
	})
	if err != nil {
		return err
	}

	out := os.Stdout
	var subject, tenant string
	if resp.Identity != nil {
		subject = resp.Identity.Subject
		tenant = resp.Identity.TenantID
	}
	fmt.Fprintf(out, "User:       %s\n", subject)
	if tenant != "" {
		fmt.Fprintf(out, "Tenant:     %s\n", tenant)
	}
	fmt.Fprintf(out, "Token:      %s\n", resp.Bearer)
	fmt.Fprintf(out, "Expires At: %s\n", resp.ExpiresAt.Format("2006-01-02T15:04:05Z"))

	if opts.saveToken {
		// The refresh credential is persisted alongside the bearer: without it
		// every command run more than 15 minutes after login (notably
		// `quicbaas mcp serve --stdio`) would fail with "authentication
		// required" instead of transparently renewing the session.
		path, werr := writeCanonicalSessionToken(opts.tokenPath, resp.Bearer, resp.RefreshToken, resp.ExpiresAt)
		if werr != nil {
			return werr
		}
		fmt.Fprintf(out, "Token saved to %s\n", path)
	}
	return nil
}

// canonicalFactorType maps a CLI method name to the qrdb transport factor type.
func canonicalFactorType(method string) (qrdbtransport.FactorType, error) {
	switch method {
	case "mtls":
		return qrdbtransport.FactorTypeMTLS, nil
	case "fido2":
		return qrdbtransport.FactorTypeFIDO2, nil
	case "piv":
		return qrdbtransport.FactorTypePIV, nil
	default:
		return "", &InvalidAuthMethodError{Method: method}
	}
}

// canonicalLoginTenant returns the tenant that scopes the login ceremony.
//
// The scope is NOT optional: WebAuthn is relying-party scoped, so the server's
// tenant-aware WebAuthn verifier fails closed when the login carries no tenant
// (pkg/server.EmptyLoginTenantError) and BeginLogin is rejected before any
// authenticator is touched. PIV and mTLS carry the tenant through the login
// session too, so the FinishLogin verifier resolves the same tenant the
// challenge was issued for.
func canonicalLoginTenant(tenantID string) (string, error) {
	if tenantID == "" {
		return "", &MissingLoginTenantError{}
	}
	return tenantID, nil
}

// resolveCanonicalAuthLoginEnv fills in viper-bound env-var values for
// any flag that was not explicitly set.
func resolveCanonicalAuthLoginEnv(opts *canonicalAuthLoginOptions) {
	v := viper.New()
	v.SetEnvPrefix("QUICBAAS")
	v.AutomaticEnv()
	if opts.pin == "" {
		opts.pin = v.GetString("USER_PIN")
	}
	if opts.clientCertPath == "" {
		opts.clientCertPath = v.GetString("CLIENT_CERT")
	}
	if opts.tokenPath == "" {
		opts.tokenPath = v.GetString("TOKEN_PATH")
	}
}

// autoDiscoverCanonicalAuthMethod probes the client environment for an
// available auth method.
func autoDiscoverCanonicalAuthMethod(ctx context.Context, opts *canonicalAuthLoginOptions) (string, error) {
	hasMTLS := resolveCanonicalDefaultClientCert(opts.clientCertPath) != ""

	hasFIDO2, hasPIV := false, false
	if !hasMTLS {
		if dev, err := canonicalAuthDevices("auth login auto-discovery"); err == nil && dev != nil {
			// FIDO2 authenticators are discovered LOCALLY (USB-HID); a server
			// cannot see the keys attached to this client.
			if present, derr := dev.Present(); derr == nil && present {
				hasFIDO2 = true
			}
		}
		if session, serr := newLocalPIVSessionFunc(localPIVConfigFromLogin(opts)); serr == nil && session != nil {
			// PIV material is discovered LOCALLY: a certificate present in
			// the configured slot of the local go-xkms PIV backend means a
			// PIV login is possible.
			slot := opts.slot
			if slot == "" {
				slot = "9a"
			}
			if cert, cerr := session.Certificate(slot); cerr == nil && cert != nil {
				hasPIV = true
			}
			_ = session.Close()
		}
	}

	switch {
	case hasMTLS:
		return "mtls", nil
	case hasFIDO2 && !hasPIV:
		return "fido2", nil
	case hasPIV && !hasFIDO2:
		return "piv", nil
	case hasFIDO2 && hasPIV:
		return "", &AmbiguousAutoDiscoveryError{Methods: []string{"fido2", "piv"}}
	}
	return "", &NoFactorsAvailableError{}
}

// proveCanonicalMTLS produces the mTLS login proof for the unified ceremony.
// Possession of the client key is proven by the TLS handshake, so there is no
// in-band signature; the lookup key is the certificate SHA-256 that the server
// resolves to the bound user.
func proveCanonicalMTLS(_ context.Context, opts *canonicalAuthLoginOptions, _ *qrdbtransport.BeginLoginResponse) (lookupKey, signature []byte, meta map[string]string, err error) {
	path := resolveCanonicalDefaultClientCert(opts.clientCertPath)
	if path == "" {
		return nil, nil, nil, &MissingClientCertError{}
	}
	fingerprint, certMeta, lerr := canonicalLoginLoadCertFingerprint(path)
	if lerr != nil {
		return nil, nil, nil, lerr
	}
	rawFingerprint, decodeErr := hex.DecodeString(fingerprint)
	if decodeErr != nil {
		return nil, nil, nil, &CanonicalCertReadError{Path: path, Cause: decodeErr}
	}
	meta = stringifyCanonicalMetadata(certMeta)
	meta["credential_sha256"] = fingerprint
	meta["cert_path"] = path
	// No in-band signature: mTLS possession is established by the handshake.
	return rawFingerprint, nil, meta, nil
}

// proveCanonicalPIV signs the SERVER-issued challenge nonce with the local
// PIV slot key. Signing the server nonce (rather than a client-chosen value)
// gives the ceremony replay protection; the qrdb PIV authenticator (go-xkms
// PIVHardwareFactorVerifier) verifies the signature over SHA-256(begin.Nonce)
// against the enrolled certificate.
//
// The card/key interaction is CLIENT-LOCAL: the go-xkms PIV backend selected
// by --piv-backend runs in-process (file | pkcs11 | tpm2), matching xkmsctl's
// embedded mode. The remote Security().Auth().PIV() surface manages the
// server's slots and is not involved in login.
//
// The primary lookup key is the renewal-stable SHA-256 of the slot
// certificate's SubjectPublicKeyInfo; altLookupKeys carries the legacy
// SHA-256(cert DER) so factors enrolled before the SPKI index existed keep
// resolving. The lookup keys only locate the factor — possession is proven
// by the signature the server verifies against the enrolled certificate.
func proveCanonicalPIV(ctx context.Context, opts *canonicalAuthLoginOptions, begin *qrdbtransport.BeginLoginResponse) (lookupKey []byte, altLookupKeys [][]byte, signature []byte, meta map[string]string, err error) {
	session, serr := newLocalPIVSessionFunc(localPIVConfigFromLogin(opts))
	if serr != nil {
		return nil, nil, nil, nil, serr
	}
	defer func() { _ = session.Close() }()

	slot := opts.slot
	if slot == "" {
		slot = "9a"
	}
	cert, cerr := session.Certificate(slot)
	if cerr != nil {
		return nil, nil, nil, nil, &PIVOperationError{Op: "Certificate", Cause: cerr}
	}
	if cert == nil {
		return nil, nil, nil, nil, &PIVOperationError{Op: "Certificate", Cause: errors.New("nil certificate")}
	}
	sig, sgerr := session.SignNonce(ctx, slot, begin.Nonce)
	if sgerr != nil {
		return nil, nil, nil, nil, &PIVOperationError{Op: "SignNonce", Cause: sgerr}
	}
	sum := sha256.Sum256(cert.Raw)
	spkiSum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	meta = map[string]string{
		"backend":     localPIVBackendOrDefault(opts.pivBackend),
		"slot":        slot,
		"sign_algo":   pivAlgoFromCanonicalCert(cert),
		"cert_cn":     cert.Subject.CommonName,
		"cert_serial": cert.SerialNumber.String(),
	}
	return spkiSum[:], [][]byte{sum[:]}, sig, meta, nil
}

// localPIVConfigFromLogin projects the login options onto the local PIV
// session configuration.
func localPIVConfigFromLogin(opts *canonicalAuthLoginOptions) *localPIVConfig {
	return &localPIVConfig{
		backend:      opts.pivBackend,
		dir:          opts.pivDir,
		pin:          opts.pin,
		pkcs11Module: opts.pkcs11Module,
		pkcs11Token:  opts.pkcs11Token,
	}
}

// localPIVBackendOrDefault normalizes an empty --piv-backend to the file
// default for metadata reporting.
func localPIVBackendOrDefault(backend string) string {
	if backend == "" {
		return localPIVBackendFile
	}
	return backend
}

// canonicalFIDO2AssertFunc drives a connected CTAP2 authenticator through a
// WebAuthn assertion ceremony whose options were issued by the server, and
// returns the navigator.credentials.get() assertion JSON the server verifies.
//
// It is a package-level seam so the device-free login plumbing (options
// extraction, lookup-key derivation, metadata assembly) can be unit-tested
// without a physical authenticator. The production default drives a real HID
// authenticator over CTAP2 (canonicalFIDO2AssertWithOptions); tests replace it
// with a function that returns a fixed assertion. The seam never bypasses CTAP2
// in production code.
var canonicalFIDO2AssertFunc = canonicalFIDO2AssertWithOptions

// canonicalFIDO2AssertWithOptions is the production implementation behind
// canonicalFIDO2AssertFunc. It builds a real FIDO2 handler over the platform
// HID enumerator (the same construction xkmsctl uses) and drives the connected
// authenticator with the server-issued WebAuthn request options.
//
// devicePath, when non-empty, pins a specific local HID device; when empty the
// handler selects the first connected FIDO2 authenticator. origin, when
// non-empty, overrides the WebAuthn-standard same-origin default
// ("https://<rpId>") for relying parties whose origin host differs from the RP
// id.
func canonicalFIDO2AssertWithOptions(ctx context.Context, optionsJSON []byte, devicePath, origin string) ([]byte, error) {
	fidoConfig := xkmsfido2.DefaultConfig
	if devicePath != "" {
		fidoConfig.DevicePath = devicePath
	}
	enumerator := xkmsfido2.NewDefaultEnumerator()
	handler, err := xkmsfido2.NewHandler(&fidoConfig, enumerator)
	if err != nil {
		return nil, &FIDO2OperationError{Op: "NewHandler", Cause: err}
	}
	defer func() { _ = handler.Close() }()

	assertion, aerr := handler.AssertWithOptionsAndOrigin(ctx, optionsJSON, origin)
	if aerr != nil {
		return nil, &FIDO2OperationError{Op: "Assert", Cause: aerr}
	}
	return assertion, nil
}

// proveCanonicalFIDO2 produces a WebAuthn assertion over the server-issued
// request options and assembles the proof the unified FinishLogin carries.
//
// The server's tenant WebAuthn verifier (go-xkms/pkg/auth_factors/webauthn.go)
// issued the WebAuthn PublicKeyCredentialRequestOptions under
// begin.ChallengeMetadata["options"] and the relying-party session id under
// begin.ChallengeMetadata["session_id"] from BeginChallenge. This function:
//
//  1. Reads the options JSON; a missing key means the server did not issue a
//     WebAuthn challenge.
//  2. Drives the connected authenticator over CTAP2 with those options to obtain
//     the navigator.credentials.get() assertion JSON.
//  3. Recovers the base64url credential id from the assertion's "rawId" — the
//     qrdb FIDO2 factor lookup key is the credential id (matching how the factor
//     is enrolled and indexed) — and returns:
//     - lookupKey: the credential id bytes, which the qrdb LoginResolver maps
//     to the bound user;
//     - signature: the assertion JSON, which Verify parses (it accepts the
//     assertion via FinishLoginRequest.Signature when MethodMetadata
//     ["assertion"] is absent);
//     - meta: the session id and credential id under the exact keys Verify
//     reads (session_id / credential_id).
func proveCanonicalFIDO2(ctx context.Context, opts *canonicalAuthLoginOptions, begin *qrdbtransport.BeginLoginResponse) (lookupKey, signature []byte, meta map[string]string, err error) {
	optionsJSON, perr := canonicalFIDO2OptionsFromBegin(begin)
	if perr != nil {
		return nil, nil, nil, perr
	}

	assertion, aerr := canonicalFIDO2AssertFunc(ctx, optionsJSON, opts.device, opts.origin)
	if aerr != nil {
		return nil, nil, nil, aerr
	}

	credentialID, cerr := canonicalFIDO2CredentialID(assertion)
	if cerr != nil {
		return nil, nil, nil, cerr
	}

	sessionID := ""
	if begin != nil && begin.ChallengeMetadata != nil {
		sessionID = begin.ChallengeMetadata[canonicalFIDO2SessionIDKey]
	}
	meta = map[string]string{
		canonicalFIDO2SessionIDKey:    sessionID,
		canonicalFIDO2CredentialIDKey: credentialID,
	}
	// The lookup key is the credential id bytes; the proof is the assertion JSON
	// carried in the signature field (Verify reads it there when the assertion
	// metadata key is absent).
	return []byte(credentialID), assertion, meta, nil
}

// canonicalFIDO2OptionsFromBegin extracts the JSON-marshaled WebAuthn request
// options from a BeginLogin response. Returns MissingFIDO2ChallengeError when
// the server did not issue a WebAuthn challenge.
func canonicalFIDO2OptionsFromBegin(begin *qrdbtransport.BeginLoginResponse) ([]byte, error) {
	if begin == nil || begin.ChallengeMetadata == nil {
		return nil, &MissingFIDO2ChallengeError{}
	}
	options := begin.ChallengeMetadata[canonicalFIDO2OptionsKey]
	if options == "" {
		return nil, &MissingFIDO2ChallengeError{}
	}
	return []byte(options), nil
}

// canonicalFIDO2CredentialID recovers the base64url-encoded WebAuthn credential
// id from a navigator.credentials.get() assertion document. The credential id
// is the qrdb FIDO2 factor lookup key.
func canonicalFIDO2CredentialID(assertion []byte) (string, error) {
	var doc struct {
		ID    string `json:"id"`
		RawID string `json:"rawId"`
	}
	if err := json.Unmarshal(assertion, &doc); err != nil {
		return "", &FIDO2AssertionParseError{Cause: err}
	}
	credentialID := doc.RawID
	if credentialID == "" {
		credentialID = doc.ID
	}
	if credentialID == "" {
		return "", &FIDO2AssertionParseError{Cause: errors.New("assertion carries no credential id (id/rawId)")}
	}
	return credentialID, nil
}

// ---------------------------------------------------------------------------
// Selection helpers
// ---------------------------------------------------------------------------

// canonicalAuthDevices resolves the client-LOCAL CTAP2 authenticator helper.
// Device discovery is intrinsically local: a server cannot enumerate the
// USB-HID authenticators attached to this client.
func canonicalAuthDevices(op string) (quicbaas.LocalDevices, error) {
	cl, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	auth := cl.Security().Auth()
	if auth == nil {
		return nil, &SecuritySurfaceUnavailableError{Op: op}
	}
	d := auth.Devices()
	if d == nil {
		return nil, &SecuritySurfaceUnavailableError{Op: op}
	}
	return d, nil
}

// ---------------------------------------------------------------------------
// Cert / metadata helpers
// ---------------------------------------------------------------------------

// resolveCanonicalDefaultClientCert returns the first existing path
// among an explicit override, env-var fallbacks, and well-known
// default locations.
func resolveCanonicalDefaultClientCert(explicit string) string {
	if explicit != "" {
		if _, err := os.Stat(explicit); err == nil {
			return explicit
		}
		return ""
	}
	for _, env := range defaultCanonicalClientCertEnvs {
		if v := os.Getenv(env); v != "" {
			expanded := expandCanonicalHome(v)
			if expanded == "" {
				continue
			}
			if _, err := os.Stat(expanded); err == nil {
				return expanded
			}
		}
	}
	for _, path := range defaultCanonicalClientCertPaths {
		expanded := expandCanonicalHome(path)
		if expanded == "" {
			continue
		}
		if _, err := os.Stat(expanded); err == nil {
			return expanded
		}
	}
	return ""
}

// expandCanonicalHome expands a leading "~/" against the user's home
// directory.
func expandCanonicalHome(path string) string {
	if path == "" {
		return ""
	}
	if path[0] != '~' {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	if path == "~" {
		return home
	}
	if len(path) > 1 && (path[1] == '/' || path[1] == os.PathSeparator) {
		return home + string(os.PathSeparator) + path[2:]
	}
	return path
}

// canonicalLoginLoadCertFingerprint parses a PEM cert at path and
// returns a hex-encoded SHA-256 fingerprint of the DER bytes plus
// selected metadata.
func canonicalLoginLoadCertFingerprint(path string) (string, map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, &CanonicalCertReadError{Path: path, Cause: err}
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", nil, &CanonicalCertReadError{Path: path, Cause: errors.New("no CERTIFICATE block found")}
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", nil, &CanonicalCertReadError{Path: path, Cause: err}
	}
	sum := sha256.Sum256(cert.Raw)
	fingerprint := hex.EncodeToString(sum[:])
	meta := map[string]any{
		"cert_cn":         cert.Subject.CommonName,
		"cert_serial":     cert.SerialNumber.String(),
		"cert_not_after":  cert.NotAfter.UTC().Format("2006-01-02T15:04:05Z"),
		"cert_not_before": cert.NotBefore.UTC().Format("2006-01-02T15:04:05Z"),
	}
	return fingerprint, meta, nil
}

// stringifyCanonicalMetadata flattens map[string]any to map[string]string.
func stringifyCanonicalMetadata(in map[string]any) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		switch t := v.(type) {
		case string:
			out[k] = t
		case []byte:
			out[k] = string(t)
		default:
			out[k] = fmt.Sprint(t)
		}
	}
	return out
}

// pivAlgoFromCanonicalCert returns a PIV signing algorithm name for a
// cert. Defaults to "ecdsa".
func pivAlgoFromCanonicalCert(cert *x509.Certificate) string {
	if cert == nil {
		return "ecdsa"
	}
	switch cert.PublicKeyAlgorithm {
	case x509.RSA:
		return "rsa-pkcs1"
	case x509.Ed25519:
		return "ed25519"
	case x509.ECDSA:
		fallthrough
	default:
		return "ecdsa"
	}
}

// writeCanonicalSessionToken persists the issued JWT to disk in the canonical
// cli.TokenData envelope, so every other command's loadStoredAuthToken can
// consume it. Defaults to cli.GetDefaultTokenPath() when explicit is empty —
// writing a bare JWT to a non-default path produced a token the CLI could not
// read back, so the format and the default path are deliberately shared.
func writeCanonicalSessionToken(explicit, token, refreshToken string, expiresAt time.Time) (string, error) {
	path := explicit
	if path == "" {
		def, err := cli.GetDefaultTokenPath()
		if err != nil {
			return "", &TokenWriteError{Path: "", Cause: err}
		}
		path = def
	}
	if err := cli.SaveToken(path, &cli.TokenData{
		AccessToken:  token,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt,
		TokenType:    "Bearer",
	}); err != nil {
		return "", &TokenWriteError{Path: path, Cause: err}
	}
	return path, nil
}
