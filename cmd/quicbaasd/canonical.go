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

// canonical.go wires the locked canonical CLI surface for quicbaasd
// per docs/08-bootstrap-auth-architecture.md §6. The daemon exposes
// the same root-level "init" command as qrdb, xkmsd and platformctl
// with the same flag set; the legacy "seal" command tree is
// renamed to "barrier" and the standalone "seal init" verb is
// collapsed into the canonical root init.
package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// canonicalInitFlags holds the canonical init flag set per
// docs/08-bootstrap-auth-architecture.md §6. The flag set is identical
// across qrdb, xkmsd, quicbaasd and platformctl.
type canonicalInitFlags struct {
	soPin           string
	userPin         string
	barrierStrategy string
	shares          int
	threshold       int
	hostname        string
	sans            string
	adminBundle     string
}

// validBarrierStrategies is the canonical --barrier-strategy set per
// docs/08-bootstrap-auth-architecture.md §2 plus the Phase 11 "disabled"
// strategy used by ephemeral test installs.
var validBarrierStrategies = map[string]struct{}{
	"disabled": {},
	"software": {},
	"shamir":   {},
	"pkcs11":   {},
	"tpm2":     {},
	"awskms":   {},
	"gcpkms":   {},
	"azurekv":  {},
	"vault":    {},
}

// CanonicalRedirectError is emitted when an operator runs a legacy
// renamed command path. It names the deprecated path and the
// canonical replacement.
type CanonicalRedirectError struct {
	Old string
	New string
}

// Error returns a human-readable description.
func (e *CanonicalRedirectError) Error() string {
	return "subcommand '" + e.Old +
		"' has been replaced by '" + e.New +
		"' (per docs/08-bootstrap-auth-architecture.md §6). Run: " + e.New
}

// PendingFactorBackendError is emitted by "auth" subcommand handlers
// before M10 / M14 land.
type PendingFactorBackendError struct {
	Op string
}

// Error returns a human-readable description.
func (e *PendingFactorBackendError) Error() string {
	return e.Op + " is not yet wired to the SDK; pending M10 and M14"
}

// PendingCredentialBackendError is emitted by "credential" subcommand
// handlers before the credential service surface ships in the SDK.
type PendingCredentialBackendError struct {
	Op string
}

// Error returns a human-readable description.
func (e *PendingCredentialBackendError) Error() string {
	return e.Op + " is not yet wired to the SDK CredentialService"
}

// newCanonicalInitCmd builds the canonical root "init" command. The
// command exposes the full Phase 11 inline-ceremony surface defined in
// init.go; the canonical subset (so-pin/user-pin/barrier-strategy/...)
// is the locked invariant validated by canonical_surface_test.go and
// continues to be enforced by resolveAndValidateCanonicalInit.
func newCanonicalInitCmd() *cobra.Command {
	f := &inlineInitFlags{}

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize the barrier, CA, admin cert and roles in one call",
		Long: `Bootstrap a fresh quicbaas server in one call.

Canonical init command per docs/08-bootstrap-auth-architecture.md §6,
extended with the Phase 11 inline-ceremony surface (officer enrollment
via CSR / external certificate / raw public key, multi-pool trusted
CA roots, credential-strategy selection). Delegates to the qrdb
CeremonyService via the quicbaas SDK.

Canonical environment variables (Viper-bound):
  QUICBAAS_SO_PIN              equivalent to --so-pin
  QUICBAAS_USER_PIN            equivalent to --user-pin
  QUICBAAS_BARRIER_STRATEGY    equivalent to --barrier-strategy
  QUICBAAS_SHARES              equivalent to --shares
  QUICBAAS_THRESHOLD           equivalent to --threshold
  QUICBAAS_HOSTNAME            equivalent to --hostname
  QUICBAAS_SANS                equivalent to --sans
  QUICBAAS_ADMIN_BUNDLE        equivalent to --admin-bundle`,
		Args: cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			return resolveAndValidateInlineInit(cmd, f, "QUICBAAS")
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInlineInit(cmd, f)
		},
	}
	wireInlineInitFlags(cmd, f)
	return cmd
}


// resolveAndValidateInlineInit resolves env-var fallbacks via viper and
// validates the full Phase 11 inline init flag set. The canonical subset
// is enforced via resolveAndValidateCanonicalInit so the locked surface
// invariants stay symmetric with qrdb / xkmsd / platformctl.
func resolveAndValidateInlineInit(cmd *cobra.Command, f *inlineInitFlags, envPrefix string) error {
	canonical := &canonicalInitFlags{
		soPin:           f.soPin,
		userPin:         f.userPin,
		barrierStrategy: f.barrierStrategy,
		shares:          f.shares,
		threshold:       f.threshold,
		hostname:        f.hostname,
		sans:            f.sans,
		adminBundle:     f.adminBundle,
	}
	if err := resolveAndValidateCanonicalInit(cmd, canonical, envPrefix); err != nil {
		return err
	}
	// Copy resolved canonical values back. resolveAndValidateCanonicalInit
	// can mutate fields via cmd.Flags().Set so the env-var-resolved values
	// must be propagated into the inline struct before runInlineInit reads
	// them.
	f.soPin = canonical.soPin
	f.userPin = canonical.userPin
	f.barrierStrategy = canonical.barrierStrategy
	f.shares = canonical.shares
	f.threshold = canonical.threshold
	f.hostname = canonical.hostname
	f.sans = canonical.sans
	f.adminBundle = canonical.adminBundle
	return nil
}

