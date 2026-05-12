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
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// canonicalAuthLoginOptions consolidates the canonical "auth login"
// flag set so the cobra builder and run handler stay aligned.
type canonicalAuthLoginOptions struct {
	method         string
	factorID       string
	username       string
	clientCertPath string
	device         string
	rpid           string
	reader         string
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
// Typed errors
// ---------------------------------------------------------------------------

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

// NoFIDO2DeviceError is returned when --method fido2 has no
// authenticator connected.
type NoFIDO2DeviceError struct{}

// Error returns a human-readable description.
func (e *NoFIDO2DeviceError) Error() string {
	return "auth login: no FIDO2 authenticator connected"
}

// AmbiguousFIDO2DeviceError is returned when --method fido2 sees more
// than one authenticator and the operator did not pass --device.
type AmbiguousFIDO2DeviceError struct {
	Devices []string
}

// Error returns a human-readable description.
func (e *AmbiguousFIDO2DeviceError) Error() string {
	return "auth login --method fido2: multiple authenticators connected (" +
		strings.Join(e.Devices, ", ") + "); pass --device <id>"
}

// FIDO2DeviceNotFoundError is returned when --device matches no
// authenticator.
type FIDO2DeviceNotFoundError struct {
	DeviceID string
}

// Error returns a human-readable description.
func (e *FIDO2DeviceNotFoundError) Error() string {
	return "auth login --method fido2: device " + e.DeviceID + " not found"
}

// AmbiguousPIVReaderError is returned when --method piv sees more than
// one reader and the operator did not pass --reader.
type AmbiguousPIVReaderError struct {
	Readers []string
}

// Error returns a human-readable description.
func (e *AmbiguousPIVReaderError) Error() string {
	return "auth login --method piv: multiple PC/SC readers connected (" +
		strings.Join(e.Readers, ", ") + "); pass --reader <name>"
}

// PIVReaderNotFoundError is returned when --reader matches no PC/SC
// reader.
type PIVReaderNotFoundError struct {
	Name string
}

// Error returns a human-readable description.
func (e *PIVReaderNotFoundError) Error() string {
	return "auth login --method piv: reader " + e.Name + " not found"
}

// PIVPINRequiredError is returned when --method piv requires a PIN but
// none was supplied.
type PIVPINRequiredError struct{}

// Error returns a human-readable description.
func (e *PIVPINRequiredError) Error() string {
	return "auth login --method piv: User PIN is required (set --pin or QUICBAAS_USER_PIN)"
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
a connected FIDO2 device, then for a PC/SC PIV reader. Passing
--method skips auto-discovery.

Per docs/08-bootstrap-auth-architecture.md §5c the login flow is
vendor-agnostic: YubiKey, SoloKey, Nitrokey, TouchID, Windows Hello,
or any CTAP2 / NIST SP 800-73-4 device works.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCanonicalAuthLoginV2(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.method, "method", "", "Auth method: mtls | fido2 | piv (default: auto-discover)")
	cmd.Flags().StringVar(&opts.factorID, "factor", "", "Enrolled factor id")
	cmd.Flags().StringVar(&opts.username, "username", "", "Username (subject) for login")
	cmd.Flags().StringVar(&opts.clientCertPath, "client-cert", "", "Client certificate PEM path (--method mtls)")
	cmd.Flags().StringVar(&opts.device, "device", "", "FIDO2 device id (--method fido2)")
	cmd.Flags().StringVar(&opts.rpid, "rp-id", "", "WebAuthn relying-party id (--method fido2; default: quicbaas)")
	cmd.Flags().StringVar(&opts.reader, "reader", "", "PC/SC reader name (--method piv)")
	cmd.Flags().StringVar(&opts.slot, "slot", "9a", "PIV slot id (--method piv; default 9a)")
	cmd.Flags().StringVar(&opts.pin, "pin", "", "PIV User PIN (--method piv; falls back to QUICBAAS_USER_PIN)")
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

	var (
		req *quicbaas.AuthFactorLoginRequest
		err error
	)
	switch method {
	case "mtls":
		req, err = buildCanonicalMTLSLogin(ctx, opts)
	case "fido2":
		req, err = buildCanonicalFIDO2Login(ctx, opts)
	case "piv":
		req, err = buildCanonicalPIVLogin(ctx, opts)
	default:
		return &InvalidAuthMethodError{Method: method}
	}
	if err != nil {
		return err
	}

	factors, err := canonicalAuthFactors("auth login")
	if err != nil {
		return err
	}
	resp, err := factors.Login(ctx, req)
	if err != nil {
		return err
	}

	out := os.Stdout
	fmt.Fprintf(out, "User:       %s (%s)\n", resp.Username, resp.UserID)
	fmt.Fprintf(out, "Token:      %s\n", resp.Token)
	fmt.Fprintf(out, "Issued At:  %s\n", resp.IssuedAt.Format("2006-01-02T15:04:05Z"))
	fmt.Fprintf(out, "Expires At: %s\n", resp.ExpiresAt.Format("2006-01-02T15:04:05Z"))

	if opts.saveToken {
		path, werr := writeCanonicalSessionToken(opts.tokenPath, resp.Token)
		if werr != nil {
			return werr
		}
		fmt.Fprintf(out, "Token saved to %s\n", path)
	}
	return nil
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
		if fido, err := canonicalAuthFIDO2("auth login auto-discovery"); err == nil && fido != nil {
			if devs, derr := fido.ListDevices(ctx); derr == nil && devs != nil && len(devs.Devices) > 0 {
				hasFIDO2 = true
			}
		}
		if piv, err := canonicalAuthPIV("auth login auto-discovery"); err == nil && piv != nil {
			if rd, rerr := piv.ListReaders(ctx); rerr == nil && rd != nil && len(rd.Readers) > 0 {
				hasPIV = true
			}
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

// buildCanonicalMTLSLogin gathers an mTLS login proof.
func buildCanonicalMTLSLogin(_ context.Context, opts *canonicalAuthLoginOptions) (*quicbaas.AuthFactorLoginRequest, error) {
	path := resolveCanonicalDefaultClientCert(opts.clientCertPath)
	if path == "" {
		return nil, &MissingClientCertError{}
	}
	fingerprint, certMeta, err := canonicalLoginLoadCertFingerprint(path)
	if err != nil {
		return nil, err
	}
	rawFingerprint, decodeErr := hex.DecodeString(fingerprint)
	if decodeErr != nil {
		return nil, &CanonicalCertReadError{Path: path, Cause: decodeErr}
	}
	meta := stringifyCanonicalMetadata(certMeta)
	meta["credential_sha256"] = fingerprint
	meta["cert_path"] = path
	return &quicbaas.AuthFactorLoginRequest{
		Username:   opts.username,
		FactorName: "mtls",
		FactorID:   opts.factorID,
		Proof:      rawFingerprint,
		Metadata:   meta,
	}, nil
}

// buildCanonicalFIDO2Login gathers a FIDO2 assertion proof.
func buildCanonicalFIDO2Login(ctx context.Context, opts *canonicalAuthLoginOptions) (*quicbaas.AuthFactorLoginRequest, error) {
	fido, err := canonicalAuthFIDO2("auth login --method fido2")
	if err != nil {
		return nil, err
	}
	devices, err := fido.ListDevices(ctx)
	if err != nil {
		return nil, &FIDO2OperationError{Op: "ListDevices", Cause: err}
	}
	if devices == nil || len(devices.Devices) == 0 {
		return nil, &NoFIDO2DeviceError{}
	}
	device, derr := selectCanonicalFIDO2Device(devices.Devices, opts.device)
	if derr != nil {
		return nil, derr
	}
	rpid := opts.rpid
	if rpid == "" {
		rpid = "quicbaas"
	}
	assertResp, err := fido.Assert(ctx, &quicbaas.FIDO2BeginAuthenticationRequest{
		Username: opts.username,
		RPID:     rpid,
	})
	if err != nil {
		return nil, &FIDO2OperationError{Op: "Assert", Cause: err}
	}
	if assertResp == nil {
		return nil, &FIDO2OperationError{Op: "Assert", Cause: errors.New("nil assertion response")}
	}
	meta := map[string]string{
		"rp_id":     rpid,
		"device_id": device.DeviceID,
	}
	if len(assertResp.CredentialIDs) > 0 {
		meta["credential_id"] = assertResp.CredentialIDs[0]
	}
	return &quicbaas.AuthFactorLoginRequest{
		Username:   opts.username,
		FactorName: "fido2",
		FactorID:   opts.factorID,
		Proof:      []byte(assertResp.Challenge),
		Metadata:   meta,
	}, nil
}

// buildCanonicalPIVLogin gathers a PIV signature proof.
func buildCanonicalPIVLogin(ctx context.Context, opts *canonicalAuthLoginOptions) (*quicbaas.AuthFactorLoginRequest, error) {
	piv, err := canonicalAuthPIV("auth login --method piv")
	if err != nil {
		return nil, err
	}
	readers, err := piv.ListReaders(ctx)
	if err != nil {
		return nil, &PIVOperationError{Op: "ListReaders", Cause: err}
	}
	if readers == nil || len(readers.Readers) == 0 {
		return nil, &PIVOperationError{Op: "ListReaders", Cause: errors.New("no PC/SC readers detected")}
	}
	reader, derr := selectCanonicalPIVReader(readers.Readers, opts.reader)
	if derr != nil {
		return nil, derr
	}
	slot := opts.slot
	if slot == "" {
		slot = "9a"
	}
	cert, err := piv.GetCertificate(ctx, reader.Name, slot)
	if err != nil {
		return nil, &PIVOperationError{Op: "GetCertificate", Cause: err}
	}
	if cert == nil {
		return nil, &PIVOperationError{Op: "GetCertificate", Cause: errors.New("nil certificate")}
	}
	if opts.pin == "" {
		return nil, &PIVPINRequiredError{}
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return nil, &PIVOperationError{Op: "RandRead", Cause: err}
	}
	digest := sha256.Sum256(nonce)
	signResp, err := piv.Sign(ctx, &quicbaas.PIVSignRequest{
		Reader: reader.Name,
		Slot:   slot,
		PIN:    opts.pin,
		Digest: digest[:],
		Algo:   pivAlgoFromCanonicalCert(cert),
	})
	if err != nil {
		return nil, &PIVOperationError{Op: "Sign", Cause: err}
	}
	if signResp == nil {
		return nil, &PIVOperationError{Op: "Sign", Cause: errors.New("nil signature response")}
	}
	sum := sha256.Sum256(cert.Raw)
	return &quicbaas.AuthFactorLoginRequest{
		Username:   opts.username,
		FactorName: "piv",
		FactorID:   opts.factorID,
		Proof:      signResp.Signature,
		Metadata: map[string]string{
			"reader":            reader.Name,
			"slot":              slot,
			"credential_sha256": hex.EncodeToString(sum[:]),
			"nonce":             hex.EncodeToString(nonce),
			"sign_algo":         signResp.Algo,
			"cert_cn":           cert.Subject.CommonName,
			"cert_serial":       cert.SerialNumber.String(),
		},
	}, nil
}

// ---------------------------------------------------------------------------
// Selection helpers
// ---------------------------------------------------------------------------

// selectCanonicalFIDO2Device picks the device matching deviceID, or
// returns an ambiguity / not-found error.
func selectCanonicalFIDO2Device(devices []quicbaas.FIDO2Device, deviceID string) (*quicbaas.FIDO2Device, error) {
	if deviceID != "" {
		for i := range devices {
			if devices[i].DeviceID == deviceID {
				return &devices[i], nil
			}
		}
		return nil, &FIDO2DeviceNotFoundError{DeviceID: deviceID}
	}
	if len(devices) == 1 {
		return &devices[0], nil
	}
	ids := make([]string, 0, len(devices))
	for i := range devices {
		ids = append(ids, devices[i].DeviceID)
	}
	return nil, &AmbiguousFIDO2DeviceError{Devices: ids}
}

// selectCanonicalPIVReader picks the reader matching name, or returns
// an ambiguity / not-found error.
func selectCanonicalPIVReader(readers []quicbaas.PIVReader, name string) (*quicbaas.PIVReader, error) {
	if name != "" {
		for i := range readers {
			if readers[i].Name == name {
				return &readers[i], nil
			}
		}
		return nil, &PIVReaderNotFoundError{Name: name}
	}
	if len(readers) == 1 {
		return &readers[0], nil
	}
	names := make([]string, 0, len(readers))
	for i := range readers {
		names = append(names, readers[i].Name)
	}
	return nil, &AmbiguousPIVReaderError{Readers: names}
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

// writeCanonicalSessionToken persists the issued JWT to disk. Defaults
// to ~/.quicbaas/session.token when explicit is empty.
func writeCanonicalSessionToken(explicit, token string) (string, error) {
	path := explicit
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", &TokenWriteError{Path: "", Cause: err}
		}
		path = filepath.Join(home, ".quicbaas", "session.token")
	}
	if mkErr := os.MkdirAll(filepath.Dir(path), 0o700); mkErr != nil {
		return "", &TokenWriteError{Path: path, Cause: mkErr}
	}
	if wErr := os.WriteFile(path, []byte(token+"\n"), 0o600); wErr != nil {
		return "", &TokenWriteError{Path: path, Cause: wErr}
	}
	return path, nil
}
