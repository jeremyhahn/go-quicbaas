package rest

// Tests for the REST SDK transport's quicbaas.IAM.User Approve and Reject
// verbs, which reach quicbaasd's
// POST /api/v1/iam/users/{user_id}/approve|reject?tenant_id= routes.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	iam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestIAMUserApprove_REST_PostsApproveRoute(t *testing.T) {
	rec := newIAMRecorder(t)
	rec.respBody = `{"status":"approved","user_id":"u-1"}`
	tr := newTestTransport(t, rec.server)

	if err := iam.NewUserClient(tr).Approve(context.Background(), "t-1", "u-1"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if rec.gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", rec.gotMethod)
	}
	if want := "/api/v1/iam/users/u-1/approve?tenant_id=t-1"; rec.gotRawPath != want {
		t.Errorf("request URI = %q, want %q", rec.gotRawPath, want)
	}
	if len(rec.gotBody) != 0 {
		t.Errorf("approve body = %q, want none", rec.gotBody)
	}
}

func TestIAMUserApprove_REST_RejectPostsRejectRouteWithReason(t *testing.T) {
	rec := newIAMRecorder(t)
	rec.respBody = `{"status":"rejected","user_id":"u-1"}`
	tr := newTestTransport(t, rec.server)

	if err := iam.NewUserClient(tr).Reject(context.Background(), "t-1", "u-1", "spam signup"); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if rec.gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", rec.gotMethod)
	}
	if want := "/api/v1/iam/users/u-1/reject?tenant_id=t-1"; rec.gotRawPath != want {
		t.Errorf("request URI = %q, want %q", rec.gotRawPath, want)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rec.gotBody, &body); err != nil {
		t.Fatalf("reject body %q: %v", rec.gotBody, err)
	}
	if body["reason"] != "spam signup" {
		t.Errorf("reject body reason = %v, want %q", body["reason"], "spam signup")
	}
}

func TestIAMUserApprove_REST_RejectWithoutReasonSendsNoBody(t *testing.T) {
	rec := newIAMRecorder(t)
	tr := newTestTransport(t, rec.server)

	if err := iam.NewUserClient(tr).Reject(context.Background(), "t-1", "u-1", ""); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if want := "/api/v1/iam/users/u-1/reject?tenant_id=t-1"; rec.gotRawPath != want {
		t.Errorf("request URI = %q, want %q", rec.gotRawPath, want)
	}
	if len(rec.gotBody) != 0 {
		t.Errorf("reject body = %q, want none", rec.gotBody)
	}
}

func TestIAMUserApprove_REST_EscapesPathAndQuery(t *testing.T) {
	rec := newIAMRecorder(t)
	tr := newTestTransport(t, rec.server)

	if err := iam.NewUserClient(tr).Approve(context.Background(), "t 1&x=y", "u/1"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if want := "/api/v1/iam/users/u%2F1/approve?tenant_id=t+1%26x%3Dy"; rec.gotRawPath != want {
		t.Errorf("request URI = %q, want %q", rec.gotRawPath, want)
	}
}

func TestIAMUserApprove_REST_EmptyUserIDIsInvalidInput(t *testing.T) {
	for _, method := range []string{"Approve", "Reject"} {
		t.Run(method, func(t *testing.T) {
			rec := newIAMRecorder(t)
			tr := newTestTransport(t, rec.server)

			_, err := tr.Request(context.Background(), iamUserServicePrefix, method,
				map[string]interface{}{"tenant_id": "t-1"})
			if errors.Code(err) != errors.CodeInvalidInput {
				t.Fatalf("err = %v, want invalid input", err)
			}
			if rec.gotMethod != "" {
				t.Errorf("a request was sent: %s %s", rec.gotMethod, rec.gotRawPath)
			}
		})
	}
}

func TestIAMUserApprove_REST_ServerErrorsMap(t *testing.T) {
	cases := []struct {
		status int
		check  func(error) bool
		name   string
	}{
		{http.StatusNotFound, errors.IsNotFound, "not found"},
		{http.StatusForbidden, errors.IsForbidden, "forbidden"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":"refused"}`))
			}))
			defer server.Close()

			client := iam.NewUserClient(newTestTransport(t, server))
			if err := client.Approve(context.Background(), "t-1", "u-1"); !tc.check(err) {
				t.Errorf("Approve err = %v, want %s", err, tc.name)
			}
			if err := client.Reject(context.Background(), "t-1", "u-1", "r"); !tc.check(err) {
				t.Errorf("Reject err = %v, want %s", err, tc.name)
			}
		})
	}
}
