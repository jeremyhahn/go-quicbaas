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
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/server"
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

// ErrSecretRequiredForSoftware is returned when --barrier-strategy=passphrase is
// requested without a passphrase. The passphrase barrier is sealed with the
// admin's passphrase at init; there is no deployment-derived default.
var ErrSecretRequiredForSoftware = errors.New("init: --passphrase is required when --barrier-strategy=passphrase")

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
	secret             string
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
	cmd.Flags().StringVar(&f.barrierStrategy, "barrier-strategy", "passphrase",
		"Barrier sealing strategy: disabled | passphrase | shamir | pkcs11 | tpm2 | awskms | gcpkms | azurekv | vault")
	cmd.Flags().StringVar(&f.secret, "passphrase", "",
		"Barrier passphrase for --barrier-strategy=passphrase (REQUIRED for passphrase). The admin keeps it and supplies it to `barrier unseal` on restart; the client never performs init. No deployment-derived default.")
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
	// The passphrase (software) barrier is sealed with the admin's passphrase at
	// init; it is REQUIRED (no deployment-derived default). "none" needs none;
	// "shamir" is initialized through the M-of-N ceremony.
	if isPassphraseStrategy(f.barrierStrategy) && f.secret == "" {
		return ErrSecretRequiredForSoftware
	}
	// Officers and trusted-CAs are validated lazily by their loaders so
	// the operator gets a precise per-file error rather than a generic
	// "validation failed". No flag-level cross-checks are needed at this
	// layer.
	return nil
}

// initDoneMarkerName is the filename of the marker dropped into --data-dir
// after a successful init. Its presence is the canonical "this data dir has
// already been initialised" signal; absence permits the init flow to run.
const initDoneMarkerName = ".quicbaasd_init_done"

// pendingCeremonyFileName is the filename used by the shamir init path to
// persist requested ceremony parameters between init and the daemon's next
// start. The state machine that drives ceremony.claim-cert reads this on
// daemon start to resume the in-flight ceremony.
const pendingCeremonyFileName = "pending_ceremony.json"

// initDoneMarker is the on-disk record dropped after a successful init.
// Operators and tooling can inspect it to confirm the init parameters
// without rerunning the command.
type initDoneMarker struct {
	BarrierStrategy    string    `json:"barrier_strategy"`
	CredentialStrategy string    `json:"credential_strategy"`
	Hostname           string    `json:"hostname,omitempty"`
	OfficerCount       int       `json:"officer_count"`
	Shares             int       `json:"shares,omitempty"`
	Threshold          int       `json:"threshold,omitempty"`
	CompletedAt        time.Time `json:"completed_at"`
}

// pendingCeremonyState is written by the shamir init path so the daemon can
// resume the ceremony on its next start. Officers are stored by username +
// kind only; their submitted material lives alongside the marker as separate
// files (one per officer) referenced by the path field.
type pendingCeremonyState struct {
	Strategy           string                   `json:"strategy"`
	Shares             int                      `json:"shares"`
	Threshold          int                      `json:"threshold"`
	CredentialStrategy string                   `json:"credential_strategy"`
	Officers           []pendingCeremonyOfficer `json:"officers,omitempty"`
	CreatedAt          time.Time                `json:"created_at"`
}

// pendingCeremonyOfficer captures the per-officer state written alongside
// pending_ceremony.json so subsequent ceremony.claim-* invocations can
// resume from the persisted material.
type pendingCeremonyOfficer struct {
	Username string `json:"username"`
	Kind     string `json:"kind"`
	Path     string `json:"path"`
}

