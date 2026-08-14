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
	"strconv"

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
}

// validBarrierStrategies is the canonical --barrier-strategy set per
// docs/08-bootstrap-auth-architecture.md §2. "none" is the canonical
// no-barrier value; "disabled" is preserved as a synonym for backward
// compatibility with operators upgrading from earlier builds. The
// hardware-backed strategies (pkcs11/tpm2/awskms/gcpkms/azurekv/vault)
// are recognised by the flag parser so operators see a precise typed
// UnsupportedBarrierStrategyError instead of a generic "invalid value";
// their factory wiring lands in a follow-on phase.
var validBarrierStrategies = map[string]struct{}{
	"none":       {},
	"disabled":   {},
	"passphrase": {},
	"shamir":     {},
	"pkcs11":     {},
	"tpm2":       {},
	"awskms":     {},
	"gcpkms":     {},
	"azurekv":    {},
	"vault":      {},
}

// supportedBarrierStrategies enumerates the strategies that have wiring
// landed today. Strategies in validBarrierStrategies but absent from this
// set are recognised by the CLI surface but rejected at init time with
// UnsupportedBarrierStrategyError so operators get a precise message
// rather than a confusing init failure deep inside the seal manager.
var supportedBarrierStrategies = map[string]struct{}{
	"none":       {},
	"disabled":   {},
	"passphrase": {},
	"shamir":     {},
}

// isPassphraseStrategy reports whether a --barrier-strategy value selects the
// passphrase-protected software seal strategy.
func isPassphraseStrategy(strategy string) bool {
	return strategy == "passphrase"
}

