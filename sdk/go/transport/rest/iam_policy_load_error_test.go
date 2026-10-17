package rest

import (
	"encoding/json"
	"testing"

	iam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// serverPolicyListJSON is the server's REST policy list with one flagged
// record and one healthy record.
const serverPolicyListJSON = `{"policies":[
	{"apiVersion":"iam.quicbaas.io/v1","kind":"Policy",
	 "metadata":{"name":"flagged","tenant":"t1"},"spec":{},
	 "load_error":{"reason":"json: unknown field \"bogus\"","unknown_keys":["spec.rules[0].bogus"]}},
	{"apiVersion":"iam.quicbaas.io/v1","kind":"Policy",
	 "metadata":{"name":"healthy","tenant":"t1","description":"applied"},"spec":{}}
],"limit":100,"offset":0}`

func decodeSDKPolicyList(t *testing.T, serverJSON string) []iam.Policy {
	t.Helper()
	var resp interface{}
	if err := json.Unmarshal([]byte(serverJSON), &resp); err != nil {
		t.Fatalf("server JSON: %v", err)
	}
	mapped, err := json.Marshal(mapServerPolicyListToSDK(resp))
	if err != nil {
		t.Fatalf("marshal mapped list: %v", err)
	}
	var list struct {
		Policies []iam.Policy `json:"policies"`
	}
	if err := json.Unmarshal(mapped, &list); err != nil {
		t.Fatalf("decode SDK list: %v", err)
	}
	return list.Policies
}

func TestMapServerPolicyListToSDK_CarriesLoadError(t *testing.T) {
	policies := decodeSDKPolicyList(t, serverPolicyListJSON)
	if len(policies) != 2 {
		t.Fatalf("got %d policies, want 2", len(policies))
	}

	flagged := policies[0]
	if flagged.Name != "flagged" || flagged.LoadError == nil {
		t.Fatalf("flagged policy lost its load_error: %+v", flagged)
	}
	if flagged.LoadError.Reason != `json: unknown field "bogus"` {
		t.Errorf("Reason = %q", flagged.LoadError.Reason)
	}
	if len(flagged.LoadError.UnknownKeys) != 1 || flagged.LoadError.UnknownKeys[0] != "spec.rules[0].bogus" {
		t.Errorf("UnknownKeys = %v", flagged.LoadError.UnknownKeys)
	}
}

func TestMapServerPolicyListToSDK_HealthyPolicyHasNoLoadError(t *testing.T) {
	policies := decodeSDKPolicyList(t, serverPolicyListJSON)
	if len(policies) != 2 {
		t.Fatalf("got %d policies, want 2", len(policies))
	}
	if healthy := policies[1]; healthy.LoadError != nil {
		t.Errorf("healthy policy has a load_error: %+v", healthy.LoadError)
	}
}

func TestMapServerPolicyToSDK_LoadErrorRequiresServerShape(t *testing.T) {
	// A response that is not a server-format policy passes through as is.
	sdkShape := map[string]interface{}{"name": "p1", "load_error": "unexpected"}
	got, ok := mapServerPolicyToSDK(sdkShape).(map[string]interface{})
	if !ok || got["load_error"] != "unexpected" {
		t.Errorf("SDK-shaped response was rewritten: %v", got)
	}
}
