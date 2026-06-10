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

// canonical_auth.go wires the canonical "auth login" / "auth factor"
// subcommands on the quicbaas CLI to the quicbaas SDK
// Security().Auth().Factors() surface (M10) backed by the qrdb User
// N-AuthFactor service (M14). Per
// docs/08-bootstrap-auth-architecture.md §6 the quicbaas CLI exposes
// the same shape as qrdb / xkmsctl / platformctl.
package main

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/spf13/cobra"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	xkmsfido2 "github.com/jeremyhahn/go-xkms/sdk/go/fido2"
)

// canonicalValidFactorTypes is the canonical "--type" set per
// docs/08-bootstrap-auth-architecture.md §5e.
var canonicalValidFactorTypes = map[string]struct{}{
	"mtls":  {},
	"fido2": {},
	"piv":   {},
}

// canonicalValidAuthMethods is the canonical "auth login --method" set. It
// mirrors canonicalValidFactorTypes: each factor type is also a login method.
var canonicalValidAuthMethods = map[string]struct{}{
	"mtls":  {},
	"fido2": {},
	"piv":   {},
}

// InvalidAuthMethodError is returned when "auth login --method" is empty or
// not one of the canonical login methods.
type InvalidAuthMethodError struct {
	Method string
}

// Error returns a human-readable description.
func (e *InvalidAuthMethodError) Error() string {
	methods := make([]string, 0, len(canonicalValidAuthMethods))
	for m := range canonicalValidAuthMethods {
		methods = append(methods, m)
	}
	sort.Strings(methods)
	return fmt.Sprintf("invalid auth method %q: must be one of %s",
		e.Method, strings.Join(methods, " | "))
}

// ---------------------------------------------------------------------------
// Typed errors
// ---------------------------------------------------------------------------

// MissingFactorIDError is returned when a factor subcommand requires a
// positional <factor-id> argument that was not supplied.
type MissingFactorIDError struct {
	Op string
}

// Error returns a human-readable description.
func (e *MissingFactorIDError) Error() string {
	return e.Op + ": a factor id argument is required"
}

// MissingLabelError is returned when --label is required but unset.
type MissingLabelError struct {
	Op string
}

// Error returns a human-readable description.
func (e *MissingLabelError) Error() string {
	return e.Op + ": --label is required"
}

// InvalidFactorTypeError is returned when --type is empty or not one of
// the canonical factor types.
type InvalidFactorTypeError struct {
	Type string
}

// Error returns a human-readable description.
func (e *InvalidFactorTypeError) Error() string {
	return fmt.Sprintf("invalid factor type %q: must be one of mtls | fido2 | piv", e.Type)
}

// MissingCertPathError is returned when --type mtls is selected without
// --cert.
type MissingCertPathError struct{}

// Error returns a human-readable description.
func (e *MissingCertPathError) Error() string {
	return "auth factor add --type mtls: --cert <path> is required"
}

// CertReadError wraps a failure to read or parse a PEM certificate.
type CertReadError struct {
	Path  string
	Cause error
}

// Error returns a human-readable description.
func (e *CertReadError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("cert read error (%s): %v", e.Path, e.Cause)
	}
	return fmt.Sprintf("cert read error: %v", e.Cause)
}

// Unwrap returns the wrapped cause.
func (e *CertReadError) Unwrap() error { return e.Cause }

// SecuritySurfaceUnavailableError is returned when the SDK Security()
// surface (or a required sub-service) is nil for the requested operation.
type SecuritySurfaceUnavailableError struct {
	Op string
}

// Error returns a human-readable description.
func (e *SecuritySurfaceUnavailableError) Error() string {
	return e.Op + ": Security() surface unavailable on the configured SDK client"
}

// ---------------------------------------------------------------------------
// Flag struct
// ---------------------------------------------------------------------------

