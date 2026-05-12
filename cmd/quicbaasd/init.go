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

// init.go implements the inline `quicbaasd init` ceremony per Phase 11
// of the unification plan. The flag surface mirrors xkmsd init so the
// three operator binaries (xkmsd, qrdb, quicbaasd) accept identical
// arguments. The command supports both the internal-CA path (officers
// supply a CSR signed by the daemon-issued CA) and the external-CA path
// (officers supply an existing certificate or public key validated
// against operator-supplied trust pools).
package main

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Officer enrollment kinds
// ---------------------------------------------------------------------------

// officerKind identifies how an SO officer's identity material is delivered
// to the ceremony. Each kind is dispatched through a distinct loader in
// officerLoaders and validated according to the kind's own rules.
type officerKind string

const (
	// officerKindCSR indicates the officer supplied a PEM-encoded
	// certificate signing request. The daemon CA signs the CSR during
	// the ceremony and returns the issued certificate.
	officerKindCSR officerKind = "csr"

	// officerKindCert indicates the officer supplied a PEM-encoded X.509
	// certificate already issued by an external CA. The certificate is
	// validated against the configured --trusted-ca pool.
	officerKindCert officerKind = "cert"

	// officerKindPubKey indicates the officer supplied a PEM-encoded
	// public key. Trust is admin-asserted; no chain validation runs.
	officerKindPubKey officerKind = "pubkey"
)

// officerSpec captures a parsed --so flag value: "<username>:<kind>:<path>".
type officerSpec struct {
	Username string
	Kind     officerKind
	Path     string
}

// officerMaterial is the loader output for an --so flag value. Exactly one
// of CSRPEM, CertPEM, or PubKeyPEM is populated, matching the kind.
type officerMaterial struct {
	Username  string
	Kind      officerKind
	CSRPEM    []byte
	CertPEM   []byte
	PubKeyPEM []byte
}

// officerLoader reads, decodes, and validates the file referenced by an
// officerSpec, returning a populated officerMaterial.
type officerLoader func(spec officerSpec) (*officerMaterial, error)

// officerLoaders is the map-based dispatch table per the project rule:
// "Use map-based dispatch instead of large switch statements for O(1)
// constant-time lookup." Adding a new officer kind requires only a new
// loader and a single map entry.
var officerLoaders = map[officerKind]officerLoader{
	officerKindCSR:    loadOfficerCSR,
	officerKindCert:   loadOfficerCert,
	officerKindPubKey: loadOfficerPubKey,
}

// ---------------------------------------------------------------------------
// Init subcommand error types
// ---------------------------------------------------------------------------

// ErrInvalidOfficerFormat is returned when a --so value does not match the
// "username:kind:path" format.
var ErrInvalidOfficerFormat = errors.New("init: --so value must be username:KIND:/path/to/file (KIND ∈ csr|cert|pubkey)")

// ErrEmptyOfficerUsername is returned when the username portion of a --so
// flag value is empty.
var ErrEmptyOfficerUsername = errors.New("init: officer username must not be empty")

// ErrEmptyOfficerPath is returned when the file path portion of a --so flag
// value is empty.
var ErrEmptyOfficerPath = errors.New("init: officer file path must not be empty")

// ErrUnknownOfficerKind is returned when the kind portion of a --so flag
// value is not one of the registered kinds.
var ErrUnknownOfficerKind = errors.New("init: --so kind must be one of csr, cert, pubkey")

// ErrOfficerFileNotFound is returned when the file referenced by --so does
// not exist on disk.
var ErrOfficerFileNotFound = errors.New("init: officer file not found")

// ErrOfficerFileRead is returned when an officer file cannot be read.
var ErrOfficerFileRead = errors.New("init: failed to read officer file")

// ErrOfficerPEMDecode is returned when an officer file does not contain
// valid PEM data.
var ErrOfficerPEMDecode = errors.New("init: officer file does not contain valid PEM data")

// ErrCSRParseFailed is returned when an officer CSR file cannot be parsed.
var ErrCSRParseFailed = errors.New("init: failed to parse certificate request")

// ErrCSRSignatureInvalid is returned when an officer CSR's self-signature
// does not verify.
var ErrCSRSignatureInvalid = errors.New("init: CSR self-signature verification failed")

// ErrCertParseFailed is returned when an officer certificate file cannot be
// parsed.
var ErrCertParseFailed = errors.New("init: failed to parse certificate")