// runInlineInit is the canonical "quicbaasd init" entry point. The flow:
//
//  1. Validate flags (including reject PINs under none; reject hardware
//     strategies and any not in supportedBarrierStrategies).
//  2. Resolve the per-init data dir (--init-data-dir overrides the global
//     --data-dir for the init ceremony so test runs can target a tmpdir).
//  3. If the data dir already holds an init-done marker, fail with
//     InitAlreadyDoneError rather than silently re-running.
//  4. Load every --so officer through the dispatch table and the --trusted-ca
//     pool. These calls validate the material BEFORE any state is persisted
//     so an invalid CSR or unparseable PEM aborts the init cleanly.
//  5. For barrier strategies that bring up a running daemon (none, passphrase):
//     construct an embedded Server, call Start (which runs the IAM
//     bootstrap and persists the admin assignment + invite), then Shutdown.
//  6. For shamir: persist a pending_ceremony.json file in the data dir so
//     the operator can complete the ceremony via "quicbaas ceremony
//     claim-cert begin ..." after a subsequent "quicbaasd start".
//  7. Drop the init-done marker and emit a brief operator summary.
//
// All steps treat the data dir as the canonical persistence root. Failure at
// any point leaves the data dir in whatever state it was at — re-running
// init after an interrupted run will see the marker is absent and proceed
// (the embedded Server's firstBootBootstrap is itself idempotent).
func runInlineInit(cmd *cobra.Command, f *inlineInitFlags) error {
	if err := validateInlineInitFlags(f); err != nil {
		return err
	}

	dataDir := resolveInitDataDir(f)
	if dataDir == "" {
		return fmt.Errorf("init: --data-dir is required")
	}

	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return fmt.Errorf("init: create data dir %s: %w", dataDir, err)
	}

	markerPath := filepath.Join(dataDir, initDoneMarkerName)
	if _, statErr := os.Stat(markerPath); statErr == nil {
		return &InitAlreadyDoneError{DataDir: dataDir}
	}

	officers, err := loadOfficers(f.officers)
	if err != nil {
		return err
	}

	pool, err := loadTrustedCAs(f.trustedCAs)
	if err != nil {
		return err
	}

	switch f.barrierStrategy {
	case "none", "disabled":
		if err := runInitEmbeddedServer(dataDir, f, false); err != nil {
			return err
		}
	case "passphrase":
		if err := runInitEmbeddedServer(dataDir, f, true); err != nil {
			return err
		}
	case "shamir":
		if err := writePendingCeremonyState(dataDir, f, officers); err != nil {
			return err
		}
	default:
		// Unreachable: validateInlineInitFlags + supportedBarrierStrategies
		// already rejected anything outside {none, disabled, passphrase,
		// shamir}. Keep the branch so future strategy additions surface a
		// clean error rather than silently no-op.
		return &UnsupportedBarrierStrategyError{Strategy: f.barrierStrategy}
	}

	marker := initDoneMarker{
		BarrierStrategy:    f.barrierStrategy,
		CredentialStrategy: f.credentialStrategy,
		Hostname:           f.hostname,
		OfficerCount:       len(officers),
		Shares:             f.shares,
		Threshold:          f.threshold,
		CompletedAt:        time.Now().UTC(),
	}
	if err := writeInitDoneMarker(markerPath, &marker); err != nil {
		return err
	}

	printInitSummary(cmd, f, dataDir, len(officers), pool != nil)
	return nil
}

// resolveInitDataDir returns the data directory the init command should use.
// --init-data-dir wins when set so a single binary can drive multiple isolated
// init ceremonies in a test environment; otherwise the global --data-dir
// (already populated into the package-level cfg by initConfig) applies.
func resolveInitDataDir(f *inlineInitFlags) string {
	if f.dataDir != "" {
		return f.dataDir
	}
	if cfg != nil && cfg.Storage.DataDir != "" {
		return cfg.Storage.DataDir
	}
	return dataDir
}