// canonicalAuthFactorAddFlags consolidates the type-agnostic +
// type-specific "auth factor add" / "auth factor rotate" flag set.
type canonicalAuthFactorAddFlags struct {
	factorType  string
	label       string
	user        string
	tenant      string
	certPath    string
	fido2Device string
	fido2Origin string
	pivReader   string
	pivSlot     string
}

// ---------------------------------------------------------------------------
// Builders
// ---------------------------------------------------------------------------

// newCanonicalAuthFactorListCmd implements "auth factor list [--user]".
func newCanonicalAuthFactorListCmd() *cobra.Command {
	var user string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List enrolled factors for the active user",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCanonicalAuthFactorList(cmd.Context(), user)
		},
	}
	cmd.Flags().StringVar(&user, "user", "", "Filter by user/subject (admin)")
	return cmd
}

// newCanonicalAuthFactorAddCmd implements "auth factor add".
func newCanonicalAuthFactorAddCmd() *cobra.Command {
	flags := canonicalAuthFactorAddFlags{}
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Enroll a new factor",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCanonicalAuthFactorAdd(cmd.Context(), &flags)
		},
	}
	bindCanonicalAuthFactorAddFlags(cmd, &flags)
	return cmd
}

// newCanonicalAuthFactorRemoveCmd implements "auth factor remove
// <factor-id> [--force]".
func newCanonicalAuthFactorRemoveCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "remove <factor-id>",
		Short: "Remove an enrolled factor",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return &MissingFactorIDError{Op: "auth factor remove"}
			}
			return runCanonicalAuthFactorRemove(cmd.Context(), args[0], force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Allow removing the last factor (admin)")
	return cmd
}

// newCanonicalAuthFactorRotateCmd implements "auth factor rotate
// <factor-id> --type <t> --label <name>".
func newCanonicalAuthFactorRotateCmd() *cobra.Command {
	flags := canonicalAuthFactorAddFlags{}
	cmd := &cobra.Command{
		Use:   "rotate <factor-id>",
		Short: "Rotate an enrolled factor",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return &MissingFactorIDError{Op: "auth factor rotate"}
			}
			return runCanonicalAuthFactorRotate(cmd.Context(), args[0], &flags)
		},
	}
	bindCanonicalAuthFactorAddFlags(cmd, &flags)
	return cmd
}

// newCanonicalAuthFactorRelabelCmd implements "auth factor relabel
// <factor-id> --label <name>".
func newCanonicalAuthFactorRelabelCmd() *cobra.Command {
	var label string
	cmd := &cobra.Command{
		Use:   "relabel <factor-id>",
		Short: "Relabel an enrolled factor",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return &MissingFactorIDError{Op: "auth factor relabel"}
			}
			if label == "" {
				return &MissingLabelError{Op: "auth factor relabel"}
			}
			return runCanonicalAuthFactorRelabel(cmd.Context(), args[0], label)
		},
	}
	cmd.Flags().StringVar(&label, "label", "", "New human-readable label")
	return cmd
}

// newCanonicalAuthLoginCmd builds the canonical "auth login" command.
// The full per-method implementation lives in canonical_auth_login.go;
// this thin wrapper preserves the registered command name on the auth
// group while delegating to the V2 builder.
func newCanonicalAuthLoginCmd() *cobra.Command {
	return newCanonicalAuthLoginCmdV2()
}

// bindCanonicalAuthFactorAddFlags wires the canonical add/rotate flag
// set onto a cobra command.
func bindCanonicalAuthFactorAddFlags(cmd *cobra.Command, f *canonicalAuthFactorAddFlags) {
	cmd.Flags().StringVar(&f.factorType, "type", "", "Factor type: mtls | fido2 | piv")
	cmd.Flags().StringVar(&f.label, "label", "", "Human-readable factor label (required)")
	cmd.Flags().StringVar(&f.user, "user", "", "Subject/user (default: current authenticated user)")
	cmd.Flags().StringVar(&f.tenant, "tenant", "", "Tenant ID (optional)")
	cmd.Flags().StringVar(&f.certPath, "cert", "", "Certificate PEM path (--type mtls)")
	cmd.Flags().StringVar(&f.fido2Device, "device", "", "FIDO2 device id (--type fido2; default first connected)")
	cmd.Flags().StringVar(&f.fido2Origin, "origin", "", "WebAuthn origin (--type fido2; default: https://<rp-id>)")
	cmd.Flags().StringVar(&f.pivReader, "reader", "", "PC/SC reader name (--type piv)")
	cmd.Flags().StringVar(&f.pivSlot, "slot", "9a", "PIV slot id (--type piv; default 9a)")
}

