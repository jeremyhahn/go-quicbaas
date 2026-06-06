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

package grpc

import (
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/cluster"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/embedding"
	iam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/vector"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// --- KV enum conversions ---

func TestConvertConsistencyToProto_AllBranches(t *testing.T) {
	cases := map[service.ConsistencyLevel]pb.ConsistencyLevel{
		service.ConsistencyLevelStrong:       pb.ConsistencyLevel_CONSISTENCY_LEVEL_STRONG,
		service.ConsistencyLevelLinearizable: pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE,
		service.ConsistencyLevelEventual:     pb.ConsistencyLevel_CONSISTENCY_LEVEL_EVENTUAL,
		service.ConsistencyLevel(999):        pb.ConsistencyLevel_CONSISTENCY_LEVEL_UNSPECIFIED,
	}
	for in, want := range cases {
		if got := convertConsistencyToProto(in); got != want {
			t.Errorf("convertConsistencyToProto(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestConvertCompareTargetToProto_AllBranches(t *testing.T) {
	cases := map[service.CompareTarget]pb.CompareTarget{
		service.CompareTargetVersion: pb.CompareTarget_COMPARE_TARGET_VERSION,
		service.CompareTargetValue:   pb.CompareTarget_COMPARE_TARGET_VALUE,
		service.CompareTargetExists:  pb.CompareTarget_COMPARE_TARGET_EXISTS,
		service.CompareTarget(999):   pb.CompareTarget_COMPARE_TARGET_UNSPECIFIED,
	}
	for in, want := range cases {
		if got := convertCompareTargetToProto(in); got != want {
			t.Errorf("convertCompareTargetToProto(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestConvertCompareOperatorToProto_AllBranches(t *testing.T) {
	cases := map[service.CompareOperator]pb.CompareOperator{
		service.CompareOperatorEqual:        pb.CompareOperator_COMPARE_OPERATOR_EQUAL,
		service.CompareOperatorNotEqual:     pb.CompareOperator_COMPARE_OPERATOR_NOT_EQUAL,
		service.CompareOperatorGreater:      pb.CompareOperator_COMPARE_OPERATOR_GREATER,
		service.CompareOperatorLess:         pb.CompareOperator_COMPARE_OPERATOR_LESS,
		service.CompareOperatorGreaterEqual: pb.CompareOperator_COMPARE_OPERATOR_GREATER_EQUAL,
		service.CompareOperatorLessEqual:    pb.CompareOperator_COMPARE_OPERATOR_LESS_EQUAL,
		service.CompareOperator(999):        pb.CompareOperator_COMPARE_OPERATOR_UNSPECIFIED,
	}
	for in, want := range cases {
		if got := convertCompareOperatorToProto(in); got != want {
			t.Errorf("convertCompareOperatorToProto(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestConvertTransactionOpType_RoundTrip(t *testing.T) {
	pairs := []struct {
		sdk service.TransactionOpType
		pb  pb.TransactionOpType
	}{
		{service.TransactionOpTypePut, pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT},
		{service.TransactionOpTypeGet, pb.TransactionOpType_TRANSACTION_OP_TYPE_GET},
		{service.TransactionOpTypeDelete, pb.TransactionOpType_TRANSACTION_OP_TYPE_DELETE},
	}
	for _, p := range pairs {
		if got := convertTransactionOpTypeToProto(p.sdk); got != p.pb {
			t.Errorf("toProto(%v) = %v, want %v", p.sdk, got, p.pb)
		}
		if got := convertTransactionOpTypeFromProto(p.pb); got != p.sdk {
			t.Errorf("fromProto(%v) = %v, want %v", p.pb, got, p.sdk)
		}
	}
	if got := convertTransactionOpTypeToProto(service.TransactionOpType(999)); got != pb.TransactionOpType_TRANSACTION_OP_TYPE_UNSPECIFIED {
		t.Errorf("toProto(unknown) = %v", got)
	}
	if got := convertTransactionOpTypeFromProto(pb.TransactionOpType_TRANSACTION_OP_TYPE_UNSPECIFIED); got != service.TransactionOpTypeUnspecified {
		t.Errorf("fromProto(unspecified) = %v", got)
	}
}

// --- Vector enum conversions ---

func TestConvertIndexType_RoundTrip(t *testing.T) {
	if convertIndexTypeToProto(vector.IndexTypeHNSW) != pb.IndexType_INDEX_TYPE_HNSW {
		t.Error("HNSW toProto mismatch")
	}
	if convertIndexTypeToProto(vector.IndexTypeFlat) != pb.IndexType_INDEX_TYPE_FLAT {
		t.Error("Flat toProto mismatch")
	}
	if convertIndexTypeToProto(vector.IndexTypeIVF) != pb.IndexType_INDEX_TYPE_IVF_FLAT {
		t.Error("IVF toProto mismatch")
	}
	if convertIndexTypeToProto(vector.IndexType("zzz")) != pb.IndexType_INDEX_TYPE_UNSPECIFIED {
		t.Error("unknown toProto mismatch")
	}
	if convertIndexTypeFromProto(pb.IndexType_INDEX_TYPE_HNSW) != vector.IndexTypeHNSW {
		t.Error("HNSW fromProto mismatch")
	}
	if convertIndexTypeFromProto(pb.IndexType_INDEX_TYPE_FLAT) != vector.IndexTypeFlat {
		t.Error("Flat fromProto mismatch")
	}
	if convertIndexTypeFromProto(pb.IndexType_INDEX_TYPE_IVF_PQ) != vector.IndexTypeIVF {
		t.Error("IVF_PQ fromProto mismatch")
	}
	if convertIndexTypeFromProto(pb.IndexType_INDEX_TYPE_UNSPECIFIED) != vector.IndexTypeHNSW {
		t.Error("unspecified fromProto should default to HNSW")
	}
}

func TestConvertFilterOperatorToProto_AllBranches(t *testing.T) {
	cases := map[vector.FilterOperator]pb.FilterOperator{
		vector.FilterOperatorEquals:    pb.FilterOperator_FILTER_OPERATOR_EQUALS,
		vector.FilterOperatorNotEquals: pb.FilterOperator_FILTER_OPERATOR_NOT_EQUALS,
		vector.FilterOperatorContains:  pb.FilterOperator_FILTER_OPERATOR_CONTAINS,
		vector.FilterOperatorIn:        pb.FilterOperator_FILTER_OPERATOR_IN,
		vector.FilterOperator("zzz"):   pb.FilterOperator_FILTER_OPERATOR_UNSPECIFIED,
	}
	for in, want := range cases {
		if got := convertFilterOperatorToProto(in); got != want {
			t.Errorf("convertFilterOperatorToProto(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestConvertNilVectorConversions(t *testing.T) {
	if convertNamespaceConfigToProto(nil) != nil {
		t.Error("nil NamespaceConfig should map to nil")
	}
	if convertVectorEntityToProto(nil) != nil {
		t.Error("nil VectorEntity should map to nil")
	}
	if convertVectorEntityFromProto(nil) != nil {
		t.Error("nil proto VectorEntity should map to nil")
	}
	if convertSimilarityResultFromProto(nil) != nil {
		t.Error("nil SimilarityResult should map to nil")
	}
	if convertMetadataFilterToProto(nil) != nil {
		t.Error("nil MetadataFilter should map to nil")
	}
}

// --- Cluster / embedding / iam helper conversions ---

func TestConvertHealthStatusToNodeState_AllBranches(t *testing.T) {
	cases := map[pb.HealthStatus]cluster.NodeState{
		pb.HealthStatus_HEALTH_STATUS_HEALTHY:     cluster.NodeStateHealthy,
		pb.HealthStatus_HEALTH_STATUS_DEGRADED:    cluster.NodeStateUnhealthy,
		pb.HealthStatus_HEALTH_STATUS_UNHEALTHY:   cluster.NodeStateUnhealthy,
		pb.HealthStatus_HEALTH_STATUS_UNSPECIFIED: cluster.NodeStateUnknown,
	}
	for in, want := range cases {
		if got := convertHealthStatusToNodeState(in); got != want {
			t.Errorf("convertHealthStatusToNodeState(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestProtoStatusToSDK_AllBranches(t *testing.T) {
	cases := map[pb.EmbeddingStatus]embedding.Status{
		pb.EmbeddingStatus_EMBEDDING_STATUS_AVAILABLE:    embedding.StatusAvailable,
		pb.EmbeddingStatus_EMBEDDING_STATUS_UNAVAILABLE:  embedding.StatusUnavailable,
		pb.EmbeddingStatus_EMBEDDING_STATUS_RATE_LIMITED: embedding.StatusRateLimited,
		pb.EmbeddingStatus_EMBEDDING_STATUS_ERROR:        embedding.StatusError,
		pb.EmbeddingStatus_EMBEDDING_STATUS_UNSPECIFIED:  embedding.Status(""),
	}
	for in, want := range cases {
		if got := protoStatusToSDK(in); got != want {
			t.Errorf("protoStatusToSDK(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestProviderTypeToString_AllBranches(t *testing.T) {
	cases := map[iampb.ProviderType]string{
		iampb.ProviderType_PROVIDER_TYPE_OIDC:        "oidc",
		iampb.ProviderType_PROVIDER_TYPE_SAML:        "saml",
		iampb.ProviderType_PROVIDER_TYPE_OAUTH2:      "oauth2",
		iampb.ProviderType_PROVIDER_TYPE_UNSPECIFIED: "",
	}
	for in, want := range cases {
		if got := providerTypeToString(in); got != want {
			t.Errorf("providerTypeToString(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestConvertProtoConsentStatus_AllBranches(t *testing.T) {
	if convertProtoConsentStatus(iampb.ConsentStatus_CONSENT_STATUS_GRANTED) != iam.ConsentStatusGranted {
		t.Error("granted mismatch")
	}
	if convertProtoConsentStatus(iampb.ConsentStatus_CONSENT_STATUS_WITHDRAWN) != iam.ConsentStatusWithdrawn {
		t.Error("withdrawn mismatch")
	}
	// Default branch casts the enum directly.
	if got := convertProtoConsentStatus(iampb.ConsentStatus_CONSENT_STATUS_DENIED); got != iam.ConsentStatus(iampb.ConsentStatus_CONSENT_STATUS_DENIED) {
		t.Errorf("default branch mismatch: %v", got)
	}
}

// --- Timestamp helpers ---

func TestProtoTimestamp(t *testing.T) {
	if protoTimestamp(time.Time{}) != nil {
		t.Error("zero time should map to nil")
	}
	if protoTimestamp(time.Unix(1, 0)) == nil {
		t.Error("non-zero time should produce a timestamp")
	}
}

func TestProtoTimestampPtr(t *testing.T) {
	if protoTimestampPtr(nil) != nil {
		t.Error("nil pointer should map to nil")
	}
	zero := time.Time{}
	if protoTimestampPtr(&zero) != nil {
		t.Error("zero time pointer should map to nil")
	}
	now := time.Unix(2, 0)
	if protoTimestampPtr(&now) == nil {
		t.Error("non-zero time pointer should produce a timestamp")
	}
}

func TestTimestampToProto_Placeholder(t *testing.T) {
	if timestampToProto(nil) != nil {
		t.Error("nil input should map to nil")
	}
	if timestampToProto("anything") == nil {
		t.Error("non-nil input should produce a timestamp placeholder")
	}
}

func TestGdprTimestampToProto(t *testing.T) {
	if gdprTimestampToProto(time.Now()) != nil {
		t.Error("gdprTimestampToProto is intentionally a no-op returning nil")
	}
}

func TestConvertTimestampFromProto(t *testing.T) {
	if !convertTimestampFromProto(nil).IsZero() {
		t.Error("nil timestamp should map to the zero time")
	}
	ts := timestamppb.New(time.Unix(100, 0))
	if convertTimestampFromProto(ts).Unix() != 100 {
		t.Error("timestamp conversion mismatch")
	}
}

// --- getInterfaceSlice helper ---

func TestGetInterfaceSlice(t *testing.T) {
	if _, ok := getInterfaceSlice(nil, "k"); ok {
		t.Error("nil map should return ok=false")
	}
	if _, ok := getInterfaceSlice(map[string]interface{}{}, "missing"); ok {
		t.Error("missing key should return ok=false")
	}
	if _, ok := getInterfaceSlice(map[string]interface{}{"k": "notaslice"}, "k"); ok {
		t.Error("wrong type should return ok=false")
	}
	got, ok := getInterfaceSlice(map[string]interface{}{"k": []interface{}{1, 2}}, "k")
	if !ok || len(got) != 2 {
		t.Errorf("expected 2-element slice, got %v ok=%v", got, ok)
	}
}
