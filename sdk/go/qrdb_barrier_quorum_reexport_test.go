// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package quicbaas_test

import (
	"context"
	"testing"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// The barrier quorum, rekey and officer verbs are part of Security().Barrier()
// and the tenant barrier surface. A consumer that imports only go-quicbaas
// must be able to name every request and response those verbs carry, so each
// method expression below is typed with quicbaas re-exports alone.

func TestBarrierQuorumReexports_NameEverySystemVerb(t *testing.T) {
	verbs := map[string]any{
		"BarrierQuorumBegin": func(c quicbaas.QRDBBarrierClient, ctx context.Context, req *quicbaas.QRDBQuorumBeginRequest) (*quicbaas.QRDBQuorumChallenge, error) {
			return c.BarrierQuorumBegin(ctx, req)
		},
		"BarrierQuorumSubmit": func(c quicbaas.QRDBBarrierClient, ctx context.Context, req *quicbaas.QRDBQuorumSubmitRequest) (*quicbaas.QRDBQuorumOutcome, error) {
			return c.BarrierQuorumSubmit(ctx, req)
		},
		"BarrierRekeyStart": func(c quicbaas.QRDBBarrierClient, ctx context.Context, req *quicbaas.QRDBRekeyStartRequest) (*quicbaas.QRDBQuorumRound, error) {
			return c.BarrierRekeyStart(ctx, req)
		},
		"BarrierOfficerRevoke": func(c quicbaas.QRDBBarrierClient, ctx context.Context, req *quicbaas.QRDBOfficerRevokeRequest) (*quicbaas.QRDBQuorumRound, error) {
			return c.BarrierOfficerRevoke(ctx, req)
		},
	}
	if len(verbs) != 4 {
		t.Fatalf("named %d system barrier verbs, want 4", len(verbs))
	}
}

func TestBarrierQuorumReexports_NameEveryTenantVerb(t *testing.T) {
	verbs := map[string]any{
		"TenantBarrierOfficerRegister": func(c quicbaas.QRDBTenantClient, ctx context.Context, req *quicbaas.QRDBTenantOfficerRegisterRequest) (*quicbaas.QRDBOfficerRegisterResponse, error) {
			return c.TenantBarrierOfficerRegister(ctx, req)
		},
		"TenantBarrierShareClaimBegin": func(c quicbaas.QRDBTenantClient, ctx context.Context, req *quicbaas.QRDBTenantShareClaimBeginRequest) (*quicbaas.QRDBShareClaimBeginResponse, error) {
			return c.TenantBarrierShareClaimBegin(ctx, req)
		},
		"TenantBarrierShareClaimComplete": func(c quicbaas.QRDBTenantClient, ctx context.Context, req *quicbaas.QRDBTenantShareClaimCompleteRequest) (*quicbaas.QRDBShareClaimCompleteResponse, error) {
			return c.TenantBarrierShareClaimComplete(ctx, req)
		},
		"TenantBarrierQuorumBegin": func(c quicbaas.QRDBTenantClient, ctx context.Context, req *quicbaas.QRDBTenantQuorumBeginRequest) (*quicbaas.QRDBQuorumChallenge, error) {
			return c.TenantBarrierQuorumBegin(ctx, req)
		},
		"TenantBarrierQuorumSubmit": func(c quicbaas.QRDBTenantClient, ctx context.Context, req *quicbaas.QRDBTenantQuorumSubmitRequest) (*quicbaas.QRDBQuorumOutcome, error) {
			return c.TenantBarrierQuorumSubmit(ctx, req)
		},
		"TenantBarrierRekeyStart": func(c quicbaas.QRDBTenantClient, ctx context.Context, req *quicbaas.QRDBTenantRekeyStartRequest) (*quicbaas.QRDBQuorumRound, error) {
			return c.TenantBarrierRekeyStart(ctx, req)
		},
		"TenantBarrierOfficerRevoke": func(c quicbaas.QRDBTenantClient, ctx context.Context, req *quicbaas.QRDBTenantOfficerRevokeRequest) (*quicbaas.QRDBQuorumRound, error) {
			return c.TenantBarrierOfficerRevoke(ctx, req)
		},
	}
	if len(verbs) != 7 {
		t.Fatalf("named %d tenant barrier verbs, want 7", len(verbs))
	}
}
