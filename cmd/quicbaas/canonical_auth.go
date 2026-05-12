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
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// canonicalValidFactorTypes is the canonical "--type" set per
// docs/08-bootstrap-auth-architecture.md §5e.
var canonicalValidFactorTypes = map[string]struct{}{
	"mtls":  {},
	"fido2": {},
	"piv":   {},
}

// canonicalValidAuthMethods is the canonical "--method" set per
// docs/08-bootstrap-auth-architecture.md §5e.
var canonicalValidAuthMethods = map[string]struct{}{
	"mtls":  {},
	"fido2": {},
	"piv":   {},
}

// ---------------------------------------------------------------------------
// Typed errors
// ---------------------------------------------------------------------------

// MissingFactorIDError is returned when a factor subcommand requires
// a positional <factor-id> argument and none was supplied.
type MissingFactorIDError struct {
	Op string
}

// Error returns a human-readable description.
func (e *MissingFactorIDError) Error() string {
	return e.Op + ": factor id is required"
}

// MissingLabelError is returned when --label is required but missing.
type MissingLabelError struct {
	Op string
}

// Error returns a human-readable description.
func (e *MissingLabelError) Error() string {
	return e.Op + ": --label is required"
}

// InvalidFactorTypeError is returned when --type is not one of the
// canonical factor types.
type InvalidFactorTypeError struct {
	Type string
}

// Error returns a human-readable description.
func (e *InvalidFactorTypeError) Error() string {
	return "invalid --type " + e.Type + " (must be one of: mtls, fido2, piv)"
}

// InvalidAuthMethodError is returned when --method is not one of the
// canonical auth methods.
type InvalidAuthMethodError struct {
	Method string
}

// Error returns a human-readable description.
func (e *InvalidAuthMethodError) Error() string {
	return "invalid --method " + e.Method + " (must be one of: mtls, fido2, piv)"
}

// MissingCertPathError is returned when "--type mtls" is selected but
// --cert was not supplied.
type MissingCertPathError struct{}

// Error returns a human-readable description.
func (e *MissingCertPathError) Error() string {
	return "auth factor add --type mtls: --cert <path> is required"
}

// CertReadError wraps errors raised while reading or parsing a cert
// PEM file.
type CertReadError struct {
	Path  string
	Cause error
}

// Error returns a human-readable description.
func (e *CertReadError) Error() string {
	if e.Path == "" {
		return "cert read error: " + e.Cause.Error()
	}
	return "cert read error (" + e.Path + "): " + e.Cause.Error()
}

// Unwrap returns the wrapped cause for errors.Is/As.
func (e *CertReadError) Unwrap() error { return e.Cause }

// SecuritySurfaceUnavailableError is returned when the SDK client does
// not implement the M10 ExtendedSecurityService surface (older server
// builds). The handlers expect Security().Auth().Factors() to be
// reachable.
type SecuritySurfaceUnavailableError struct {
	Op string
}

// Error returns a human-readable description.
func (e *SecuritySurfaceUnavailableError) Error() string {
	return e.Op + ": quicbaas SDK Security().Auth().Factors() surface is unavailable (server too old?)"
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

// canonicalAuthFIDO2 resolves the FIDO2 sub-client.
func canonicalAuthFIDO2(op string) (quicbaas.FIDO2Client, error) {
	cl, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	auth := cl.Security().Auth()
	if auth == nil {
		return nil, &SecuritySurfaceUnavailableError{Op: op}
	}
	f := auth.FIDO2()
	if f == nil {
		return nil, &SecuritySurfaceUnavailableError{Op: op}
	}
	return f, nil
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
	fmt.Fprintf(out, "User:          %s\n", resp.UserID)
	if resp.TenantID != "" {
		fmt.Fprintf(out, "Tenant:        %s\n", resp.TenantID)
	}
	fmt.Fprintf(out, "Enrolled At:   %s\n", resp.EnrolledAt.Format("2006-01-02T15:04:05Z"))
	if f.label != "" {
		fmt.Fprintf(out, "Label:         %s\n", f.label)
	}
	return nil
}

// runCanonicalAuthFactorRemove revokes an enrollment.
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

// runCanonicalAuthFactorRotate rotates a factor atomically.
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
		fido2, err := canonicalAuthFIDO2("auth factor add --type fido2")
		if err != nil {
			return nil, err
		}
		// Build a vendor-agnostic registration request. The deviceID
		// scope is the only flag-driven input; the SDK selects the
		// first connected authenticator when empty.
		regReq := &quicbaas.FIDO2BeginRegistrationRequest{}
		regResp, err := fido2.Register(ctx, regReq)
		if err != nil {
			return nil, fmt.Errorf("fido2.Register: %w", err)
		}
		params["fido2_credential_create_options"] = regResp
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
// returns selected metadata.
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
	meta := map[string]any{
		"cert_cn":         cert.Subject.CommonName,
		"cert_serial":     cert.SerialNumber.String(),
		"cert_not_after":  cert.NotAfter.UTC().Format("2006-01-02T15:04:05Z"),
		"cert_not_before": cert.NotBefore.UTC().Format("2006-01-02T15:04:05Z"),
	}
	return fingerprint, meta, nil
}

// keep the strings import in scope (used elsewhere in this file).
var _ = strings.Builder{}
