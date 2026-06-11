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

package iam

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// Broker profile typed errors. Declared here (not errors.go) so the seam's
// storage contract is self-contained while still following the project's typed
// *dberrors.QBError convention.
var (
	// ErrBrokerProfileNotFound is returned when a broker side-record does not
	// exist for the given (tenantID, qrdb uint64 user ID) join key.
	ErrBrokerProfileNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.broker_profile",
		Err:  &IAMMessageError{Message: "broker profile not found"},
	}

	// ErrBrokerProfileInvalid is returned when a broker side-record is missing a
	// required field (non-zero qrdb user ID and non-empty tenant ID).
	ErrBrokerProfileInvalid = &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   "iam.broker_profile",
		Err:  &IAMMessageError{Message: "broker profile requires a non-zero qrdb user id and a tenant id"},
	}
)

// BrokerProfile is the quicbaas-owned side-record that holds the broker-only
// identity fields qrdb's sdk.User does NOT model. The locked identity
// architecture keeps qrdb as the canonical store for the user core
// (Username, TenantID, RoleIDs/RoleBindings, CertBindings, Factors, ABAC
// Attributes, Enabled, System, timestamps); everything the broker layer adds on
// top — the OIDC/PEP-facing display identity, the lifecycle state machine, MFA
// bookkeeping, invite flow, and IAM-only group membership — lives here.
//
// A BrokerProfile is keyed by the qrdb uint64 user ID (the durable cross-store
// correlation key), NOT the quicbaas string UUID and NOT crammed into qrdb's
// Attributes map. The string UUID ↔ uint64 correlation is owned by the IDMap
// (see pkg/iam/qrdbadapter); the BrokerProfile only ever references the uint64.
type BrokerProfile struct {
	// QRDBUserID is the qrdb Snowflake user ID this profile decorates. It is the
	// primary key and the join key back to the qrdb sdk.User core.
	QRDBUserID uint64 `json:"qrdb_user_id"`

	// TenantID is the owning tenant; it scopes the store and matches the qrdb
	// user's TenantID.
	TenantID string `json:"tenant_id"`

	// Email is the broker display/login email (unique within tenant). qrdb has no
	// Email field, so it is broker-owned.
	Email string `json:"email,omitempty"`

	// Name is the broker display name. qrdb has no Name field.
	Name string `json:"name,omitempty"`

	// Groups lists the IAM group IDs this user belongs to. Groups are a
	// BaaS-only concept; qrdb has no Group type, so membership stays broker-side.
	Groups []string `json:"groups,omitempty"`

	// AuthMethods lists the allowed authentication methods (e.g. "webauthn",
	// "piv"). Broker-side login-policy metadata.
	AuthMethods []string `json:"auth_methods,omitempty"`

	// State is the broker lifecycle state (pending, active, disabled).
	State UserState `json:"state,omitempty"`

	// Suspended indicates the account is suspended by an administrator.
	Suspended bool `json:"suspended,omitempty"`

	// Locked indicates the account is locked (e.g. by failed-attempt lockout).
	Locked bool `json:"locked,omitempty"`

	// MFAEnabled indicates whether MFA is enabled for this user.
	MFAEnabled bool `json:"mfa_enabled,omitempty"`

	// MFAMethod is the enrolled MFA method (totp, webauthn).
	MFAMethod MFAMethod `json:"mfa_method,omitempty"`

	// MFAEnrolledAt is when MFA was enrolled.
	MFAEnrolledAt *time.Time `json:"mfa_enrolled_at,omitempty"`

	// MFAVerified records last-session MFA verification (legacy field carried for
	// round-trip fidelity with iam.User).
	MFAVerified bool `json:"mfa_verified,omitempty"`

	// MFATimestamp is the last MFA verification time.
	MFATimestamp *time.Time `json:"mfa_timestamp,omitempty"`

	// LastAuth is the last successful authentication timestamp.
	LastAuth *time.Time `json:"last_auth,omitempty"`

	// CertBindings holds the qrdb-canonical mTLS certificate bindings carried
	// for round-trip fidelity with iam.User. PIV activation appends a binding of
	// kind qrdbsdk.CertBindingPIVAttested. Empty unless the user has bound at
	// least one certificate.
	CertBindings []qrdbsdk.CertBinding `json:"cert_bindings,omitempty"`

	// InviteToken holds the plaintext invite token shown once at creation time;
	// cleared after activation.
	InviteToken string `json:"invite_token,omitempty"`

	// InviteExpiresAt is the invite expiry. Nil when no invite is pending.
	InviteExpiresAt *time.Time `json:"invite_expires_at,omitempty"`

	// Created is the profile creation timestamp.
	Created time.Time `json:"created,omitempty"`

	// Updated is the last modification timestamp.
	Updated time.Time `json:"updated,omitempty"`
}

