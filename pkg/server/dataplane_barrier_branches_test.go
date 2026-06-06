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

//go:build !integration

// dataplane_barrier_branches_test.go covers the error branches of
// buildSealStrategies (Shamir construction failure) and the
// strategyAwareBarrierService decorator (inner-service error propagation), which
// the happy-path integration tests do not reach.

package server

import (
	"context"
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// TestBuildSealStrategies_ShamirInvalidParams covers the Shamir construction
// error branch: an invalid threshold/total combination (threshold greater than
// total shares) must surface as a typed DataPlaneBarrierError rather than a
// usable strategy.
func TestBuildSealStrategies_ShamirInvalidParams(t *testing.T) {
	strats, err := buildSealStrategies(qrdbsdk.StrategyShamir, 5, 3) // threshold > total
	if err == nil {
		t.Fatalf("expected an error for threshold > total, got strategies %v", strats)
	}
	if strats != nil {
		t.Fatalf("expected nil strategies on Shamir construction failure, got %v", strats)
	}
	var dpErr *DataPlaneBarrierError
	if !errors.As(err, &dpErr) {
		t.Fatalf("expected *DataPlaneBarrierError, got %T: %v", err, err)
	}
	if dpErr.Stage != "construct shamir strategy" {
		t.Fatalf("DataPlaneBarrierError.Stage = %q, want %q", dpErr.Stage, "construct shamir strategy")
	}
}

// errBarrierService embeds BaasBarrierService (nil) and overrides BarrierStatus
// to fail, driving the error-propagation branch of the
// strategyAwareBarrierService decorator. No other method is reached by the
// decorator's BarrierStatus, so the nil embedding is safe.
type errBarrierService struct {
	BaasBarrierService
	err error
}

func (e *errBarrierService) BarrierStatus(context.Context) (*qrdbsdk.BarrierStatusInfo, error) {
	return nil, e.err
}

// TestStrategyAwareBarrierService_BarrierStatusError covers the decorator's
// error-propagation branch: when the wrapped service's BarrierStatus fails, the
// decorator returns the error verbatim and does not attempt to substitute the
// configured strategy.
func TestStrategyAwareBarrierService_BarrierStatusError(t *testing.T) {
	sentinel := errors.New("barrier status backend down")
	svc := &strategyAwareBarrierService{
		BaasBarrierService: &errBarrierService{err: sentinel},
		configuredStrategy: string(qrdbsdk.StrategyShamir),
	}

	info, err := svc.BarrierStatus(context.Background())
	if !errors.Is(err, sentinel) {
		t.Fatalf("BarrierStatus err = %v, want %v", err, sentinel)
	}
	if info != nil {
		t.Fatalf("expected nil status info on error, got %v", info)
	}
}