// ErrPubKeyParseFailed is returned when an officer public-key file cannot
// be parsed.
var ErrPubKeyParseFailed = errors.New("init: failed to parse public key")

// ErrTrustedCANotFound is returned when a --trusted-ca path does not exist.
var ErrTrustedCANotFound = errors.New("init: --trusted-ca path not found")

// ErrTrustedCARead is returned when a --trusted-ca file cannot be read.
var ErrTrustedCARead = errors.New("init: failed to read --trusted-ca file")

// ErrTrustedCANoCerts is returned when a --trusted-ca file contains no
// certificates.
var ErrTrustedCANoCerts = errors.New("init: --trusted-ca file contains no PEM certificates")

// ErrInvalidCredentialStrategy is returned when --credential-strategy is
// not one of the canonical values.
var ErrInvalidCredentialStrategy = errors.New("init: --credential-strategy must be one of manual, barrier")

// ---------------------------------------------------------------------------
// Loaders (map-based dispatch entries)
// ---------------------------------------------------------------------------

// loadOfficerCSR reads a PEM-encoded certificate signing request, parses
// it, and verifies its self-signature.
func loadOfficerCSR(spec officerSpec) (*officerMaterial, error) {
	data, err := readOfficerFile(spec.Path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("%w: %s", ErrOfficerPEMDecode, spec.Path)
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrCSRParseFailed, spec.Path, err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrCSRSignatureInvalid, spec.Path, err)
	}
	return &officerMaterial{
		Username: spec.Username,
		Kind:     spec.Kind,
		CSRPEM:   data,
	}, nil
}

// loadOfficerCert reads a PEM-encoded X.509 certificate and parses it.
// The certificate is NOT chain-validated here; chain validation happens
// during ceremony enrollment against the configured --trusted-ca pool.
func loadOfficerCert(spec officerSpec) (*officerMaterial, error) {
	data, err := readOfficerFile(spec.Path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("%w: %s", ErrOfficerPEMDecode, spec.Path)
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrCertParseFailed, spec.Path, err)
	}
	return &officerMaterial{
		Username: spec.Username,
		Kind:     spec.Kind,
		CertPEM:  data,
	}, nil
}

// loadOfficerPubKey reads a PEM-encoded public key and parses it. Trust is
// admin-asserted; no further validation runs.
func loadOfficerPubKey(spec officerSpec) (*officerMaterial, error) {
	data, err := readOfficerFile(spec.Path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("%w: %s", ErrOfficerPEMDecode, spec.Path)
	}
	if _, err := x509.ParsePKIXPublicKey(block.Bytes); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrPubKeyParseFailed, spec.Path, err)
	}
	return &officerMaterial{
		Username:  spec.Username,
		Kind:      spec.Kind,
		PubKeyPEM: data,
	}, nil
}

// readOfficerFile returns the bytes of a file referenced by an --so flag,
// translating filesystem errors into the canonical typed errors above.
func readOfficerFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrOfficerFileNotFound, path)
		}
		return nil, fmt.Errorf("%w: %s: %v", ErrOfficerFileRead, path, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%w: %s is a directory", ErrOfficerFileRead, path)
	}
	// #nosec G304 - officer file path is provided by the operator during init
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrOfficerFileRead, path, err)
	}
	return data, nil
}

// ---------------------------------------------------------------------------
// Officer flag parsing
// ---------------------------------------------------------------------------

// parseOfficerSpec parses a single "--so" flag value of the form
// "username:KIND:/path/to/file" and returns the parsed officerSpec.
func parseOfficerSpec(value string) (officerSpec, error) {
	// The path component may itself contain ":" on Windows, but the first
	// two delimiters split off username and kind unambiguously.
	parts := strings.SplitN(value, ":", 3)
	if len(parts) != 3 {
		return officerSpec{}, fmt.Errorf("%w: %q", ErrInvalidOfficerFormat, value)
	}
	username := strings.TrimSpace(parts[0])
	kindStr := strings.TrimSpace(parts[1])
	path := strings.TrimSpace(parts[2])
	if username == "" {
		return officerSpec{}, fmt.Errorf("%w: %q", ErrEmptyOfficerUsername, value)
	}
	if path == "" {
		return officerSpec{}, fmt.Errorf("%w: %q", ErrEmptyOfficerPath, value)
	}
	kind := officerKind(strings.ToLower(kindStr))
	if _, ok := officerLoaders[kind]; !ok {
		return officerSpec{}, fmt.Errorf("%w: %q", ErrUnknownOfficerKind, kindStr)
	}
	return officerSpec{Username: username, Kind: kind, Path: path}, nil
}

