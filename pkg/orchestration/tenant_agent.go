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

package orchestration

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// TenantAgentRecordID is the fixed key segment a tenant's TenantAgent
// appointment is stored under. Because every tenant has exactly one key, the
// key itself enforces one Tenant Agent per tenant.
const TenantAgentRecordID = "current"

// TenantAgent is a tenant's appointment of its Tenant Agent: the tenant-level
// agent that coordinates the tenant. It binds the catalog Agent or pool that
// runs it to the go-qrdb certificate principal it acts as and the go-xkms key
// it signs with.
//
// The record is METADATA ONLY: it names a principal and a key and never holds
// a credential or key material.
type TenantAgent struct {
	// TenantID is the tenant the appointment belongs to. The store sets it
	// from the key, so it can never name another tenant.
	TenantID string `json:"tenant_id"`

	// Agent is the catalog Agent or pool that runs the Tenant Agent.
	Agent AgentRef `json:"agent"`

	// Principal is the go-qrdb username (certificate CN) it acts as.
	Principal string `json:"principal"`

	// SigningKeyID is the go-xkms key ID it signs with.
	SigningKeyID string `json:"signing_key_id"`

	// CreatedAt is when the appointment was first stored.
	CreatedAt time.Time `json:"created_at,omitempty"`

	// UpdatedAt is when the appointment was last written.
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// Validate reports the first missing or malformed field as an
// *InputValidationError.
func (a TenantAgent) Validate() error {
	if strings.TrimSpace(a.TenantID) == "" {
		return &InputValidationError{Field: "tenant_agent.tenant_id", Message: "required"}
	}
	if err := RequireAgentRef(a.Agent, "tenant_agent.agent"); err != nil {
		return err
	}
	if strings.TrimSpace(a.Principal) == "" {
		return &InputValidationError{Field: "tenant_agent.principal", Message: "required"}
	}
	if strings.TrimSpace(a.SigningKeyID) == "" {
		return &InputValidationError{Field: "tenant_agent.signing_key_id", Message: "required"}
	}
	return nil
}

var (
	// ErrTenantAgentUnknown reports that a tenant has no Tenant Agent.
	ErrTenantAgentUnknown = errors.New("tenant agent: not appointed")

	// ErrTenantAgentExists reports an appointment refused because the tenant
	// already has a Tenant Agent.
	ErrTenantAgentExists = errors.New("tenant agent: already appointed")
)

// UnknownTenantAgentError reports a tenant with no Tenant Agent appointed.
type UnknownTenantAgentError struct {
	TenantID string
}

// Error implements the error interface.
func (e *UnknownTenantAgentError) Error() string {
	return ErrTenantAgentUnknown.Error() + ": tenant " + strconv.Quote(e.TenantID)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *UnknownTenantAgentError) Unwrap() error { return ErrTenantAgentUnknown }

// TenantAgentExistsError reports an appointment refused because the tenant
// already has a Tenant Agent, naming the principal that holds it.
type TenantAgentExistsError struct {
	TenantID  string
	Principal string
}

// Error implements the error interface.
func (e *TenantAgentExistsError) Error() string {
	return ErrTenantAgentExists.Error() + ": tenant " + strconv.Quote(e.TenantID) +
		" is held by " + strconv.Quote(e.Principal)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *TenantAgentExistsError) Unwrap() error { return ErrTenantAgentExists }