// runInitEmbeddedServer constructs an embedded Server (no listeners) against
// dataDir, runs the full startup sequence so initializeIAMComponents and
// firstBootBootstrap persist the admin + invite, then shuts down. The
// embedded flag suppresses every transport listener; the only public surface
// is via Server.Service() which we do not consume here.
//
// barrierEnabled selects whether the seal manager is wired. For "none" /
// "disabled" the barrier stays off; for "passphrase" we enable it so the
// data-plane seal manager initialises its state alongside the IAM bootstrap.
// "shamir" never reaches this path — its pending state is written separately
// and the daemon comes up on the next "quicbaasd start".
func runInitEmbeddedServer(dataDir string, f *inlineInitFlags, barrierEnabled bool) error {
	initCfg := buildInitConfig(dataDir, f, barrierEnabled)

	// When the passphrase barrier is enabled, inject the passphrase via
	// WithSealPassphrase so that NewStandaloneNode auto-unseals the qrdb
	// node during Start. Without this the node boots sealed (node.Client()
	// returns nil) and GetKVStore panics before the IAM bootstrap can run.
	var srvOpts []server.ServerOption
	if barrierEnabled && isPassphraseStrategy(f.barrierStrategy) && f.secret != "" {
		srvOpts = append(srvOpts, server.WithSealPassphrase([]byte(f.secret)))
	}

	srv, err := server.NewServer(initCfg, srvOpts...)
	if err != nil {
		return fmt.Errorf("init: construct server: %w", err)
	}

	if err := srv.Start(); err != nil {
		_ = srv.Shutdown(context.Background())
		return fmt.Errorf("init: start server: %w", err)
	}

	// Fully initialize the data-plane barrier at SERVER init so the daemon comes
	// up with a usable (initialized) barrier. The operator supplies the
	// passphrase here, once; the runtime client only ever unseals with it and
	// never performs init. Only the software strategy takes a passphrase at this
	// layer — "none" needs no secret, and shamir is initialized through the
	// locked M-of-N ceremony (writePendingCeremonyState), not here.
	if barrierEnabled && isPassphraseStrategy(f.barrierStrategy) {
		if err := initializeSoftwareBarrier(srv, f.secret); err != nil {
			_ = srv.Shutdown(context.Background())
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("init: shutdown server: %w", err)
	}
	return nil
}

// initializeSoftwareBarrier initializes the data-plane software barrier with the
// operator's passphrase so the seal store persists the root key. It is
// idempotent: re-running init against an already-initialized data dir is a
// no-op rather than an error.
func initializeSoftwareBarrier(srv *server.Server, secret string) error {
	svc := srv.GetDataPlaneBarrierService()
	if svc == nil {
		return errors.New("init: data-plane barrier service unavailable; cannot initialize software barrier")
	}
	// The passphrase (software) barrier is sealed with the admin's passphrase,
	// run through the KDF inside qrdb's deferred software strategy. The passphrase
	// is REQUIRED (validated in validateInlineInitFlags) — there is no
	// deployment-derived default. The admin keeps it and supplies it to
	// `barrier unseal` on restart.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := svc.BarrierInitialize(ctx, secret); err != nil {
		if errors.Is(err, qrdbsdk.ErrAlreadyInitialized) {
			return nil
		}
		return fmt.Errorf("init: initialize software barrier: %w", err)
	}
	return nil
}

// buildInitConfig returns a *config.Config tailored for the init ceremony.
// The config is deliberately minimal: standalone mode + embedded transport
// (no listeners), PebbleDB storage rooted at dataDir, seal disabled or
// enabled depending on the strategy, no orchestration / RAG / vector /
// timeseries subsystems. The goal is to bring up just enough of the server
// to drive the IAM bootstrap and persist its state.
func buildInitConfig(dataDir string, f *inlineInitFlags, barrierEnabled bool) *config.Config {
	cfg := config.Default()
	cfg.Server.Embedded = true
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.Backend = config.StorageBackendPebble
	cfg.Storage.DataDir = dataDir

	// Disable every listener at the config level too, so any code that
	// inspects the per-protocol Enabled flag (rather than Server.Embedded)
	// short-circuits.
	cfg.Server.GRPC.Enabled = false
	cfg.Server.REST.Enabled = false
	cfg.Server.QUIC.Enabled = false
	cfg.Server.MCP.Enabled = false
	cfg.Server.Unix.Enabled = false

	// TLS off — init does not need a CA chain. The first regular "start"
	// will bring up the CA against the same data dir.
	cfg.Security.TLSEnabled = false

	// Seal: propagate the barrier strategy so that when quicbaasd start
	// reads this config (or when the embedded server's lifecycle reads
	// cfg.Seal.Strategy) it builds the correct data-plane StorageBarrier.
	// For "none"/"disabled" the barrier is left disabled; for "passphrase"
	// it is enabled so the seal sub-store is initialised under dataDir.
	cfg.Seal.Enabled = barrierEnabled
	cfg.Seal.Strategy = f.barrierStrategy

	// Auth: keep AllowPasswordAuth false so the firstBootBootstrap path
	// (which seeds the system tenant admin + invite) runs.
	cfg.Auth.AllowPasswordAuth = false
	cfg.Auth.InsecureDevMode = false
	if f.hostname != "" {
		cfg.Server.Host = f.hostname
	}

	// Disable optional subsystems so init runs without external deps.
	cfg.KMS.Enabled = false
	cfg.Secrets.Enabled = false
	cfg.Vector.Enabled = false
	cfg.Ollama.Enabled = false
	cfg.Backup.Enabled = false
	cfg.Discovery.Enabled = false
	cfg.Messaging.Enabled = false
	cfg.Orchestration.Enabled = false
	cfg.TimeSeries.Enabled = false
	cfg.Raft.EnableBootstrap = false
	cfg.Server.EmbeddedAgent.Enabled = false

	// Logging: keep it quiet so init produces a clean summary; the server
	// emits its own structured logs anyway.
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "warn"
	}
	if cfg.Logging.Format == "" {
		cfg.Logging.Format = "json"
	}
	if cfg.Logging.Output == "" {
		cfg.Logging.Output = "stderr"
	}

	return cfg
}

