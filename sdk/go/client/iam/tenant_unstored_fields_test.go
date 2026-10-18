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
	"errors"
	"reflect"
	"testing"
)

// TestTenantClient_UpdateRefusesUnstoredFields proves an update that sets
// Description, Metadata or Status is refused whole before dispatch, naming
// the fields in a fixed order, so nothing is reported stored that is not
// (#1518).
func TestTenantClient_UpdateRefusesUnstoredFields(t *testing.T) {
	status := TenantStatusActive
	cases := []struct {
		name   string
		req    *UpdateTenantRequest
		fields []string
	}{
		{"description", &UpdateTenantRequest{Description: "d"}, []string{"description"}},
		{"metadata", &UpdateTenantRequest{Metadata: map[string]string{"k": "v"}}, []string{"metadata"}},
		{"status", &UpdateTenantRequest{Status: &status}, []string{"status"}},
		{"all three with display name", &UpdateTenantRequest{
			DisplayName: "Acme",
			Status:      &status,
			Metadata:    map[string]string{"k": "v"},
			Description: "d",
		}, []string{"description", "metadata", "status"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			mt := &mockTransport{
				requestFunc: func(context.Context, string, string, interface{}) (interface{}, error) {
					calls++
					return &Tenant{}, nil
				},
			}

			_, err := NewTenantClient(mt).Update(context.Background(), "acme", tc.req)

			if !errors.Is(err, ErrTenantFieldNotStored) {
				t.Fatalf("err = %v, want ErrTenantFieldNotStored", err)
			}
			var refused *TenantFieldNotStoredError
			if !errors.As(err, &refused) || !reflect.DeepEqual(refused.Fields, tc.fields) {
				t.Fatalf("err = %v, want TenantFieldNotStoredError%v", err, tc.fields)
			}
			if calls != 0 {
				t.Fatalf("transport called %d time(s), want none", calls)
			}
		})
	}
}

// TestTenantClient_UpdateDisplayNameStillDispatches proves a DisplayName-only
// update, and an empty Metadata map, are not refused.
func TestTenantClient_UpdateDisplayNameStillDispatches(t *testing.T) {
	calls := 0
	mt := &mockTransport{
		requestFunc: func(context.Context, string, string, interface{}) (interface{}, error) {
			calls++
			return &Tenant{ID: "acme", DisplayName: "Acme"}, nil
		},
	}

	_, err := NewTenantClient(mt).Update(context.Background(), "acme", &UpdateTenantRequest{
		DisplayName: "Acme",
		Metadata:    map[string]string{},
	})

	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if calls != 1 {
		t.Fatalf("transport called %d time(s), want 1", calls)
	}
}
