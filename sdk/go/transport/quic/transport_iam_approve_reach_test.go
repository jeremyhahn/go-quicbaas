package quic

// The QUIC transport speaks the REST dialect, so the IAM user Approve and
// Reject verbs and the policy GetEffectivePermissions verb reach the same
// quicbaasd routes the REST transport sends them to.

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	iam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

func TestIAMUserApprove_QUIC_Reach(t *testing.T) {
	var (
		mu       sync.Mutex
		requests []string
	)
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","user_id":"u-1"}`))
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer tr.Close()

	users := iam.NewUserClient(tr)
	policies := iam.NewPolicyClient(tr)
	cases := []struct {
		name  string
		call  func(context.Context) error
		route string
	}{
		{"Approve", func(ctx context.Context) error {
			return users.Approve(ctx, "t-1", "u-1")
		}, "POST /api/v1/iam/users/u-1/approve?tenant_id=t-1"},
		{"Reject", func(ctx context.Context) error {
			return users.Reject(ctx, "t-1", "u-1", "spam signup")
		}, "POST /api/v1/iam/users/u-1/reject?tenant_id=t-1"},
		{"GetEffectivePermissions", func(ctx context.Context) error {
			_, perr := policies.GetEffectivePermissions(ctx, "t-1",
				&iam.EffectivePermissionsRequest{Subject: iam.Subject{ID: "u-1"}})
			return perr
		}, "POST /api/v1/tenants/t-1/policies/effective-permissions"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			mu.Lock()
			before := len(requests)
			mu.Unlock()

			if err := c.call(ctx); err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}

			mu.Lock()
			sent := append([]string(nil), requests[before:]...)
			mu.Unlock()
			if len(sent) != 1 || sent[0] != c.route {
				t.Errorf("%s: server saw %v, want [%s]", c.name, sent, c.route)
			}
		})
	}
}