// ---------------------------------------------------------------------------
// SDK client helpers
// ---------------------------------------------------------------------------

// canonicalAuthFactors resolves the M10 ExtendedSecurityService surface
// from the configured SDK client and returns its AuthFactorsClient.
func canonicalAuthFactors(op string) (quicbaas.AuthFactorsClient, error) {
	cl, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	auth := cl.Security().Auth()
	if auth == nil {
		return nil, &SecuritySurfaceUnavailableError{Op: op}
	}
	factors := auth.Factors()
	if factors == nil {
		return nil, &SecuritySurfaceUnavailableError{Op: op}
	}
	return factors, nil
}

// canonicalAuthService resolves the unified Security().Auth() surface used by
// the canonical login dispatcher to drive the qrdb-backed BeginLogin /
// FinishLogin ceremony for every factor type.
func canonicalAuthService(op string) (quicbaas.AuthService, error) {
	cl, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	auth := cl.Security().Auth()
	if auth == nil {
		return nil, &SecuritySurfaceUnavailableError{Op: op}
	}
	return auth, nil
}

// canonicalAuthWebAuthn resolves the WebAuthn relying-party sub-client used to
// complete the server-side registration ceremony (FinishRegistration).
func canonicalAuthWebAuthn(op string) (quicbaas.WebAuthnClient, error) {
	cl, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	auth := cl.Security().Auth()
	if auth == nil {
		return nil, &SecuritySurfaceUnavailableError{Op: op}
	}
	w := auth.WebAuthn()
	if w == nil {
		return nil, &SecuritySurfaceUnavailableError{Op: op}
	}
	return w, nil
}

// canonicalAuthPIV resolves the PIV sub-client.
func canonicalAuthPIV(op string) (quicbaas.PIVClient, error) {
	cl, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	auth := cl.Security().Auth()
	if auth == nil {
		return nil, &SecuritySurfaceUnavailableError{Op: op}
	}
	p := auth.PIV()
	if p == nil {
		return nil, &SecuritySurfaceUnavailableError{Op: op}
	}
	return p, nil
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// runCanonicalAuthFactorList lists enrolled factors.
func runCanonicalAuthFactorList(ctx context.Context, user string) error {
	factors, err := canonicalAuthFactors("auth factor list")
	if err != nil {
		return err
	}
	resp, err := factors.List(ctx, user)
	if err != nil {
		return err
	}
	out := os.Stdout
	if len(resp.Enrollments) == 0 {
		fmt.Fprintln(out, "(no enrolled factors)")
		return nil
	}
	fmt.Fprintln(out, "ID\tTYPE\tUSER\tTENANT\tENROLLED\tREVOKED")
	for _, e := range resp.Enrollments {
		revoked := "-"
		if e.RevokedAt != nil {
			revoked = e.RevokedAt.Format("2006-01-02T15:04:05Z")
		}
		fmt.Fprintf(out, "%s\t%s\t%s\t%s\t%s\t%s\n",
			e.EnrollmentID,
			e.FactorName,
			e.UserID,
			e.TenantID,
			e.EnrolledAt.Format("2006-01-02T15:04:05Z"),
			revoked,
		)
	}
	return nil
}

// runCanonicalAuthFactorAdd enrolls a new factor.
func runCanonicalAuthFactorAdd(ctx context.Context, f *canonicalAuthFactorAddFlags) error {
	factors, err := canonicalAuthFactors("auth factor add")
	if err != nil {
		return err
	}
	req, err := buildCanonicalEnrollRequest(ctx, f)
	if err != nil {
		return err
	}
	resp, err := factors.Add(ctx, req)
	if err != nil {
		return err
	}
	out := os.Stdout
	fmt.Fprintf(out, "Enrollment ID: %s\n", resp.EnrollmentID)
	fmt.Fprintf(out, "Type:          %s\n", resp.FactorName)
	return nil
}

// runCanonicalAuthFactorRemove removes an enrolled factor.
func runCanonicalAuthFactorRemove(ctx context.Context, factorID string, force bool) error {
	if factorID == "" {
		return &MissingFactorIDError{Op: "auth factor remove"}
	}
	factors, err := canonicalAuthFactors("auth factor remove")
	if err != nil {
		return err
	}
	if err := factors.Remove(ctx, factorID, force); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "OK")
	return nil
}