// barrierStrategyAllowsPINs reports whether a strategy honours --so-pin /
// --user-pin. The no-barrier strategies ("none" / "disabled") MUST NOT
// accept PINs — silently ignoring them would let an operator believe their
// SO credential is in effect when the daemon has no barrier at all.
func barrierStrategyAllowsPINs(strategy string) bool {
	switch strategy {
	case "none", "disabled":
		return false
	default:
		return true
	}
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

// PINsNotAllowedError is returned when --so-pin or --user-pin is supplied
// alongside a barrier strategy that has no SO/User credential concept
// (currently "none" and "disabled"). The error is explicit rather than
// silent: ignoring the PIN would let operators believe their SO credential
// is in effect when the daemon has no barrier at all.
type PINsNotAllowedError struct {
	Strategy string
}

// Error returns a human-readable description.
func (e *PINsNotAllowedError) Error() string {
	return "init: --so-pin / --user-pin are not valid with --barrier-strategy=" + e.Strategy +
		" (no barrier, no SO/User concept)"
}

// UnsupportedBarrierStrategyError is returned for barrier strategies that
// are recognised on the CLI surface but whose factory wiring has not
// landed yet. Today the hardware-backed strategies (pkcs11, tpm2, awskms,
// gcpkms, azurekv, vault) fall in this category; "none", "passphrase",
// and "shamir" are supported.
type UnsupportedBarrierStrategyError struct {
	Strategy string
}

// Error returns a human-readable description.
func (e *UnsupportedBarrierStrategyError) Error() string {
	return "init: barrier strategy " + e.Strategy + " is recognised but not yet wired; " +
		"supported strategies: none, passphrase, shamir"
}

// SPIFFETrustDomainRequiredError is returned by "quicbaasd init" when
// iam.spiffe_trust_domain is not configured.
//
// It is a hard requirement, not a warning, because the trust domain is what
// makes the admin credential init issues actually usable: the xkms CA stamps
// the tenant-scoped SPIFFE URI SAN
// (spiffe://<trust-domain>/tenant/system/user/admin) onto the admin
// certificate ONLY when a trust domain is set, and REST login resolves — and
// pins — the tenant from exactly that SAN. Without it the certificate carries
// no SPIFFE SAN, login falls back to home-realm discovery, a freshly
// initialized node has no HRD routing entries, and the operator gets
// "401 login tenant could not be resolved" from the credential init just
// handed them.
//
// The rejected alternatives, recorded so they are not re-litigated:
//   - Synthesizing a trust domain (e.g. from --hostname) would bake a PINNED
//     security identifier into every issued certificate; configuring a real
//     trust domain later would silently invalidate all of them.
//   - Seeding a system-tenant home-realm entry would make "system" resolvable
//     for ANY unrouted login attempt, permanently widening the authentication
//     surface to buy first-boot convenience.
//
// Requiring the value invents no security identifier and widens nothing; the
// operator must declare a trust domain eventually regardless.
type SPIFFETrustDomainRequiredError struct{}

// Error returns a human-readable description naming the exact config key and
// an example value.
func (e *SPIFFETrustDomainRequiredError) Error() string {
	return "init: iam.spiffe_trust_domain is required and is not set.\n" +
		"  It is what lets the issued admin certificate authenticate: the CA stamps\n" +
		"  spiffe://<trust-domain>/tenant/system/user/admin onto it and login resolves\n" +
		"  the tenant from that SAN. Without it, login fails closed with\n" +
		"  \"login tenant could not be resolved\".\n" +
		"  Set it in your config file, for example:\n" +
		"      iam:\n" +
		"        spiffe_trust_domain: quicbaas.example.test"
}

// InitAlreadyDoneError is returned by "quicbaasd init" when the data
// directory already contains the markers of a previous successful init
// (a sealed barrier file and a persisted bootstrap admin assignment).
// Operators should run the regular "quicbaasd start" command instead.
type InitAlreadyDoneError struct {
	DataDir string
}

// Error returns a human-readable description.
func (e *InitAlreadyDoneError) Error() string {
	return "init: data directory " + e.DataDir + " is already initialized; " +
		"run 'quicbaasd start --data-dir " + e.DataDir + "' to bring the daemon up"
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
  QUICBAAS_SANS                equivalent to --sans`,
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
	return nil
}

// resolveAndValidateCanonicalInit resolves env-var fallbacks via viper
// and validates strategy-specific flag combinations. envPrefix selects
// the per-binary env var namespace.
func resolveAndValidateCanonicalInit(cmd *cobra.Command, f *canonicalInitFlags, envPrefix string) error {
	v := viper.New()
	v.SetEnvPrefix(envPrefix)
	v.AutomaticEnv()

	// stringSinks / intSinks point each canonical flag at the corresponding
	// field of f so a resolved env value is applied to the struct that the
	// validation below actually reads. The cobra flag (bound to the inline
	// struct) is updated in lock-step so the caller's copy-back stays correct.
	stringSinks := map[string]*string{
		"so-pin":           &f.soPin,
		"user-pin":         &f.userPin,
		"barrier-strategy": &f.barrierStrategy,
		"hostname":         &f.hostname,
		"sans":             &f.sans,
	}
	intSinks := map[string]*int{
		"shares":    &f.shares,
		"threshold": &f.threshold,
	}
	bindings := map[string]string{
		"so-pin":           "SO_PIN",
		"user-pin":         "USER_PIN",
		"barrier-strategy": "BARRIER_STRATEGY",
		"shares":           "SHARES",
		"threshold":        "THRESHOLD",
		"hostname":         "HOSTNAME",
		"sans":             "SANS",
	}
	for flagName, envKey := range bindings {
		_ = v.BindEnv(flagName, envPrefix+"_"+envKey)
		if cmd.Flags().Changed(flagName) {
			continue
		}
		val := v.GetString(flagName)
		if val == "" {
			continue
		}
		// Propagate the env value into the cobra-bound (inline) flag variable
		// when the flag is registered, so the caller's copy-back observes it.
		if cmd.Flags().Lookup(flagName) != nil {
			if err := cmd.Flags().Set(flagName, val); err != nil {
				return &OpError{Op: fmt.Sprintf("init: invalid env value for %s", flagName), Cause: err}
			}
		}
		// Apply the resolved value to the canonicalInitFlags fields used by the
		// validation below.
		if sink, ok := stringSinks[flagName]; ok {
			*sink = val
			continue
		}
		if sink, ok := intSinks[flagName]; ok {
			n, err := strconv.Atoi(val)
			if err != nil {
				return &OpError{Op: fmt.Sprintf("init: invalid env value for %s", flagName), Cause: err}
			}
			*sink = n
		}
	}

	if _, ok := validBarrierStrategies[f.barrierStrategy]; !ok {
		return &MessageError{Message: fmt.Sprintf("init: invalid --barrier-strategy %q", f.barrierStrategy)}
	}
	if _, ok := supportedBarrierStrategies[f.barrierStrategy]; !ok {
		return &UnsupportedBarrierStrategyError{Strategy: f.barrierStrategy}
	}

	// PIN handling depends on the strategy. The no-barrier strategies have
	// no SO/User concept; supplying PINs would silently no-op which is the
	// kind of failure mode that costs operators hours of head-scratching.
	// Reject PINs explicitly so the misuse surfaces at the CLI boundary.
	if barrierStrategyAllowsPINs(f.barrierStrategy) {
		if f.soPin == "" {
			return &MessageError{Message: fmt.Sprintf("init: --so-pin is required (or set %s_SO_PIN)", envPrefix)}
		}
		if f.userPin == "" {
			return &MessageError{Message: fmt.Sprintf("init: --user-pin is required (or set %s_USER_PIN)", envPrefix)}
		}
	} else {
		if f.soPin != "" || f.userPin != "" {
			return &PINsNotAllowedError{Strategy: f.barrierStrategy}
		}
	}

	if f.barrierStrategy == "shamir" {
		if f.shares < 2 {
			return &MessageError{Message: "init: --shares >= 2 required when --barrier-strategy=shamir"}
		}
		if f.threshold < 2 || f.threshold > f.shares {
			return &MessageError{Message: "init: --threshold (2 <= threshold <= shares) required when --barrier-strategy=shamir"}
		}
	} else if f.shares != 0 || f.threshold != 0 {
		return &MessageError{Message: "init: --shares / --threshold only valid when --barrier-strategy=shamir"}
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
				return &MessageError{Message: fmt.Sprintf("quicbaasd barrier %s: invoke via 'quicbaas barrier %s' or 'qrdb barrier %s'", s.use, s.use, s.use)}
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
				return &MessageError{Message: fmt.Sprintf("quicbaasd barrier recovery %s: invoke via 'quicbaas barrier recovery %s'", v, v)}
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
				return &MessageError{Message: fmt.Sprintf("quicbaasd ceremony %s: invoke via 'quicbaas ceremony %s' or 'qrdb ceremony %s'", s.use, s.use, s.use)}
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
				return &MessageError{Message: fmt.Sprintf("quicbaasd ca %s: invoke via 'quicbaas ca %s'", v, v)}
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
				return &MessageError{Message: fmt.Sprintf("quicbaasd bootstrap %s: bootstrap is owned by xkms; invoke via 'xkmsctl bootstrap %s'", m, m)}
			},
		})
	}
	return cmd
}

// newCanonicalCredentialCmd builds the canonical "credential" command.
// The daemon exposes the canonical command shape; the credential operations
// run through the quicbaas SDK CredentialService in the CLIENT binary
// ("quicbaas credential ..."), which shipped with the Phase 8e credential
// surface. Like the barrier / ceremony / ca daemon-side trees, these verbs
// redirect the operator to the client rather than pretending to be pending.
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
				return &MessageError{Message: fmt.Sprintf("quicbaasd credential %s: invoke via 'quicbaas credential %s' or 'qrdb credential %s'", v, v, v)}
			},
		})
	}
	return cmd
}

// newCanonicalAuthCmd builds the canonical "auth" command.
// The daemon exposes the canonical command shape; login and N-factor
// management run through the quicbaas SDK Security().Auth() surface in the
// CLIENT binary ("quicbaas auth ..."). Like the barrier / ceremony / ca
// daemon-side trees, these verbs redirect the operator to the client.
func newCanonicalAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "User authentication and N-factor management",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "login",
		Short: "Authenticate the active session",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return &MessageError{Message: "quicbaasd auth login: invoke via 'quicbaas auth login'"}
		},
	})
	factor := &cobra.Command{Use: "factor", Short: "N-factor management"}
	for _, verb := range []string{"list", "add", "remove", "rotate", "relabel"} {
		v := verb
		factor.AddCommand(&cobra.Command{
			Use:   v,
			Short: "Factor " + v,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return &MessageError{Message: fmt.Sprintf("quicbaasd auth factor %s: invoke via 'quicbaas auth factor %s'", v, v)}
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
