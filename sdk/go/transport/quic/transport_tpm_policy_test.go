package quic

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// TestTPMPolicy_QUIC_ListReachesGoXKMSRoute proves the QUIC transport carries
// the TPM-policy verbs over the REST dialect: TPMPolicyList reaches the
// go-xkms route and decodes into the typed response.
func TestTPMPolicy_QUIC_ListReachesGoXKMSRoute(t *testing.T) {
	var (
		mu       sync.Mutex
		requests []string
	)
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"policies":[{"name":"policy-a"}]}`))
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer tr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := tr.Request(ctx, "quicbaas.TPMPolicy", "TPMPolicyList",
		&xkmstransport.TPMPolicyListRequest{TenantID: "tenant-a"})
	if err != nil {
		t.Fatalf("TPMPolicyList: %v", err)
	}
	resp, ok := result.(*xkmstransport.TPMPolicyListResponse)
	if !ok || len(resp.Policies) != 1 || resp.Policies[0].Name != "policy-a" {
		t.Fatalf("TPMPolicyList result %#v", result)
	}

	mu.Lock()
	sent := append([]string(nil), requests...)
	mu.Unlock()
	want := "GET /api/v1/xkms/tpm/policies?tenant_id=tenant-a"
	if len(sent) != 1 || sent[0] != want {
		t.Fatalf("server saw %v, want [%s]", sent, want)
	}
}