// resolveAndValidateCanonicalInit resolves env-var fallbacks via viper
// and validates strategy-specific flag combinations. envPrefix selects
// the per-binary env var namespace.
func resolveAndValidateCanonicalInit(cmd *cobra.Command, f *canonicalInitFlags, envPrefix string) error {
	v := viper.New()
	v.SetEnvPrefix(envPrefix)
	v.AutomaticEnv()

	bindings := map[string]string{
		"so-pin":           "SO_PIN",
		"user-pin":         "USER_PIN",
		"barrier-strategy": "BARRIER_STRATEGY",
		"shares":           "SHARES",
		"threshold":        "THRESHOLD",
		"hostname":         "HOSTNAME",
		"sans":             "SANS",
		"admin-bundle":     "ADMIN_BUNDLE",
	}
	for flagName, envKey := range bindings {
		_ = v.BindEnv(flagName, envPrefix+"_"+envKey)
		if !cmd.Flags().Changed(flagName) {
			if val := v.GetString(flagName); val != "" {
				if err := cmd.Flags().Set(flagName, val); err != nil {
					return fmt.Errorf("init: invalid env value for %s: %w", flagName, err)
				}
			}
		}
	}

	if f.soPin == "" {
		return fmt.Errorf("init: --so-pin is required (or set %s_SO_PIN)", envPrefix)
	}
	if f.userPin == "" {
		return fmt.Errorf("init: --user-pin is required (or set %s_USER_PIN)", envPrefix)
	}
	if _, ok := validBarrierStrategies[f.barrierStrategy]; !ok {
		return fmt.Errorf("init: invalid --barrier-strategy %q", f.barrierStrategy)
	}
	if f.barrierStrategy == "shamir" {
		if f.shares < 2 {
			return fmt.Errorf("init: --shares >= 2 required when --barrier-strategy=shamir")
		}
		if f.threshold < 2 || f.threshold > f.shares {
			return fmt.Errorf("init: --threshold (2 <= threshold <= shares) required when --barrier-strategy=shamir")
		}
	} else if f.shares != 0 || f.threshold != 0 {
		return fmt.Errorf("init: --shares / --threshold only valid when --barrier-strategy=shamir")
	}
	return nil
}

// newCanonicalBarrierCmd builds the canonical "barrier" command tree
// per docs/08-bootstrap-auth-architecture.md §6. quicbaasd does not
// own the barrier implementation; the commands are exposed for shape
// parity and emit a clear redirect to the platformctl / qrdb client
// where the barrier ops actually run.
func newCanonicalBarrierCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "barrier",
		Short: "Post-init barrier operations (seal, unseal, status, rekey, recovery)",
		Long: `Operational barrier subcommands.

The quicbaasd daemon exposes the canonical command shape; the actual
barrier ops are owned by the qrdb seal package and surfaced through
the quicbaas SDK. Use 'quicbaas barrier ...' (CLI client) for runtime
operations; this daemon-side surface is for command-shape parity.`,
	}
	for _, sub := range []struct {
		use, short string
	}{
		{"seal", "Seal the barrier"},
		{"unseal", "Unseal the barrier"},
		{"status", "Show barrier status"},
		{"rekey", "Rekey the barrier"},
	} {
		s := sub
		cmd.AddCommand(&cobra.Command{
			Use:   s.use,
			Short: s.short,
			Args:  cobra.ArbitraryArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return fmt.Errorf("quicbaasd barrier %s: invoke via 'quicbaas barrier %s' or 'qrdb barrier %s'", s.use, s.use, s.use)
			},
		})
	}
	recovery := &cobra.Command{Use: "recovery", Short: "Recovery key management"}
	for _, verb := range []string{"generate", "restore"} {
		v := verb
		recovery.AddCommand(&cobra.Command{
			Use:   v,
			Short: "Recovery " + v,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return fmt.Errorf("quicbaasd barrier recovery %s: invoke via 'quicbaas barrier recovery %s'", v, v)
			},
		})
	}
	cmd.AddCommand(recovery)
	return cmd
}