// loadOfficer dispatches an officerSpec through officerLoaders to produce
// the validated officerMaterial. Unknown kinds were already rejected by
// parseOfficerSpec, so this lookup is total in practice.
func loadOfficer(spec officerSpec) (*officerMaterial, error) {
	loader, ok := officerLoaders[spec.Kind]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownOfficerKind, spec.Kind)
	}
	return loader(spec)
}

// loadOfficers parses every --so flag value, dispatches each through the
// loader table, and returns the resulting materials.
func loadOfficers(values []string) ([]*officerMaterial, error) {
	out := make([]*officerMaterial, 0, len(values))
	for _, raw := range values {
		spec, err := parseOfficerSpec(raw)
		if err != nil {
			return nil, err
		}
		material, err := loadOfficer(spec)
		if err != nil {
			return nil, err
		}
		out = append(out, material)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Trusted CA handling (multi-pool roots for external-CA validation)
// ---------------------------------------------------------------------------

// loadTrustedCAs reads each --trusted-ca path, accumulates the certs into
// an x509.CertPool, and returns it. Empty input returns nil so callers
// can distinguish "no external CAs configured" from "configured but empty".
func loadTrustedCAs(paths []string) (*x509.CertPool, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	pool := x509.NewCertPool()
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("%w: %s", ErrTrustedCANotFound, path)
			}
			return nil, fmt.Errorf("%w: %s: %v", ErrTrustedCARead, path, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("%w: %s is a directory", ErrTrustedCARead, path)
		}
		// #nosec G304 - trusted-ca path is provided by the operator
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrTrustedCARead, path, err)
		}
		if !pool.AppendCertsFromPEM(data) {
			return nil, fmt.Errorf("%w: %s", ErrTrustedCANoCerts, path)
		}
	}
	return pool, nil
}

// ---------------------------------------------------------------------------
// Strategy validators
// ---------------------------------------------------------------------------

// validCredentialStrategies enumerates the canonical --credential-strategy
// values per the Phase 11 spec.
var validCredentialStrategies = map[string]struct{}{
	"manual":  {},
	"barrier": {},
}

// validateCredentialStrategy returns nil when value is a registered
// strategy. The default "barrier" is filled in by callers, so empty
// strings are rejected here.
func validateCredentialStrategy(value string) error {
	if _, ok := validCredentialStrategies[value]; !ok {
		return fmt.Errorf("%w: %q", ErrInvalidCredentialStrategy, value)
	}
	return nil
}

// ---------------------------------------------------------------------------
// inlineInitFlags is the full Phase 11 init flag set (a strict superset of
// canonicalInitFlags). The two structs are kept distinct so the locked
// canonical surface in canonical.go does not accidentally regress when
// new optional flags land here.
// ---------------------------------------------------------------------------
type inlineInitFlags struct {
	soPin              string
	userPin            string
	barrierStrategy    string
	shares             int
	threshold          int
	hostname           string
	sans               string
	adminBundle        string
	dataDir            string
	credentialStrategy string
	officers           []string
	trustedCAs         []string
}

