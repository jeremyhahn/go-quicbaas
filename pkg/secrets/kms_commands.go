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

package secrets

import (
	"encoding/json"
	"time"
)

// KeyState represents the lifecycle state of a cryptographic key.
type KeyState string

const (
	// KeyStateActive indicates the key is active and can be used for encryption/decryption.
	KeyStateActive KeyState = "active"

	// KeyStateRotated indicates the key has been rotated and is only used for decryption.
	KeyStateRotated KeyState = "rotated"

	// KeyStateDestroyed indicates the key has been destroyed.
	KeyStateDestroyed KeyState = "destroyed"
)

// kmsCommandType identifies the type of KMS Raft command.
type kmsCommandType string

const (
	kmsCommandSetDEK    kmsCommandType = "set_dek"
	kmsCommandRotateDEK kmsCommandType = "rotate_dek"
)

// KMSDEKEntry represents a DEK entry for Raft-based cluster synchronization.
type KMSDEKEntry struct {
	// ID is the DEK identifier.
	ID string `json:"id"`

	// Version is the DEK version number.
	Version uint64 `json:"version"`

	// Algorithm is the encryption algorithm (e.g., "AES-256-GCM").
	Algorithm string `json:"algorithm"`

	// SealedData is the wrapped (encrypted) key material.
	SealedData []byte `json:"sealed_data"`

	// State is the current key lifecycle state.
	State KeyState `json:"state"`

	// KeyBytes is the key size in bytes.
	KeyBytes int `json:"key_bytes"`

	// CreatedAt is the creation timestamp.
	CreatedAt time.Time `json:"created_at"`
}

// kmsCommand is the envelope for KMS Raft commands.
type kmsCommand struct {
	Type    kmsCommandType  `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// setDEKPayload is the payload for a SetDEK command.
type setDEKPayload struct {
	Entry *KMSDEKEntry `json:"entry"`
}

// rotateDEKPayload is the payload for a RotateDEK command.
type rotateDEKPayload struct {
	ID         string `json:"id"`
	SealedData []byte `json:"sealed_data"`
	Algorithm  string `json:"algorithm"`
	KeyBytes   int    `json:"key_bytes"`
}

// KMSCommandBuilder constructs serialized Raft commands for KMS operations.
type KMSCommandBuilder struct{}

// NewKMSCommandBuilder creates a new KMS command builder.
func NewKMSCommandBuilder() *KMSCommandBuilder {
	return &KMSCommandBuilder{}
}

// SetDEK builds a serialized command to set a DEK entry in the cluster.
func (b *KMSCommandBuilder) SetDEK(entry *KMSDEKEntry) ([]byte, error) {
	payload, err := json.Marshal(&setDEKPayload{Entry: entry})
	if err != nil {
		return nil, &SecretsCauseError{Message: "marshal SetDEK payload", Cause: err}
	}
	cmd := &kmsCommand{
		Type:    kmsCommandSetDEK,
		Payload: payload,
	}
	return json.Marshal(cmd)
}

// RotateDEK builds a serialized command to rotate a DEK in the cluster.
func (b *KMSCommandBuilder) RotateDEK(id string, sealedData []byte, algorithm string, keyBytes int) ([]byte, error) {
	payload, err := json.Marshal(&rotateDEKPayload{
		ID:         id,
		SealedData: sealedData,
		Algorithm:  algorithm,
		KeyBytes:   keyBytes,
	})
	if err != nil {
		return nil, &SecretsCauseError{Message: "marshal RotateDEK payload", Cause: err}
	}
	cmd := &kmsCommand{
		Type:    kmsCommandRotateDEK,
		Payload: payload,
	}
	return json.Marshal(cmd)
}