// runCanonicalAuthFactorRotate revokes an old factor and enrolls a
// replacement in a single operation.
func runCanonicalAuthFactorRotate(ctx context.Context, oldID string, f *canonicalAuthFactorAddFlags) error {
	if oldID == "" {
		return &MissingFactorIDError{Op: "auth factor rotate"}
	}
	factors, err := canonicalAuthFactors("auth factor rotate")
	if err != nil {
		return err
	}
	req, err := buildCanonicalEnrollRequest(ctx, f)
	if err != nil {
		return err
	}
	resp, err := factors.Rotate(ctx, oldID, req)
	if err != nil {
		return err
	}
	out := os.Stdout
	fmt.Fprintf(out, "Old factor revoked: %s\n", oldID)
	fmt.Fprintf(out, "New enrollment ID:  %s\n", resp.EnrollmentID)
	fmt.Fprintf(out, "Type:               %s\n", resp.FactorName)
	return nil
}

// runCanonicalAuthFactorRelabel updates a factor's label.
func runCanonicalAuthFactorRelabel(ctx context.Context, factorID, label string) error {
	factors, err := canonicalAuthFactors("auth factor relabel")
	if err != nil {
		return err
	}
	if err := factors.Relabel(ctx, factorID, label); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "OK")
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// buildCanonicalEnrollRequest validates the canonical add/rotate flag
// set and builds an AuthFactorEnrollRequest with type-specific
// MethodParams. For FIDO2 / PIV the helper invokes the SDK hardware
// sub-clients to produce vendor-agnostic credential payloads.
func buildCanonicalEnrollRequest(ctx context.Context, f *canonicalAuthFactorAddFlags) (*quicbaas.AuthFactorEnrollRequest, error) {
	if f.factorType == "" {
		return nil, &InvalidFactorTypeError{Type: ""}
	}
	if _, ok := canonicalValidFactorTypes[f.factorType]; !ok {
		return nil, &InvalidFactorTypeError{Type: f.factorType}
	}
	if f.label == "" {
		return nil, &MissingLabelError{Op: "auth factor add"}
	}

	params := map[string]any{"label": f.label}

	switch f.factorType {
	case "mtls":
		if f.certPath == "" {
			return nil, &MissingCertPathError{}
		}
		fingerprint, certMeta, err := canonicalLoadCertFingerprint(f.certPath)
		if err != nil {
			return nil, err
		}
		params["credential_sha256"] = fingerprint
		for k, v := range certMeta {
			params[k] = v
		}
	case "fido2":
		credentialID, err := canonicalFIDO2EnrollFunc(ctx, f)
		if err != nil {
			return nil, err
		}
		// credential_id is the base64url credential id the login path uses
		// as the qrdb FIDO2 factor lookup key; the SDK stores it as the
		// factor Credential so a later "auth login --method fido2" resolves
		// it via LookupByCredentialKey.
		params["credential_id"] = credentialID
		if f.fido2Device != "" {
			params["device_id"] = f.fido2Device
		}
	case "piv":
		piv, err := canonicalAuthPIV("auth factor add --type piv")
		if err != nil {
			return nil, err
		}
		if f.pivReader == "" {
			readers, err := piv.ListReaders(ctx)
			if err != nil {
				return nil, fmt.Errorf("piv.ListReaders: %w", err)
			}
			if len(readers.Readers) == 0 {
				return nil, errors.New("auth factor add --type piv: no PC/SC readers detected")
			}
			f.pivReader = readers.Readers[0].Name
		}
		cert, err := piv.GetCertificate(ctx, f.pivReader, f.pivSlot)
		if err != nil {
			return nil, fmt.Errorf("piv.GetCertificate: %w", err)
		}
		sum := sha256.Sum256(cert.Raw)
		params["credential_sha256"] = hex.EncodeToString(sum[:])
		spkiSum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
		params["spki_sha256"] = hex.EncodeToString(spkiSum[:])
		params["reader"] = f.pivReader
		params["slot"] = f.pivSlot
		params["cert_serial"] = cert.SerialNumber.String()
		params["cert_cn"] = cert.Subject.CommonName
		params["cert_not_after"] = cert.NotAfter.UTC().Format("2006-01-02T15:04:05Z")
	}

	return &quicbaas.AuthFactorEnrollRequest{
		FactorName:   f.factorType,
		Subject:      f.user,
		TenantID:     f.tenant,
		MethodParams: params,
	}, nil
}

