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

package grpc

import (
	"bytes"
	"encoding/json"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	iam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// Tenant engine-provisioning conversions.
//
// handleCreateTenant works off the generic map produced by toMap, so the
// engine quota and entitlement sub-objects arrive as map[string]interface{}
// with JSON-decoded numbers. Rather than reading each field out of the map by
// hand — seventeen quota limits that would silently drift the moment the
// engine adds one — these helpers decode through the engine's own structs,
// whose JSON tags are the same names the SDK request marshals to. A field
// added upstream therefore travels correctly without a change here, and a
// malformed sub-object surfaces as a typed InvalidInput instead of being
// silently zeroed.

// decodeEngineObject re-decodes a generic sub-object into T. It returns
// (nil, nil) when the caller left the field unset, which is what preserves
// "use the engine's configured defaults" — distinct from sending an
// all-zero struct.
func decodeEngineObject[T any](v interface{}, context string) (*T, error) {
	if v == nil {
		return nil, nil
	}
	// A typed value already came from an in-process caller; take it directly.
	if typed, ok := v.(*T); ok {
		return typed, nil
	}
	if typed, ok := v.(T); ok {
		return &typed, nil
	}

	data, err := json.Marshal(v)
	if err != nil {
		return nil, errors.NewInvalidInput(context, "value is not encodable")
	}
	// Strict about unknown fields: absence of the object means "keep the
	// engine's configured defaults", so a mistyped key must not decode to an
	// empty override that silently replaces them.
	out := new(T)
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return nil, errors.NewInvalidInput(context, "value does not match the expected shape")
	}
	return out, nil
}

// engineQuotasToProto converts the engine_quotas field of a create-tenant
// request into its proto form. Only the configured limits cross the wire; the
// Current* usage counters on the engine struct are engine-owned outputs and a
// caller cannot set them at creation.
func engineQuotasToProto(v interface{}) (*iampb.TenantEngineQuotas, error) {
	q, err := decodeEngineObject[iam.EngineQuotas](v, "handleCreateTenant.engine_quotas")
	if err != nil || q == nil {
		return nil, err
	}
	// No counter can reach this point: iam.EngineQuotas names the limits only,
	// so the strict decode above already rejected current_* as an unknown
	// field. The proto below is therefore a total mapping, not a lossy one.
	return &iampb.TenantEngineQuotas{
		MaxDatabases:          q.MaxDatabases,
		MaxRaftGroups:         q.MaxRaftGroups,
		MaxReplicasPerGroup:   q.MaxReplicasPerGroup,
		MaxTotalReplicas:      q.MaxTotalReplicas,
		MaxCollections:        q.MaxCollections,
		MaxKeys:               q.MaxKeys,
		MaxStorageBytes:       q.MaxStorageBytes,
		MaxDatabaseSizeBytes:  q.MaxDatabaseSizeBytes,
		MaxUsers:              q.MaxUsers,
		MaxRoles:              q.MaxRoles,
		MaxCertificates:       q.MaxCertificates,
		MaxBackups:            q.MaxBackups,
		MaxVectorDims:         q.MaxVectorDims,
		MaxTsMetrics:          q.MaxTSMetrics,
		MaxDedicatedWalGroups: q.MaxDedicatedWALGroups,
		SystemBarrierQuota:    q.SystemBarrierQuota,
	}, nil
}

// engineEntitlementsToProto converts the engine_entitlements field of a
// create-tenant request into its proto form.
func engineEntitlementsToProto(v interface{}) (*iampb.TenantEngineEntitlements, error) {
	e, err := decodeEngineObject[iam.EngineEntitlements](v, "handleCreateTenant.engine_entitlements")
	if err != nil || e == nil {
		return nil, err
	}
	return &iampb.TenantEngineEntitlements{
		AllowedIsolationModes:     e.AllowedIsolationModes,
		AllowDedicatedRaft:        e.AllowDedicatedRaft,
		AllowVolumeStorage:        e.AllowVolumeStorage,
		AllowSystemManagedBarrier: e.AllowSystemManagedBarrier,
		AllowDedicatedWal:         e.AllowDedicatedWAL,
		AllowedBarrierModes:       e.AllowedBarrierModes,
	}, nil
}
