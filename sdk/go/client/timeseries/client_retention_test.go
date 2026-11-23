// Copyright (c) 2025 Jeremy Hahn

package timeseries

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestClient_SetRetention_SendsRequest(t *testing.T) {
	var gotService, gotMethod string
	var gotReq interface{}
	c := NewClient(&mockTransport{
		requestFn: func(_ context.Context, service, method string, req interface{}) (interface{}, error) {
			gotService, gotMethod, gotReq = service, method, req
			return nil, nil
		},
	})
	req := &SetRetentionRequest{
		Metric: "cpu",
		Policy: RetentionPolicy{Duration: time.Hour, Resolution: time.Minute, Aggregation: AggregateAvg},
	}
	if err := c.SetRetention(context.Background(), req); err != nil {
		t.Fatalf("SetRetention: %v", err)
	}
	if gotService != serviceName || gotMethod != "SetRetention" {
		t.Errorf("routed to %s/%s, want %s/SetRetention", gotService, gotMethod, serviceName)
	}
	if gotReq != req {
		t.Errorf("request not passed through: got %#v", gotReq)
	}
}

func TestClient_SetRetention_Validation(t *testing.T) {
	c := NewClient(&mockTransport{
		requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			t.Fatal("an invalid request must not reach the transport")
			return nil, nil
		},
	})
	cases := map[string]*SetRetentionRequest{
		"nil":                     nil,
		"no metric":               {Policy: RetentionPolicy{Duration: time.Hour}},
		"negative duration":       {Metric: "cpu", Policy: RetentionPolicy{Duration: -time.Second}},
		"sub-second duration":     {Metric: "cpu", Policy: RetentionPolicy{Duration: 1500 * time.Millisecond}},
		"negative resolution":     {Metric: "cpu", Policy: RetentionPolicy{Duration: time.Hour, Resolution: -time.Second}},
		"bad aggregate":           {Metric: "cpu", Policy: RetentionPolicy{Duration: time.Hour, Resolution: time.Minute, Aggregation: AggregateFunction(99)}},
		"aggregate no resolution": {Metric: "cpu", Policy: RetentionPolicy{Duration: time.Hour, Aggregation: AggregateSum}},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			err := c.SetRetention(context.Background(), req)
			if !errors.IsInvalidInput(err) {
				t.Fatalf("want InvalidInput, got %v", err)
			}
		})
	}
}

func TestClient_GetRetention(t *testing.T) {
	want := &GetRetentionResponse{Metric: "cpu", Policy: &RetentionPolicy{Duration: time.Hour}}
	var gotMethod string
	c := NewClient(&mockTransport{
		requestFn: func(_ context.Context, _, method string, _ interface{}) (interface{}, error) {
			gotMethod = method
			return want, nil
		},
	})
	got, err := c.GetRetention(context.Background(), &GetRetentionRequest{Metric: "cpu"})
	if err != nil {
		t.Fatalf("GetRetention: %v", err)
	}
	if gotMethod != "GetRetention" || got != want {
		t.Fatalf("GetRetention: method %q response %#v", gotMethod, got)
	}
}

// TestClient_GetRetention_ZeroPolicyIsNil pins that a zero policy from any
// transport reads nil, while a policy with any field set stays non-nil.
func TestClient_GetRetention_ZeroPolicyIsNil(t *testing.T) {
	for _, tc := range []struct {
		name    string
		policy  *RetentionPolicy
		wantNil bool
	}{
		{"zero", &RetentionPolicy{}, true},
		{"aggregation only", &RetentionPolicy{Aggregation: AggregateAvg}, false},
		{"resolution only", &RetentionPolicy{Resolution: time.Minute}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClient(&mockTransport{
				requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
					return &GetRetentionResponse{Metric: "m", Policy: tc.policy}, nil
				},
			})
			got, err := c.GetRetention(context.Background(), &GetRetentionRequest{Metric: "m"})
			if err != nil {
				t.Fatalf("GetRetention: %v", err)
			}
			if (got.Policy == nil) != tc.wantNil {
				t.Fatalf("GetRetention policy: got %+v, want nil=%v", got.Policy, tc.wantNil)
			}
		})
	}
}

func TestClient_GetRetention_NilAndBadResponse(t *testing.T) {
	var resp interface{}
	c := NewClient(&mockTransport{
		requestFn: func(context.Context, string, string, interface{}) (interface{}, error) { return resp, nil },
	})
	got, err := c.GetRetention(context.Background(), &GetRetentionRequest{Metric: "cpu"})
	if err != nil || got.Metric != "cpu" || got.Policy != nil {
		t.Fatalf("nil response: got %#v, %v; want metric cpu and no policy", got, err)
	}

	resp = "not a response"
	if _, err := c.GetRetention(context.Background(), &GetRetentionRequest{Metric: "cpu"}); err == nil {
		t.Fatal("a response of the wrong type must be an error")
	}

	for _, req := range []*GetRetentionRequest{nil, {}} {
		if _, err := c.GetRetention(context.Background(), req); !errors.IsInvalidInput(err) {
			t.Errorf("GetRetention(%#v): want InvalidInput, got %v", req, err)
		}
	}
}

func TestParseAggregateFunction_RoundTripsWireName(t *testing.T) {
	for _, f := range []AggregateFunction{AggregateSum, AggregateAvg, AggregateMin, AggregateMax, AggregateCount} {
		if got := ParseAggregateFunction(f.WireName()); got != f {
			t.Errorf("ParseAggregateFunction(%q) = %v, want %v", f.WireName(), got, f)
		}
		if got := ParseAggregateFunction(f.String()); got != f {
			t.Errorf("ParseAggregateFunction(%q) = %v, want %v", f.String(), got, f)
		}
	}
	if AggregateNone.WireName() != "" || ParseAggregateFunction("") != AggregateNone || ParseAggregateFunction("bogus") != AggregateNone {
		t.Error("AggregateNone must map to and from the empty wire name; unknown names to AggregateNone")
	}
}