// canonicalLoadCertFingerprint reads a PEM certificate from path,
// computes a hex-encoded SHA-256 fingerprint of the DER bytes, and
// returns selected metadata. The metadata includes "spki_sha256" — the hex
// SHA-256 of the certificate's SubjectPublicKeyInfo — which the SDK promotes
// into the factor's renewal-stable SPKI lookup credential.
func canonicalLoadCertFingerprint(path string) (string, map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, &CertReadError{Path: path, Cause: err}
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", nil, &CertReadError{Path: path, Cause: errors.New("no CERTIFICATE block found")}
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", nil, &CertReadError{Path: path, Cause: err}
	}
	sum := sha256.Sum256(cert.Raw)
	fingerprint := hex.EncodeToString(sum[:])
	spkiSum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	meta := map[string]any{
		"spki_sha256":     hex.EncodeToString(spkiSum[:]),
		"cert_cn":         cert.Subject.CommonName,
		"cert_serial":     cert.SerialNumber.String(),
		"cert_not_before": cert.NotBefore.UTC().Format("2006-01-02T15:04:05Z"),
		"cert_not_after":  cert.NotAfter.UTC().Format("2006-01-02T15:04:05Z"),
	}
	return fingerprint, meta, nil
}

// ---------------------------------------------------------------------------
// FIDO2 enrollment ceremony
// ---------------------------------------------------------------------------

// FIDO2RegistrationParseError is returned when the server-issued WebAuthn
// creation options cannot be parsed into a registration request, or when the
// authenticator's attestation cannot be parsed to recover the credential id.
type FIDO2RegistrationParseError struct {
	Cause error
}

// Error returns a human-readable description.
func (e *FIDO2RegistrationParseError) Error() string {
	return "auth factor add --type fido2: failed to parse WebAuthn registration data: " + e.Cause.Error()
}

// Unwrap returns the wrapped cause.
func (e *FIDO2RegistrationParseError) Unwrap() error { return e.Cause }

// MissingFIDO2RegistrationOptionsError is returned when the server's
// BeginRegistration response carries no creation options. This means the server
// did not issue a WebAuthn registration challenge (e.g. the tenant relying party
// is not configured) and the local authenticator cannot be driven.
type MissingFIDO2RegistrationOptionsError struct{}

// Error returns a human-readable description.
func (e *MissingFIDO2RegistrationOptionsError) Error() string {
	return "auth factor add --type fido2: server did not issue WebAuthn creation options"
}