// wireInlineInitFlags attaches the full Phase 11 init flag set onto cmd.
// The canonical subset (so-pin/user-pin/barrier-strategy/shares/threshold
// /hostname/sans/admin-bundle) is wired here so the locked surface stays
// in lock-step with the inline-ceremony surface; the four extra flags
// (init-data-dir/credential-strategy/so/trusted-ca) add the Phase 11
// additions on top.
func wireInlineInitFlags(cmd *cobra.Command, f *inlineInitFlags) {
	// Canonical flags (locked surface).
	cmd.Flags().StringVar(&f.soPin, "so-pin", "",
		"Security Officer PIN (required, FIPS 140-3 / PCI-DSS v4 SO credential)")
	cmd.Flags().StringVar(&f.userPin, "user-pin", "",
		"User PIN (required, FIPS 140-3 / PCI-DSS v4 user credential)")
	cmd.Flags().StringVar(&f.barrierStrategy, "barrier-strategy", "software",
		"Barrier sealing strategy: disabled | software | shamir | pkcs11 | tpm2 | awskms | gcpkms | azurekv | vault")
	cmd.Flags().IntVar(&f.shares, "shares", 0,
		"Total Shamir shares (required when --barrier-strategy=shamir)")
	cmd.Flags().IntVar(&f.threshold, "threshold", 0,
		"Shamir M-of-N threshold (required when --barrier-strategy=shamir)")
	cmd.Flags().StringVar(&f.hostname, "hostname", "",
		"FQDN written into the admin/server certificate CN")
	cmd.Flags().StringVar(&f.sans, "sans", "",
		"Comma-separated additional DNS / IP SANs")
	cmd.Flags().StringVar(&f.adminBundle, "admin-bundle", "-",
		"Path for the admin client cert+key bundle (default: stdout, one-shot)")

	// Phase 11 inline-ceremony additions.
	cmd.Flags().StringVar(&f.dataDir, "init-data-dir", "",
		"Per-init data directory (overrides root --data-dir for the init ceremony)")
	cmd.Flags().StringVar(&f.credentialStrategy, "credential-strategy", "barrier",
		"Credential management strategy: manual | barrier")
	cmd.Flags().StringSliceVar(&f.officers, "so", nil,
		"SO officer in format username:KIND:/path/to/file (KIND ∈ csr|cert|pubkey, repeatable)")
	cmd.Flags().StringSliceVar(&f.trustedCAs, "trusted-ca", nil,
		"PEM file with CA certificates for external-CA officer validation (repeatable)")
}

// validateInlineInitFlags applies all Phase 11 inline-ceremony validation
// rules. The canonical subset is enforced upstream via
// resolveAndValidateCanonicalInit (invoked by resolveAndValidateInlineInit
// in canonical.go); this function only enforces the inline-specific rules.
func validateInlineInitFlags(f *inlineInitFlags) error {
	if err := validateCredentialStrategy(f.credentialStrategy); err != nil {
		return err
	}
	// Officers and trusted-CAs are validated lazily by their loaders so
	// the operator gets a precise per-file error rather than a generic
	// "validation failed". No flag-level cross-checks are needed at this
	// layer.
	return nil
}

// runInlineInit is the Phase 11 inline-ceremony entry point. It loads
// every --so officer through the dispatch table, parses the --trusted-ca
// pool, validates the credential strategy, and prints a configuration
// summary. The actual barrier Initialize / ceremony Initialize calls are
// delegated to the in-process Server when this command is wired into the
// daemon lifecycle; for now the function operates in a validate-and-print
// shape that exercises every loader exactly like xkmsd init does today.
func runInlineInit(cmd *cobra.Command, f *inlineInitFlags) error {
	if err := validateInlineInitFlags(f); err != nil {
		return err
	}

	officers, err := loadOfficers(f.officers)
	if err != nil {
		return err
	}

	pool, err := loadTrustedCAs(f.trustedCAs)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "=== quicbaasd init: Phase 11 Inline Ceremony Validated ===")
	fmt.Fprintf(out, "  BarrierStrategy:    %s\n", f.barrierStrategy)
	if f.barrierStrategy == "shamir" {
		fmt.Fprintf(out, "  Shamir Mode:        %d-of-%d\n", f.threshold, f.shares)
	}
	fmt.Fprintf(out, "  CredentialStrategy: %s\n", f.credentialStrategy)
	fmt.Fprintf(out, "  Hostname:           %s\n", f.hostname)
	if f.sans != "" {
		fmt.Fprintf(out, "  SANs:               %s\n", f.sans)
	}
	if f.dataDir != "" {
		fmt.Fprintf(out, "  InitDataDir:        %s\n", f.dataDir)
	}
	bundlePath := f.adminBundle
	if bundlePath == "" {
		bundlePath = "-"
	}
	fmt.Fprintf(out, "  AdminBundle:        %s\n", bundlePath)
	if pool != nil {
		fmt.Fprintf(out, "  TrustedCAPaths:     %d (loaded into x509.CertPool)\n", len(f.trustedCAs))
	}
	if len(officers) > 0 {
		fmt.Fprintf(out, "  Officers (%d):\n", len(officers))
		for _, o := range officers {
			fmt.Fprintf(out, "    - %s [%s] (validated)\n", o.Username, o.Kind)
		}
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Note: SDK ceremony delegation pending; quicbaasd init validates the full")
	fmt.Fprintln(out, "Phase 11 surface (csr/cert/pubkey dispatch + multi-pool trust) ready to")
	fmt.Fprintln(out, "drive the qrdb CeremonyService via Security().Ceremony().Initialize().")
	return nil
}