// BrokerProfileStore is the tenant-scoped persistence surface for broker
// side-records. Every verb carries the owning tenantID so it routes through that
// tenant's barrier store (encrypted-at-rest under the tenant's DEK; a sealed
// barrier tenant fails closed and never falls back to the shared store).
type BrokerProfileStore interface {
	// SaveProfile creates or overwrites the profile for profile.QRDBUserID within
	// profile.TenantID.
	SaveProfile(ctx context.Context, profile *BrokerProfile) error

	// GetProfile reads the profile for qrdbUserID within tenantID. Returns
	// ErrBrokerProfileNotFound when absent.
	GetProfile(ctx context.Context, tenantID string, qrdbUserID uint64) (*BrokerProfile, error)

	// DeleteProfile removes the profile for qrdbUserID within tenantID. Deleting a
	// missing profile is a no-op.
	DeleteProfile(ctx context.Context, tenantID string, qrdbUserID uint64) error
}

// broker profile storage key helpers. Profiles live under the canonical __iam:
// namespace so they are encrypted-at-rest in the owning tenant's barrier when
// the tenant is barrier-isolated. The key embeds both the tenant and the qrdb
// uint64 id so a per-tenant scan stays cheap and the join key is unambiguous.
const keyPrefixBrokerProfile = KeyPrefixIAM + "broker_profile:"

func brokerProfileKey(tenantID string, qrdbUserID uint64) string {
	return keyPrefixBrokerProfile + tenantID + ":" + strconv.FormatUint(qrdbUserID, 10)
}

// BackendBrokerProfileStore implements BrokerProfileStore over the per-tenant
// routing KVStore, mirroring the Backend* storage pattern used across pkg/iam.
type BackendBrokerProfileStore struct {
	kv interfaces.KVStore
}

// NewBackendBrokerProfileStore constructs a persistent, per-tenant broker
// profile store over the routing KVStore.
func NewBackendBrokerProfileStore(kv interfaces.KVStore) *BackendBrokerProfileStore {
	return &BackendBrokerProfileStore{kv: kv}
}

var _ BrokerProfileStore = (*BackendBrokerProfileStore)(nil)

// SaveProfile routes by the profile's TenantID and writes the record. It
// requires a non-zero QRDBUserID (the primary key) and a non-empty TenantID.
func (s *BackendBrokerProfileStore) SaveProfile(ctx context.Context, profile *BrokerProfile) error {
	if profile == nil || profile.QRDBUserID == 0 || profile.TenantID == "" {
		return ErrBrokerProfileInvalid
	}
	ctx = withTenant(ctx, profile.TenantID)
	now := time.Now()
	if profile.Created.IsZero() {
		profile.Created = now
	}
	profile.Updated = now
	data, err := json.Marshal(profile)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.broker_profile.save", Err: err}
	}
	return s.kv.Put(ctx, brokerProfileKey(profile.TenantID, profile.QRDBUserID), data)
}

// GetProfile routes by tenantID and reads the profile, translating a not-found
// into ErrBrokerProfileNotFound.
func (s *BackendBrokerProfileStore) GetProfile(ctx context.Context, tenantID string, qrdbUserID uint64) (*BrokerProfile, error) {
	if tenantID == "" || qrdbUserID == 0 {
		return nil, ErrBrokerProfileInvalid
	}
	ctx = withTenant(ctx, tenantID)
	data, err := s.kv.Get(ctx, brokerProfileKey(tenantID, qrdbUserID))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, ErrBrokerProfileNotFound
		}
		return nil, err
	}
	var p BrokerProfile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.broker_profile.get", Err: err}
	}
	return &p, nil
}

// DeleteProfile routes by tenantID and removes the record.
func (s *BackendBrokerProfileStore) DeleteProfile(ctx context.Context, tenantID string, qrdbUserID uint64) error {
	if tenantID == "" || qrdbUserID == 0 {
		return ErrBrokerProfileInvalid
	}
	ctx = withTenant(ctx, tenantID)
	return s.kv.Delete(ctx, brokerProfileKey(tenantID, qrdbUserID))
}