// canonicalFIDO2RegisterFunc drives a connected CTAP2 authenticator through a
// WebAuthn registration ceremony whose creation options were issued by the
// server, and returns the navigator.credentials.create() PublicKeyCredential
// JSON the server's FinishRegistration consumes.
//
// It is a package-level seam so the device-free enrollment plumbing (options
// parsing, attestation -> credential-id extraction, MethodParams assembly) can
// be unit-tested without a physical authenticator. The production default
// drives a real HID authenticator over CTAP2 (canonicalFIDO2RegisterWithOptions);
// tests replace it with a function that returns a fixed attestation. The seam
// never bypasses CTAP2 in production code.
var canonicalFIDO2RegisterFunc = canonicalFIDO2RegisterWithOptions

// canonicalFIDO2RegisterWithOptions is the production implementation behind
// canonicalFIDO2RegisterFunc. It parses the server's
// PublicKeyCredentialCreationOptions, builds a real FIDO2 handler over the
// platform HID enumerator (the same construction xkmsctl's RegisterWebAuthn
// uses), and drives the connected authenticator with the server-issued
// registration challenge.
//
// devicePath, when non-empty, pins a specific local HID device; when empty the
// handler selects the first connected FIDO2 authenticator. origin, when
// non-empty, overrides the WebAuthn-standard same-origin default
// ("https://<rpId>") for relying parties whose origin host differs from the RP
// id.
func canonicalFIDO2RegisterWithOptions(_ context.Context, optionsJSON []byte, devicePath, origin string) ([]byte, error) {
	var creation protocol.CredentialCreation
	if err := json.Unmarshal(optionsJSON, &creation); err != nil {
		return nil, &FIDO2RegistrationParseError{Cause: err}
	}

	rpID := creation.Response.RelyingParty.ID
	if origin == "" {
		origin = "https://" + rpID
	}
	userID, err := decodeCanonicalWebAuthnUserID(creation.Response.User.ID)
	if err != nil {
		return nil, &FIDO2RegistrationParseError{Cause: err}
	}

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

	att, rerr := handler.RegisterWebAuthn(xkmsfido2.WebAuthnRegistrationOptions{
		Challenge:       creation.Response.Challenge,
		Origin:          origin,
		RPID:            rpID,
		RPName:          creation.Response.RelyingParty.Name,
		UserID:          userID,
		UserName:        creation.Response.User.Name,
		UserDisplayName: creation.Response.User.DisplayName,
	})
	if rerr != nil {
		return nil, &FIDO2OperationError{Op: "Register", Cause: rerr}
	}
	return att.PublicKeyCredentialJSON, nil
}

// canonicalFIDO2Registerer is the device-side registration seam signature:
// given the server creation-options JSON, an optional device path, and an
// optional WebAuthn origin, it returns the PublicKeyCredential attestation JSON.
type canonicalFIDO2Registerer func(ctx context.Context, optionsJSON []byte, devicePath, origin string) ([]byte, error)

// canonicalFIDO2EnrollFunc resolves the FIDO2/WebAuthn SDK sub-clients and drives
// the full registration ceremony, returning the base64url credential id stored
// under MethodParams["credential_id"]. It is a package-level seam so the
// buildCanonicalEnrollRequest credential-id propagation can be unit-tested
// without faking the entire SDK Security() surface; production callers use the
// default (canonicalFIDO2Enroll), which resolves live transport-backed clients.
var canonicalFIDO2EnrollFunc = canonicalFIDO2Enroll

// canonicalFIDO2Enroll is the production implementation behind
// canonicalFIDO2EnrollFunc. It resolves the WebAuthn relying-party client (the
// single canonical ceremony surface) and drives the registration ceremony via
// enrollCanonicalFIDO2.
func canonicalFIDO2Enroll(ctx context.Context, f *canonicalAuthFactorAddFlags) (string, error) {
	webauthn, err := canonicalAuthWebAuthn("auth factor add --type fido2")
	if err != nil {
		return "", err
	}
	return enrollCanonicalFIDO2(ctx, f, webauthn, canonicalFIDO2RegisterFunc)
}

