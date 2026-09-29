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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/initmarker"
	"github.com/jeremyhahn/go-quicbaas/pkg/server"
)

// InvalidAdminFactorError reports an --admin-factor value outside the
// canonical go-qrdb vocabulary.
type InvalidAdminFactorError struct {
	Cause error
}

// Error returns a human-readable description.
func (e *InvalidAdminFactorError) Error() string {
	return "init: invalid --admin-factor (must be one of mtls, fido2, piv): " + e.Cause.Error()
}

// Unwrap returns the wrapped error.
func (e *InvalidAdminFactorError) Unwrap() error { return e.Cause }

// barrierStrategyShamir is the operator-facing name of the M-of-N Shamir
// barrier strategy. Its barrier is never initialized inline: qrdb's bootstrap
// orchestrator defers it to the ceremony, so init treats it as the one
// strategy that starts no server.
const barrierStrategyShamir = "shamir"

// ErrSecretRequiredForSoftware is returned when --barrier-strategy=passphrase is
// requested without a passphrase. The passphrase barrier is sealed with the
// admin's passphrase at init; there is no deployment-derived default.
var ErrSecretRequiredForSoftware = errors.New("init: --passphrase is required when --barrier-strategy=passphrase")

// ErrInvalidCredentialStrategy is returned when --credential-strategy is
// not one of the canonical values.
var ErrInvalidCredentialStrategy = errors.New("init: --credential-strategy must be one of manual, barrier")

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
		return &SentinelDetailError{Sentinel: ErrInvalidCredentialStrategy, Detail: fmt.Sprintf("%q", value)}
	}
	return nil
}

// ---------------------------------------------------------------------------
// inlineInitFlags is the full Phase 11 init flag set (a strict superset of
// canonicalInitFlags). The two structs are kept distinct so the locked
// canonical surface in canonical.go does not accidentally regress when
// new optional flags land here.
// ---------------------------------------------------------------------------.
type inlineInitFlags struct {
	soPin              string
	userPin            string
	barrierStrategy    string
	secret             string
	shares             int
	threshold          int
	hostname           string
	sans               string
	dataDir            string
	credentialStrategy string
	adminFactors       []string
	passkeyDevice      string
}

// wireInlineInitFlags attaches the full init flag set onto cmd. The canonical
// subset (so-pin/user-pin/barrier-strategy/shares/threshold/hostname/sans) is
// wired here so the locked surface stays in lock-step; init-data-dir and
// credential-strategy add the deployment-local options on top.
//
// There are deliberately no officer flags. Under qrdb's Security Officer
// distribution model officers are registered against the RUNNING daemon
// through the sealed window (officer-register), never as init-time files —
// see runInlineInit.
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

	// Phase 11 inline-ceremony additions.
	cmd.Flags().StringVar(&f.dataDir, "init-data-dir", "",
		"Per-init data directory (overrides root --data-dir for the init ceremony)")
	cmd.Flags().StringVar(&f.credentialStrategy, "credential-strategy", "barrier",
		"Credential management strategy: manual | barrier")

	// Admin LOGIN factors. Distinct from the barrier's seal strategy: a seal
	// strategy protects the barrier key at rest, a login factor proves who a
	// human is. The flag name and vocabulary match xkmsd so all daemons in the
	// stack accept identical arguments.
	cmd.Flags().StringSliceVar(&f.adminFactors, "admin-factor", nil,
		"Administrator login factor: mtls | fido2 | piv (repeatable; mtls is always enrolled)")
	cmd.Flags().StringVar(&f.passkeyDevice, "passkey-device", "",
		"Select one FIDO2 authenticator when several are attached")
}

// validateInlineInitFlags applies all Phase 11 inline-ceremony validation
// rules. The canonical subset is enforced upstream via
// resolveAndValidateCanonicalInit (invoked by resolveAndValidateInlineInit
// in canonical.go); this function only enforces the inline-specific rules.
func validateInlineInitFlags(f *inlineInitFlags) error {
	if err := validateCredentialStrategy(f.credentialStrategy); err != nil {
		return err
	}
	// A SPIFFE trust domain is mandatory: without it the admin certificate init
	// issues carries no tenant SAN and cannot authenticate. Checked here, in the
	// validation seam, so the failure lands BEFORE any side effect — no data
	// dir, no CA, no marker. See SPIFFETrustDomainRequiredError for why this is
	// required rather than synthesized or worked around with HRD seeding.
	if cfg == nil || cfg.IAM.SPIFFETrustDomain == "" {
		return &SPIFFETrustDomainRequiredError{}
	}
	// The passphrase (software) barrier is sealed with the admin's passphrase at
	// init; it is REQUIRED (no deployment-derived default). "none" needs none;
	// "shamir" is initialized through the M-of-N ceremony.
	if isPassphraseStrategy(f.barrierStrategy) && f.secret == "" {
		return ErrSecretRequiredForSoftware
	}
	// Reject an unknown --admin-factor here, in the validation seam, so the
	// failure lands BEFORE any state exists. go-qrdb owns the vocabulary, so
	// parsing through its canonical parser keeps all three daemons in step
	// rather than each maintaining its own list.
	if len(f.adminFactors) > 0 {
		if _, err := qrdbsdk.ParseAdminFactors(f.adminFactors); err != nil {
			return &InvalidAdminFactorError{Cause: err}
		}
	}

	// Shamir needs no further flag-level checks here: --shares / --threshold
	// are validated by the canonical surface, and the officer set is supplied
	// to the ceremony against the running daemon rather than at init time.
	return nil
}

