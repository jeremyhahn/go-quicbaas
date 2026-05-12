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

// Package seal carries the public Config + SealMode types referenced
// by pkg/server/lifecycle.go's seal-manager initialization seam. The
// real BarrierManager implementation lives in pkg/barrier; this
// package exists only so the seam variables in lifecycle.go compile.
//
// When the seal-manager refactor finishes (lifecycle.go is migrated to
// drive pkg/barrier directly via SDK constructors), this package and
// pkg/api/grpc/seal_manager_compat.go should both be deleted.
package seal

// SealMode is the seal-strategy enum surfaced through the YAML config
// and propagated into pkg/barrier strategy selection. Mirrors the
// schema field at config.Seal.Mode.
type SealMode string

// Known SealMode values. The strings match the YAML schema documented
// in go-qrdb's compliance docs.
const (
	// SealModeNone disables the encrypted barrier entirely. The KV
	// store reads/writes raw storage with no per-tenant encryption,
	// no Shamir quorum, no SO ceremony required at first boot. This
	// is the appropriate default for deployments where the underlying
	// filesystem is already encrypted (LUKS) and the operator has not
	// opted into a separate per-tenant cryptographic isolation layer.
	// Switch to one of the encrypted modes (shamir, software, tpm2,
	// pkcs11) when per-tenant key isolation or split-knowledge
	// protection of the master key is required.
	SealModeNone     SealMode = "none"
	SealModeShamir   SealMode = "shamir"
	SealModeSoftware SealMode = "software"
	SealModeTPM2     SealMode = "tpm2"
	SealModePKCS11   SealMode = "pkcs11"
	// SealModeAuto is a legacy alias retained for one release while the
	// remaining lifecycle test fixtures are migrated to the strategy-
	// specific values. New callers SHOULD select an explicit strategy.
	SealModeAuto SealMode = "auto"
)

// Config captures the user-facing seal configuration. The fields
// here mirror the YAML config under the top-level "seal:" block and
// are forwarded to pkg/barrier at server start.
type Config struct {
	// Mode is the active seal strategy ID.
	Mode SealMode

	// Threshold is the number of shares required to unseal under
	// shamir mode. Ignored under other modes.
	Threshold int

	// TotalShares is the total number of shares produced by Initialize
	// under shamir mode. Ignored under other modes.
	TotalShares int

	// RecoveryKeysEnabled, when true, generates a separate set of
	// recovery keys at Initialize time so an emergency unseal path
	// exists if the primary shares are lost.
	RecoveryKeysEnabled bool

	// RecoveryThreshold is the minimum number of recovery keys
	// required to drive a recovery unseal.
	RecoveryThreshold int

	// RecoveryTotalKeys is the total number of recovery keys
	// generated at Initialize time when RecoveryKeysEnabled is true.
	RecoveryTotalKeys int
}