// enrollCanonicalFIDO2 drives the full WebAuthn registration ceremony for a
// FIDO2 factor enrollment and returns the base64url credential id used as the
// login lookup key:
//
//  1. webauthn.BeginRegistration issues the server-side creation options +
//     session id.
//  2. register drives the connected authenticator over CTAP2 with those options
//     to obtain the navigator.credentials.create() attestation JSON.
//  3. webauthn.FinishRegistration completes the server-side ceremony with the
//     attestation under the issued session id.
//  4. The credential id is recovered from the attestation's "rawId" — the same
//     base64url encoding the login path's lookup key uses — and returned so the
//     enroll request can store it as MethodParams["credential_id"].
func enrollCanonicalFIDO2(
	ctx context.Context,
	f *canonicalAuthFactorAddFlags,
	webauthn quicbaas.WebAuthnClient,
	register canonicalFIDO2Registerer,
) (string, error) {
	beginResp, err := webauthn.BeginRegistration(ctx, &quicbaas.WebAuthnUser{
		Username:    f.user,
		DisplayName: f.label,
	})
	if err != nil {
		return "", fmt.Errorf("webauthn.BeginRegistration: %w", err)
	}
	if beginResp == nil || beginResp.OptionsJSON == "" {
		return "", &MissingFIDO2RegistrationOptionsError{}
	}

	attestation, err := register(ctx, []byte(beginResp.OptionsJSON), f.fido2Device, f.fido2Origin)
	if err != nil {
		return "", err
	}

	credentialID, err := canonicalFIDO2CredentialIDFromAttestation(attestation)
	if err != nil {
		return "", err
	}

	if _, err := webauthn.FinishRegistration(ctx, &quicbaas.WebAuthnUser{
		Username:    f.user,
		DisplayName: f.label,
	}, &quicbaas.FIDO2FinishRegistrationRequest{
		SessionID:       beginResp.SessionID,
		AttestationJSON: string(attestation),
	}); err != nil {
		return "", fmt.Errorf("webauthn.FinishRegistration: %w", err)
	}

	return credentialID, nil
}

// canonicalFIDO2CredentialIDFromAttestation recovers the base64url-encoded
// WebAuthn credential id from a navigator.credentials.create() attestation
// document. The credential id is the qrdb FIDO2 factor lookup key, and uses the
// exact same encoding the login path derives from the assertion's rawId, so a
// later "auth login --method fido2" resolves the enrolled factor.
func canonicalFIDO2CredentialIDFromAttestation(attestation []byte) (string, error) {
	var doc struct {
		ID    string `json:"id"`
		RawID string `json:"rawId"`
	}
	if err := json.Unmarshal(attestation, &doc); err != nil {
		return "", &FIDO2RegistrationParseError{Cause: err}
	}
	credentialID := doc.RawID
	if credentialID == "" {
		credentialID = doc.ID
	}
	if credentialID == "" {
		return "", &FIDO2RegistrationParseError{Cause: errors.New("attestation carries no credential id (id/rawId)")}
	}
	return credentialID, nil
}

// decodeCanonicalWebAuthnUserID extracts the WebAuthn user handle bytes from the
// JSON-decoded User.ID field of a CredentialCreation. The go-webauthn library
// serializes the handle as a base64url string, so after JSON decoding it arrives
// as a Go string.
func decodeCanonicalWebAuthnUserID(id any) ([]byte, error) {
	switch v := id.(type) {
	case string:
		return base64.RawURLEncoding.DecodeString(v)
	case []byte:
		return v, nil
	case nil:
		return nil, nil
	default:
		return nil, fmt.Errorf("unexpected user handle type %T", id)
	}
}