// initDoneMarkerName is the filename of the marker dropped into --data-dir
// after a successful init. Its presence is the canonical "this data dir has
// already been initialised" signal; absence permits the init flow to run.
// The single definition lives in pkg/initmarker, which the daemon-side
// reader (pkg/server) imports too, so the writer and parser can never drift.
const initDoneMarkerName = initmarker.FileName

// initDoneMarker is the on-disk record dropped after a successful init
// (alias of the shared pkg/initmarker.Marker).
type initDoneMarker = initmarker.Marker

// runInlineInit is the canonical "quicbaasd init" entry point. The flow:
//
//  1. Validate flags (including reject PINs under none; reject hardware
//     strategies and any not in supportedBarrierStrategies).
//  2. Resolve the per-init data dir (--init-data-dir overrides the global
//     --data-dir for the init ceremony so test runs can target a tmpdir).
//  3. If the data dir already holds an init-done marker, fail with
//     InitAlreadyDoneError rather than silently re-running.
//  4. For "none" and "passphrase": construct an embedded Server and call
//     Start, which brings up the CA (TLS is always on so first-boot can issue
//     the admin mTLS credential), runs the IAM first-boot via qrdb's canonical
//     bootstrap orchestrator (system tenant, built-in roles, platform admin +
//     admin-mTLS certificate) and enrolls the SO / user PINs, then Shutdown.
//  5. For "shamir": do NOT start a server. qrdb's bootstrap orchestrator
//     documents the locked deferred-init architecture — the system barrier
//     comes up SEALED and is initialized and unsealed by the M-of-N ceremony
//     BEFORE BootstrapSystem runs, because "Shamir barrier initialization is
//     the ceremony's job (multiple SOs), never inline" (go-qrdb
//     pkg/bootstrap/bootstrap.go). init therefore only records the operator's
//     chosen strategy so the daemon boots sealed into its sealed window, where
//     officers register, the ceremony runs, and each share is claimed live.
//     Starting an embedded server here would boot sealed and fail with
//     EmbeddedSealedBootError.
//  6. Drop the init-done marker and emit a brief operator summary.
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
		return &MessageError{Message: "init: --data-dir is required"}
	}

	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return &OpError{Op: fmt.Sprintf("init: create data dir %s", dataDir), Cause: err}
	}

	markerPath := filepath.Join(dataDir, initDoneMarkerName)
	if _, statErr := os.Stat(markerPath); statErr == nil {
		return &InitAlreadyDoneError{DataDir: dataDir}
	}

	if _, ok := supportedBarrierStrategies[f.barrierStrategy]; !ok {
		// Defence in depth: validateInlineInitFlags already rejected anything
		// outside the supported set. Keep the guard so a future strategy
		// addition surfaces a clean error rather than silently no-op'ing.
		return &UnsupportedBarrierStrategyError{Strategy: f.barrierStrategy}
	}

	// "none" and "passphrase" run the embedded server, because that is the
	// path that reaches the first-boot bootstrap orchestrator: system tenant,
	// built-in roles, platform admin and the admin's mTLS certificate. Only
	// "passphrase" enables the seal layer during init; "none" has no barrier.
	//
	// "shamir" runs nothing here. Its barrier is initialized by the M-of-N
	// ceremony against the running daemon, and first-boot follows the unseal
	// rather than preceding it — see this function's doc comment.
	if f.barrierStrategy != barrierStrategyShamir {
		if err := runInitEmbeddedServer(dataDir, f, isPassphraseStrategy(f.barrierStrategy)); err != nil {
			return err
		}
	}

	marker := initDoneMarker{
		BarrierStrategy:    f.barrierStrategy,
		CredentialStrategy: f.credentialStrategy,
		Hostname:           f.hostname,
		Shares:             f.shares,
		Threshold:          f.threshold,
		CompletedAt:        time.Now().UTC(),
	}
	if err := initmarker.Write(dataDir, &marker); err != nil {
		return &OpError{Op: "init: write init-done marker", Cause: err}
	}

	printInitSummary(cmd, f, dataDir)
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
// dataDir, runs the full startup sequence so initializeCA, initializeIAMComponents
// and firstBootBootstrap establish the CA, the system tenant, the built-in
// roles and the platform admin's mTLS credential, then shuts down. The embedded
// flag suppresses every transport listener; the only public surface is via
// Server.Service() which we do not consume here.
//
// barrierEnabled selects whether the seal manager is wired. For "none" /
// "disabled" the barrier stays off; for "passphrase" we enable it so the
// data-plane seal manager initialises its state alongside the IAM bootstrap.
// Shamir never reaches this function — its barrier belongs to the ceremony.
func runInitEmbeddedServer(
	dataDir string,
	f *inlineInitFlags,
	barrierEnabled bool,
) error {
	initCfg := buildInitConfig(dataDir, f, barrierEnabled)

	// When the passphrase barrier is enabled, inject the passphrase via
	// WithSealPassphrase so that NewStandaloneNode auto-unseals the qrdb
	// node during Start. Without this the node boots sealed: in embedded
	// mode (which init uses) the server refuses the boot with a typed
	// EmbeddedSealedBootError, because the sealed-window listeners that a
	// daemon boot would serve (see pkg/server/sealed_window.go) have no
	// place in a blocking library Start. The init flow has the passphrase
	// in hand, so auto-unseal is always the right call here.
	var srvOpts []server.ServerOption
	if barrierEnabled && isPassphraseStrategy(f.barrierStrategy) && f.secret != "" {
		srvOpts = append(srvOpts, server.WithSealPassphrase([]byte(f.secret)))
	}

	srv, err := server.NewServer(initCfg, srvOpts...)
	if err != nil {
		return &OpError{Op: "init: construct server", Cause: err}
	}

	if err := srv.Start(); err != nil {
		_ = srv.Shutdown(context.Background())
		return &OpError{Op: "init: start server", Cause: err}
	}

	// Barrier initialization is NOT hand-rolled here. For "passphrase" the
	// embedded qrdb node initializes and unseals its barrier from the
	// injected passphrase during Start, and qrdb's canonical bootstrap
	// orchestrator (qrdbsdk.BootstrapSystem, invoked by the server's
	// first-boot) observes / completes it idempotently.

	// Enroll the operator's Security Officer and user PINs into the SAME PIN
	// backend the embedded node verifies against. --so-pin / --user-pin are
	// mandatory for every barrier-bearing strategy; without this call they were
	// collected and discarded, leaving the backend uninitialised so every later
	// SO-authorized operation failed.
	if err := srv.EnrollInitPINs(f.soPin, f.userPin); err != nil {
		_ = srv.Shutdown(context.Background())
		return &OpError{Op: "init: enroll ceremony PINs", Cause: err}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return &OpError{Op: "init: shutdown server", Cause: err}
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
	// operatorCfg is the daemon's loaded configuration (package-level `cfg`),
	// aliased before the local shadow below so the settings that must survive
	// into the init run can be read from it.
	operatorCfg := cfg

	cfg := config.Default()
	cfg.Server.Embedded = true
	cfg.Server.Mode = config.ServerModeStandalone
	cfg.Storage.DataDir = dataDir

	// Honour the operator's storage backend. This function starts from
	// config.Default(), so hard-coding a backend here would silently override
	// storage.backend / QUICBAAS_STORAGE_BACKEND and leave init writing to a
	// different engine than the daemon later reads from — first boot would
	// appear to succeed and `quicbaasd start` would come up with no IAM state.
	// Pebble remains the default for a config that names no backend.
	cfg.Storage.Backend = config.StorageBackendPebble
	if operatorCfg != nil && operatorCfg.Storage.Backend != "" {
		cfg.Storage.Backend = operatorCfg.Storage.Backend
	}

	// Disable every listener at the config level too, so any code that
	// inspects the per-protocol Enabled flag (rather than Server.Embedded)
	// short-circuits.
	cfg.Server.GRPC.Enabled = false
	cfg.Server.REST.Enabled = false
	cfg.Server.MCP.Enabled = false
	cfg.Server.Unix.Enabled = false

	// TLS ON — not to serve anything (every listener above is disabled and the
	// server runs embedded), but because initializeCA early-returns when TLS is
	// off, and the CA is what first-boot needs to issue the platform admin's
	// mTLS certificate and what the ceremony needs to sign officer CSRs. The CA
	// is file-backed under the data dir, so bootstrapping it here also means the
	// subsequent `quicbaasd start` loads the CA this ceremony established rather
	// than minting a second, unrelated one.
	cfg.Security.TLSEnabled = true

	// Seal: propagate the barrier strategy so that when quicbaasd start
	// reads this config (or when the embedded server's lifecycle reads
	// cfg.Seal.Strategy) it builds the correct data-plane StorageBarrier.
	// For "none"/"disabled" the barrier is left disabled; for "passphrase"
	// it is enabled so the seal sub-store is initialised under dataDir.
	//
	// Shamir never reaches this function (runInlineInit skips the embedded
	// server for it), so barrierEnabled is only ever the passphrase decision
	// here. The shamir M-of-N parameters are recorded in the init-done marker
	// instead, and the daemon boots sealed on them.
	cfg.Seal.Enabled = barrierEnabled
	cfg.Seal.Strategy = f.barrierStrategy

	// Auth: keep AllowPasswordAuth false so the firstBootBootstrap path
	// (which seeds the system tenant admin + its mTLS certificate) runs.
	cfg.Auth.AllowPasswordAuth = false
	cfg.Auth.InsecureDevMode = false

	// Propagate the operator's login-factor selection so first-boot enrolls
	// the administrator with the credentials they asked for. Without this the
	// admin would silently fall back to the mTLS baseline and an operator who
	// requested a passkey would discover it only at their first login attempt.
	cfg.Auth.AdminFactors = f.adminFactors
	cfg.Auth.PasskeyDevice = f.passkeyDevice

	// Propagate the operator's SPIFFE trust domain into the init run. This is
	// load-bearing, not cosmetic: the xkms CAAdapter stamps the tenant-scoped
	// SPIFFE URI SAN onto the admin certificate ONLY when a trust domain is set,
	// and REST's deriveLoginTenant resolves (and pins) the login tenant from
	// exactly that SAN. Because this function starts from config.Default()
	// rather than the loaded config, omitting this line would issue an admin
	// certificate with CN=admin and NO SANs, which then fails login with
	// "login tenant could not be resolved".
	//
	// validateInlineInitFlags guarantees a non-empty value on the CLI path; the
	// guard keeps this safe for direct callers (tests) that bypass validation.
	if operatorCfg != nil && operatorCfg.IAM.SPIFFETrustDomain != "" {
		cfg.IAM.SPIFFETrustDomain = operatorCfg.IAM.SPIFFETrustDomain
	}
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
	cfg.Server.EmbeddedFleetNode.Enabled = false

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

// printInitSummary writes a short operator summary to stderr (so the
// command's stdout stays clean for tooling that captures it). The summary
// names the barrier strategy, data dir, officer count, and any next steps
// specific to the chosen strategy.
func printInitSummary(cmd *cobra.Command, f *inlineInitFlags, dataDir string) {
	out := cmd.ErrOrStderr()
	fmt.Fprintln(out, "=== quicbaasd init: complete ===")
	fmt.Fprintf(out, "  DataDir:            %s\n", dataDir)
	fmt.Fprintf(out, "  BarrierStrategy:    %s\n", f.barrierStrategy)
	if f.barrierStrategy == barrierStrategyShamir {
		fmt.Fprintf(out, "  Shamir Mode:        %d-of-%d\n", f.threshold, f.shares)
	}
	fmt.Fprintf(out, "  CredentialStrategy: %s\n", f.credentialStrategy)
	if f.hostname != "" {
		fmt.Fprintf(out, "  Hostname:           %s\n", f.hostname)
	}
	fmt.Fprintln(out)
	switch f.barrierStrategy {
	case barrierStrategyShamir:
		fmt.Fprintln(out, "The Shamir barrier is NOT initialized yet, by design: it is the M-of-N")
		fmt.Fprintln(out, "ceremony's job, and shares are never written to disk. Next steps:")
		fmt.Fprintf(out, "  1) Start the daemon: quicbaasd start --data-dir %s (boots SEALED)\n", dataDir)
		fmt.Fprintln(out, "  2) Register each officer's CSR:  quicbaas init officer register \\")
		fmt.Fprintln(out, "         --username <name> --csr <file>   (returns their signed certificate)")
		fmt.Fprintf(out, "  3) Run the ceremony:  quicbaas init initialize --threshold %d --shares %d\n", f.threshold, f.shares)
		fmt.Fprintln(out, "  4) Each officer claims live, from their own host, with their own key:")
		fmt.Fprintln(out, "         quicbaas init claim-cert  then  quicbaas init claim-share")
		fmt.Fprintln(out, "  5) A quorum submits shares to unseal:  quicbaas barrier unseal --share <value>")
	default:
		fmt.Fprintln(out, "Next step:")
		fmt.Fprintf(out, "  Start the daemon: quicbaasd start --data-dir %s\n", dataDir)
	}
}