// newCanonicalSealRedirectCmd preserves the legacy "quicbaasd seal"
// command path so operators see a clear migration error rather than
// "unknown command" per docs/08-bootstrap-auth-architecture.md §6.
func newCanonicalSealRedirectCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "seal",
		Short:              "RENAMED: use the 'barrier' command group instead",
		DisableFlagParsing: true,
		Args:               cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 && args[0] == "init" {
				return &CanonicalRedirectError{
					Old: "quicbaasd seal init",
					New: "quicbaasd init --barrier-strategy <name> --so-pin ... --user-pin ... [...]",
				}
			}
			return &CanonicalRedirectError{Old: "quicbaasd seal", New: "quicbaasd barrier"}
		},
	}
}

// newCanonicalCeremonyCmd builds the canonical "ceremony" command
// tree. The daemon exposes the shape; quicbaas SDK delegates to qrdb.
func newCanonicalCeremonyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ceremony",
		Short: "Ceremony state-machine ops (status, claim-cert, claim-share, sign-csr)",
	}
	for _, sub := range []struct{ use, short string }{
		{"status", "Show the current ceremony state"},
		{"claim-cert", "Claim a server certificate"},
		{"claim-share", "Claim a threshold share"},
		{"sign-csr", "Sign a CSR with SO authorization"},
	} {
		s := sub
		c := &cobra.Command{
			Use:   s.use,
			Short: s.short,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return fmt.Errorf("quicbaasd ceremony %s: invoke via 'quicbaas ceremony %s' or 'qrdb ceremony %s'", s.use, s.use, s.use)
			},
		}
		if s.use == "claim-cert" {
			c.AddCommand(&cobra.Command{Use: "begin", Short: "Begin certificate claim"})
			c.AddCommand(&cobra.Command{Use: "complete", Short: "Complete certificate claim"})
		}
		cmd.AddCommand(c)
	}
	return cmd
}

// newCanonicalCACmd builds the canonical "ca" command tree.
func newCanonicalCACmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ca",
		Short: "CA operations (info, bundle, list, issue, revoke, export-bundle)",
	}
	for _, verb := range []string{"info", "bundle", "list", "issue", "revoke", "export-bundle"} {
		v := verb
		cmd.AddCommand(&cobra.Command{
			Use:   v,
			Short: "CA " + v,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return fmt.Errorf("quicbaasd ca %s: invoke via 'quicbaas ca %s'", v, v)
			},
		})
	}
	return cmd
}

// newCanonicalBootstrapCmd builds the canonical "bootstrap" command
// tree (auto, dane, noise, spki, direct).
func newCanonicalBootstrapCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Trust bootstrap (auto, dane, noise, spki, direct)",
	}
	for _, mode := range []string{"auto", "dane", "noise", "spki", "direct"} {
		m := mode
		cmd.AddCommand(&cobra.Command{
			Use:   m,
			Short: "Trust bootstrap via " + m,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return fmt.Errorf("quicbaasd bootstrap %s: bootstrap is owned by xkms; invoke via 'xkmsctl bootstrap %s'", m, m)
			},
		})
	}
	return cmd
}

// newCanonicalCredentialCmd builds the canonical "credential" command.
func newCanonicalCredentialCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "credential",
		Short: "Post-init credential operations (submit, list, rotate)",
	}
	for _, verb := range []string{"submit", "list", "rotate"} {
		v := verb
		cmd.AddCommand(&cobra.Command{
			Use:   v,
			Short: "Credential " + v,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return &PendingCredentialBackendError{Op: "credential " + v}
			},
		})
	}
	return cmd
}

// newCanonicalAuthCmd builds the canonical "auth" command.
func newCanonicalAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "User authentication and N-factor management",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "login",
		Short: "Authenticate the active session",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return &PendingFactorBackendError{Op: "auth login"}
		},
	})
	factor := &cobra.Command{Use: "factor", Short: "N-factor management"}
	for _, verb := range []string{"list", "add", "remove", "rotate", "relabel"} {
		v := verb
		factor.AddCommand(&cobra.Command{
			Use:   v,
			Short: "Factor " + v,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return &PendingFactorBackendError{Op: "auth factor " + v}
			},
		})
	}
	cmd.AddCommand(factor)
	return cmd
}

// configureCanonicalSurface registers the canonical commands onto the
// quicbaasd rootCmd.
func configureCanonicalSurface() {
	rootCmd.AddCommand(newCanonicalInitCmd())
	rootCmd.AddCommand(newCanonicalAuthCmd())
	rootCmd.AddCommand(newCanonicalBarrierCmd())
	rootCmd.AddCommand(newCanonicalCACmd())
	rootCmd.AddCommand(newCanonicalCeremonyCmd())
	rootCmd.AddCommand(newCanonicalCredentialCmd())
	rootCmd.AddCommand(newCanonicalBootstrapCmd())
	rootCmd.AddCommand(newCanonicalSealRedirectCmd())
}
