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

// This file completes the root package's view of the DEK-management and
// secrets clients. With these constructors and aliases a consumer reaches
// every DEK and secrets operation, and names every request and response
// type, through sdk/go alone, without importing sdk/go/client/dek,
// sdk/go/client/secrets or any generated api/proto type.

package quicbaas

import (
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/dek"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/secrets"
)

// NewDEKClient returns a DEK-management client over t; it returns nil for a nil transport.
func NewDEKClient(t Transport) DEKClient { return dek.NewClient(t) }

// NewSecretsClient returns a secrets client over t; it returns nil for a nil transport.
func NewSecretsClient(t Transport) SecretsClient { return secrets.NewClient(t) }

// SetDefaultDEKResponse is the result of DEKClient.SetDefaultDEK.
type SetDefaultDEKResponse = dek.SetDefaultDEKResponse

// SetDEKSafetyRequest configures the AEAD safety limits of a DEK.
type SetDEKSafetyRequest = dek.SetDEKSafetyRequest

// StartReencryptionRequest starts background re-encryption to a new DEK version.
type StartReencryptionRequest = dek.StartReencryptionRequest

// StartReencryptionResponse is the result of DEKClient.StartReencryption.
type StartReencryptionResponse = dek.StartReencryptionResponse

// GetReencryptionProgressRequest asks for the progress of a re-encryption.
type GetReencryptionProgressRequest = dek.GetReencryptionProgressRequest

// ReencryptionProgressResponse is the result of DEKClient.GetReencryptionProgress.
type ReencryptionProgressResponse = dek.ReencryptionProgressResponse

// ReencryptionProgress is the progress of one re-encryption operation.
type ReencryptionProgress = dek.ReencryptionProgress

// EnsureSecretRequest asks SecretsClient.Ensure for a secret, generating it on first access.
type EnsureSecretRequest = secrets.EnsureSecretRequest

// EnsureSecretResponse is the result of SecretsClient.Ensure.
type EnsureSecretResponse = secrets.EnsureSecretResponse

// SecretStatus is the status of a secret, set with SecretsClient.UpdateStatus.
type SecretStatus = secrets.SecretStatus

const (
	// SecretStatusEnabled indicates the secret is active.
	SecretStatusEnabled = secrets.SecretStatusEnabled
	// SecretStatusDisabled indicates the secret is disabled.
	SecretStatusDisabled = secrets.SecretStatusDisabled
	// SecretStatusDeleted indicates the secret is deleted.
	SecretStatusDeleted = secrets.SecretStatusDeleted
)
