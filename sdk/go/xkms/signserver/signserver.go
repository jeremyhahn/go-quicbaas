// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// Package signserver re-exports go-xkms's sdk/go/signserver: the embeddable,
// node-local, sign-only xkms server a remote-mode PKCS#11 module
// (libxkms_pkcs11.so) signs through, and the key provisioning that creates a
// key with exactly the CKA_LABEL and CKA_ID the module exposes
// (ProvisionECKeyPair, KeyRequest). It is the KMS key-create with PKCS#11
// identity: a CGO-free daemon runs the Server in process and creates the keys
// a separate, cgo-loading consumer (an IKE daemon loading the module through
// p11-kit) later signs with.
//
// Every name here is an alias, never a wrapper, so values cross between this
// package and go-xkms without conversion. The server and the provisioning
// stay in go-xkms; quicbaas routes, it does not implement.
package signserver

import xkmssignserver "github.com/jeremyhahn/go-xkms/sdk/go/signserver"

// Server types.
type (
	// Server is the node-local sign-only server.
	Server = xkmssignserver.Server

	// Config configures a Server.
	Config = xkmssignserver.Config

	// Slot is a sign-only view of one PKCS#11 token.
	Slot = xkmssignserver.Slot

	// KeyRequest describes a key to create in a slot.
	KeyRequest = xkmssignserver.KeyRequest

	// Key is a provisioned key.
	Key = xkmssignserver.Key

	// Error reports the server operation that failed.
	Error = xkmssignserver.Error

	// SlotError adds the slot a failure concerns.
	SlotError = xkmssignserver.SlotError
)

// Key provisioning against any key service and token store, for an embedder
// that provisions a remote-mode module's keys without running a Server (for
// example against xkmsd over its unix socket).
type (
	// ProvisionConfig names the key service and token store.
	ProvisionConfig = xkmssignserver.ProvisionConfig

	// KeyPairRequest describes the EC key pair to create.
	KeyPairRequest = xkmssignserver.KeyPairRequest

	// ProvisionedKeyPair describes the key pair on the token.
	ProvisionedKeyPair = xkmssignserver.ProvisionedKeyPair

	// ProvisionError reports the Cryptoki step that failed.
	ProvisionError = xkmssignserver.ProvisionError

	// PKCS11Transport is the key-service client provisioning drives.
	PKCS11Transport = xkmssignserver.PKCS11Transport

	// PKCS11StateClient is the token-record client a remote token store uses.
	PKCS11StateClient = xkmssignserver.PKCS11StateClient
)

var (
	// New builds a Server.
	New = xkmssignserver.New

	// ProvisionECKeyPair creates an EC key pair with the requested CKA_LABEL
	// and CKA_ID on a token, idempotently.
	ProvisionECKeyPair = xkmssignserver.ProvisionECKeyPair

	// NewRemoteStorage returns the token store a remote-mode module reads,
	// over a PKCS11StateClient.
	NewRemoteStorage = xkmssignserver.NewRemoteStorage
)

// DefaultSocketMode is the socket file mode when Config.SocketMode is zero.
const DefaultSocketMode = xkmssignserver.DefaultSocketMode

// Sentinel errors, matched with errors.Is.
var (
	ErrSocketPathRequired   = xkmssignserver.ErrSocketPathRequired
	ErrNoAllowedPeers       = xkmssignserver.ErrNoAllowedPeers
	ErrBackendsRequired     = xkmssignserver.ErrBackendsRequired
	ErrStateStorageRequired = xkmssignserver.ErrStateStorageRequired
	ErrSlotsRequired        = xkmssignserver.ErrSlotsRequired
	ErrSlotTokenRequired    = xkmssignserver.ErrSlotTokenRequired
	ErrSlotDuplicate        = xkmssignserver.ErrSlotDuplicate
	ErrSlotKeysRequired     = xkmssignserver.ErrSlotKeysRequired
	ErrUnknownSlot          = xkmssignserver.ErrUnknownSlot
	ErrKeyNotNamed          = xkmssignserver.ErrKeyNotNamed
	ErrKeyNotAdmitted       = xkmssignserver.ErrKeyNotAdmitted
	ErrTokenWriteDenied     = xkmssignserver.ErrTokenWriteDenied
	ErrRequestShape         = xkmssignserver.ErrRequestShape

	ErrProvisionClientRequired   = xkmssignserver.ErrProvisionClientRequired
	ErrProvisionStorageRequired  = xkmssignserver.ErrProvisionStorageRequired
	ErrProvisionLabelRequired    = xkmssignserver.ErrProvisionLabelRequired
	ErrProvisionIDRequired       = xkmssignserver.ErrProvisionIDRequired
	ErrProvisionCurveUnsupported = xkmssignserver.ErrProvisionCurveUnsupported
	ErrProvisionUserPINRequired  = xkmssignserver.ErrProvisionUserPINRequired
	ErrProvisionSOPINRequired    = xkmssignserver.ErrProvisionSOPINRequired
	ErrProvisionECPoint          = xkmssignserver.ErrProvisionECPoint
	ErrProvisionKeyConflict      = xkmssignserver.ErrProvisionKeyConflict
)
