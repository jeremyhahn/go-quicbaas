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

package account

import (
	"context"
	"testing"
)

func TestClient_SessionReservations_Dispatch(t *testing.T) {
	tr := &mockTransport{response: &SessionReservation{ReservationID: "r1"}}
	c := NewClient(tr)
	ctx := context.Background()

	got, err := c.ReserveSession(ctx, &ReserveSessionRequest{TenantID: "t", AccountID: "a"})
	if err != nil || got.ReservationID != "r1" || tr.method != "ReserveSession" {
		t.Fatalf("ReserveSession = (%+v, %v) via %q", got, err, tr.method)
	}
	if _, err := c.ReleaseSession(ctx, &ReleaseSessionRequest{TenantID: "t", AccountID: "a", ReservationID: "r1"}); err != nil || tr.method != "ReleaseSession" {
		t.Fatalf("ReleaseSession = %v via %q", err, tr.method)
	}
}

func TestClient_SessionReservations_RejectBadInput(t *testing.T) {
	ctx := context.Background()
	c := NewClient(&mockTransport{response: &SessionReservation{}})
	for name, call := range map[string]func() error{
		"reserve nil":       func() error { _, err := c.ReserveSession(ctx, nil); return err },
		"reserve no tenant": func() error { _, err := c.ReserveSession(ctx, &ReserveSessionRequest{AccountID: "a"}); return err },
		"release nil":       func() error { _, err := c.ReleaseSession(ctx, nil); return err },
		"release no id": func() error {
			_, err := c.ReleaseSession(ctx, &ReleaseSessionRequest{TenantID: "t", AccountID: "a"})
			return err
		},
	} {
		if call() == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	wrong := NewClient(&mockTransport{response: sampleInfo()})
	if _, err := wrong.ReserveSession(ctx, &ReserveSessionRequest{TenantID: "t", AccountID: "a"}); err == nil {
		t.Error("a response of the wrong shape must be an error")
	}
}