// writePendingCeremonyState persists the shamir ceremony parameters under
// dataDir so the operator can resume via "quicbaas ceremony claim-cert begin
// ...". The officer materials submitted via --so are stored at their
// original paths; we only record the metadata required to identify them on
// resume.
//
// The state file uses an atomic-rename write to avoid partially-written JSON
// being read by a concurrent ceremony tool.
func writePendingCeremonyState(dataDir string, f *inlineInitFlags, officers []*officerMaterial) error {
	pendingOfficers := make([]pendingCeremonyOfficer, 0, len(officers))
	for i, raw := range f.officers {
		spec, err := parseOfficerSpec(raw)
		if err != nil {
			return err
		}
		_ = officers[i] // material already loaded for validation; the path is the persisted reference
		pendingOfficers = append(pendingOfficers, pendingCeremonyOfficer{
			Username: spec.Username,
			Kind:     string(spec.Kind),
			Path:     spec.Path,
		})
	}
	state := &pendingCeremonyState{
		Strategy:           f.barrierStrategy,
		Shares:             f.shares,
		Threshold:          f.threshold,
		CredentialStrategy: f.credentialStrategy,
		Officers:           pendingOfficers,
		CreatedAt:          time.Now().UTC(),
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("init: marshal pending ceremony state: %w", err)
	}
	target := filepath.Join(dataDir, pendingCeremonyFileName)
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("init: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, target); err != nil {
		return fmt.Errorf("init: rename pending ceremony state to %s: %w", target, err)
	}
	return nil
}

// writeInitDoneMarker writes the init-done marker file with 0600 permissions
// using an atomic rename so an interrupted init cannot leave a half-written
// marker that fools the next invocation.
func writeInitDoneMarker(path string, m *initDoneMarker) error {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("init: marshal init-done marker: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("init: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("init: rename init-done marker to %s: %w", path, err)
	}
	return nil
}

// printInitSummary writes a short operator summary to stderr (so the
// command's stdout stays clean for tooling that captures it). The summary
// names the barrier strategy, data dir, officer count, and any next steps
// specific to the chosen strategy.
func printInitSummary(cmd *cobra.Command, f *inlineInitFlags, dataDir string, officerCount int, trustedCAs bool) {
	out := cmd.ErrOrStderr()
	fmt.Fprintln(out, "=== quicbaasd init: complete ===")
	fmt.Fprintf(out, "  DataDir:            %s\n", dataDir)
	fmt.Fprintf(out, "  BarrierStrategy:    %s\n", f.barrierStrategy)
	if f.barrierStrategy == "shamir" {
		fmt.Fprintf(out, "  Shamir Mode:        %d-of-%d\n", f.threshold, f.shares)
	}
	fmt.Fprintf(out, "  CredentialStrategy: %s\n", f.credentialStrategy)
	if f.hostname != "" {
		fmt.Fprintf(out, "  Hostname:           %s\n", f.hostname)
	}
	if officerCount > 0 {
		fmt.Fprintf(out, "  Officers:           %d (validated)\n", officerCount)
	}
	if trustedCAs {
		fmt.Fprintf(out, "  TrustedCAPaths:     %d\n", len(f.trustedCAs))
	}
	fmt.Fprintln(out)
	switch f.barrierStrategy {
	case "shamir":
		fmt.Fprintln(out, "Next steps:")
		fmt.Fprintf(out, "  1) Start the daemon: quicbaasd start --data-dir %s\n", dataDir)
		fmt.Fprintln(out, "  2) Complete the ceremony: quicbaas ceremony claim-cert begin ...")
	default:
		fmt.Fprintln(out, "Next step:")
		fmt.Fprintf(out, "  Start the daemon: quicbaasd start --data-dir %s\n", dataDir)
	}
}
