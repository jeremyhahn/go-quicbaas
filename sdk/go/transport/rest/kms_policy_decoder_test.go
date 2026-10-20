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

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// getPolicyFrom serves policyJSON as the GetKeyPolicy body and returns what
// the SDK REST handler decodes from it.
func getPolicyFrom(t *testing.T, policyJSON string) (*service.GetKeyPolicyResponse, error) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"policy":` + policyJSON + `,"timestamp":1700000000}`)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	resp, err := tr.kmsHandler.handleGetKeyPolicy(context.Background(), &kmsKeyIDReq{KeyID: "k1"})
	if err != nil {
		return nil, err
	}
	return resp.(*service.GetKeyPolicyResponse), nil
}

func TestKMSRESTGetKeyPolicy_DecodesRestrictions(t *testing.T) {
	resp, err := getPolicyFrom(t, `{"key_id":"k1","version":3,"allowed_operations":["sign"],`+
		`"allowed_principals":["app-a"],"denied_principals":["intruder"],`+
		`"expires_at":"2031-02-03T04:05:06.789Z","max_usage_count":500,"updated_at":1700000000}`)
	if err != nil {
		t.Fatalf("GetKeyPolicy: %v", err)
	}
	p := resp.Policy
	wantExpiry := time.Date(2031, 2, 3, 4, 5, 6, 789000000, time.UTC)
	if p.ExpiresAt == nil || !p.ExpiresAt.Equal(wantExpiry) {
		t.Errorf("ExpiresAt = %v, want %v", p.ExpiresAt, wantExpiry)
	}
	if p.MaxUsageCount == nil || *p.MaxUsageCount != 500 {
		t.Errorf("MaxUsageCount = %v, want 500", p.MaxUsageCount)
	}
	if len(p.AllowedPrincipals) != 1 || p.AllowedPrincipals[0] != "app-a" {
		t.Errorf("AllowedPrincipals = %v", p.AllowedPrincipals)
	}
	if len(p.DeniedPrincipals) != 1 || p.DeniedPrincipals[0] != "intruder" {
		t.Errorf("DeniedPrincipals = %v", p.DeniedPrincipals)
	}
}

func TestKMSRESTGetKeyPolicy_DecodesUnrestricted(t *testing.T) {
	resp, err := getPolicyFrom(t, `{"key_id":"k1","allowed_operations":["sign"],`+
		`"allowed_principals":[],"denied_principals":[],"expires_at":null,"max_usage_count":null}`)
	if err != nil {
		t.Fatalf("GetKeyPolicy: %v", err)
	}
	p := resp.Policy
	if p.ExpiresAt != nil || p.MaxUsageCount != nil {
		t.Errorf("want no expiry and no limit, got %v / %v", p.ExpiresAt, p.MaxUsageCount)
	}
	if p.AllowedPrincipals == nil || len(p.AllowedPrincipals) != 0 || len(p.DeniedPrincipals) != 0 {
		t.Errorf("want empty principal lists, got %v / %v", p.AllowedPrincipals, p.DeniedPrincipals)
	}
}

func TestKMSRESTGetKeyPolicy_NullPolicyIsNoPolicy(t *testing.T) {
	resp, err := getPolicyFrom(t, `null`)
	if err != nil {
		t.Fatalf("GetKeyPolicy: %v", err)
	}
	if resp.Policy != nil {
		t.Errorf("Policy = %+v, want nil", resp.Policy)
	}
}

func TestKMSRESTGetKeyPolicy_RefusesMissingOrMalformedFields(t *testing.T) {
	const allowed = `"allowed_principals":[]`
	const denied = `"denied_principals":[]`
	const expiry = `"expires_at":null`
	const usage = `"max_usage_count":null`
	cases := map[string]string{
		"missing allowed_principals": `{` + denied + `,` + expiry + `,` + usage + `}`,
		"null allowed_principals":    `{"allowed_principals":null,` + denied + `,` + expiry + `,` + usage + `}`,
		"string allowed_principals":  `{"allowed_principals":"app-a",` + denied + `,` + expiry + `,` + usage + `}`,
		"missing denied_principals":  `{` + allowed + `,` + expiry + `,` + usage + `}`,
		"numeric denied_principals":  `{` + allowed + `,"denied_principals":[7],` + expiry + `,` + usage + `}`,
		"missing expires_at":         `{` + allowed + `,` + denied + `,` + usage + `}`,
		"unix expires_at":            `{` + allowed + `,` + denied + `,"expires_at":1800000000,` + usage + `}`,
		"non RFC 3339 expires_at":    `{` + allowed + `,` + denied + `,"expires_at":"next tuesday",` + usage + `}`,
		"missing max_usage_count":    `{` + allowed + `,` + denied + `,` + expiry + `}`,
		"string max_usage_count":     `{` + allowed + `,` + denied + `,` + expiry + `,"max_usage_count":"ten"}`,
		"negative max_usage_count":   `{` + allowed + `,` + denied + `,` + expiry + `,"max_usage_count":-1}`,
	}
	for name, policy := range cases {
		t.Run(name, func(t *testing.T) {
			resp, err := getPolicyFrom(t, policy)
			if err == nil {
				t.Fatalf("want an error, got policy %+v", resp.Policy)
			}
		})
	}
}
