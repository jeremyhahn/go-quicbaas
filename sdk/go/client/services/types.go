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

package services

import (
	"encoding/json"
	"time"
)

// ServiceName is the service identifier every transport routes service
// plane requests through.
const ServiceName = "quicbaas.Services"

// SignedManifest is a service manifest and the signature over it. Payload is
// the manifest's JSON encoding exactly as signed; the server verifies the
// signature against KeyID in its keyring before it reads the payload.
type SignedManifest struct {
	// Payload is the signed manifest JSON.
	Payload []byte `json:"payload"`

	// KeyID names the signing key in the server's keyring.
	KeyID string `json:"key_id"`

	// Signature is the signature over Payload.
	Signature []byte `json:"signature"`
}

// DeployRequest carries the signed manifest of a deploy or redeploy.
type DeployRequest struct {
	// Manifest is the signed manifest.
	Manifest *SignedManifest `json:"manifest"`
}

// NameRequest addresses one of the caller tenant's services.
type NameRequest struct {
	// Name is the service name.
	Name string `json:"name"`
}

// ListRequest lists the caller tenant's services.
type ListRequest struct{}

// Record is a service as the plane holds it.
type Record struct {
	// Tenant owns the service.
	Tenant string `json:"tenant"`

	// Name is the service name, unique within its tenant.
	Name string `json:"name"`

	// State is the lifecycle state: deploying, running, stopped, halted or
	// undeploying.
	State string `json:"state"`

	// Verb is the verb that moved the service last.
	Verb string `json:"verb,omitempty"`

	// Revision counts the manifests the service has run; a redeploy adds one.
	Revision int64 `json:"revision"`

	// Restarts counts restarts.
	Restarts int64 `json:"restarts"`

	// Grants maps each grant kind (identity, certificate, name, rbac,
	// secrets) to the reference the grant returned.
	Grants map[string]string `json:"grants,omitempty"`

	// Reason says why the last verb failed, when it did.
	Reason string `json:"reason,omitempty"`

	// UpdatedAt is the time of the last change.
	UpdatedAt time.Time `json:"updated_at"`

	// KeyID names the key that signed the running manifest.
	KeyID string `json:"key_id"`

	// Manifest is the verified manifest, as JSON.
	Manifest json.RawMessage `json:"manifest,omitempty"`
}

// ListResponse lists services.
type ListResponse struct {
	// Services are the caller tenant's services, ordered by name.
	Services []Record `json:"services"`
}
