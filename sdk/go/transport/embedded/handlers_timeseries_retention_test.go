package embedded

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/timeseries"
)

// retentionMock records the retention calls the embedded transport makes on
// the service and answers get with a fixed response.
type retentionMock struct {
	*mockQuicBaaS
	gotSet  *service.TSSetRetentionPolicyRequest
	gotGet  *service.TSGetRetentionPolicyRequest
	getResp *service.TSGetRetentionPolicyResponse
	err     error
}

func (m *retentionMock) SetRetentionPolicy(_ context.Context, req *service.TSSetRetentionPolicyRequest) (*service.TSSetRetentionPolicyResponse, error) {
	m.gotSet = req
	if m.err != nil {
		return nil, m.err
	}
	return &service.TSSetRetentionPolicyResponse{Success: true}, nil
}

func (m *retentionMock) GetRetentionPolicy(_ context.Context, req *service.TSGetRetentionPolicyRequest) (*service.TSGetRetentionPolicyResponse, error) {
	m.gotGet = req
	if m.err != nil {
		return nil, m.err
	}
	return m.getResp, nil
}

func newRetentionClient(t *testing.T, m *retentionMock) timeseries.Client {
	t.Helper()
	m.mockQuicBaaS = &mockQuicBaaS{}
	tr, err := New(m)
	if err != nil {
		t.Fatal(err)
	}
	return timeseries.NewClient(tr)
}

func TestSetRetention_SDKPolicyReachesTheService(t *testing.T) {
	m := &retentionMock{}
	err := newRetentionClient(t, m).SetRetention(context.Background(), &timeseries.SetRetentionRequest{
		Metric: "cpu.usage",
		Policy: timeseries.RetentionPolicy{Duration: time.Hour, Resolution: time.Minute, Aggregation: timeseries.AggregateAvg},
	})
	if err != nil {
		t.Fatalf("SetRetention: %v", err)
	}
	got := m.gotSet
	if got == nil || got.Metric != "cpu.usage" || got.Policy == nil {
		t.Fatalf("service received %+v", got)
	}
	if got.Policy.Duration != time.Hour || got.Policy.Resolution != time.Minute || got.Policy.Aggregation != "avg" {
		t.Errorf("service received policy %+v", got.Policy)
	}
}

func TestGetRetention_ServicePolicyIsTyped(t *testing.T) {
	m := &retentionMock{getResp: &service.TSGetRetentionPolicyResponse{
		Metric: "cpu.usage",
		Policy: &service.TSRetentionPolicy{Duration: 24 * time.Hour, Resolution: 5 * time.Minute, Aggregation: "max"},
	}}
	resp, err := newRetentionClient(t, m).GetRetention(context.Background(), &timeseries.GetRetentionRequest{Metric: "cpu.usage"})
	if err != nil {
		t.Fatalf("GetRetention: %v", err)
	}
	if m.gotGet == nil || m.gotGet.Metric != "cpu.usage" {
		t.Errorf("service received %+v", m.gotGet)
	}
	want := timeseries.RetentionPolicy{Duration: 24 * time.Hour, Resolution: 5 * time.Minute, Aggregation: timeseries.AggregateMax}
	if resp.Metric != "cpu.usage" || resp.Policy == nil || *resp.Policy != want {
		t.Fatalf("GetRetention: got %+v (policy %+v), want %+v", resp, resp.Policy, want)
	}
}

func TestGetRetention_NoPolicyIsNil(t *testing.T) {
	for name, getResp := range map[string]*service.TSGetRetentionPolicyResponse{
		"nil response": nil,
		"no policy":    {Metric: "cpu.usage"},
	} {
		resp, err := newRetentionClient(t, &retentionMock{getResp: getResp}).GetRetention(context.Background(), &timeseries.GetRetentionRequest{Metric: "cpu.usage"})
		if err != nil {
			t.Fatalf("%s: GetRetention: %v", name, err)
		}
		if resp.Metric != "cpu.usage" || resp.Policy != nil {
			t.Errorf("%s: got %+v, want metric cpu.usage and a nil policy", name, resp)
		}
	}
}

// TestGetRetention_ZeroPolicyIsNil pins that a zero policy from the service
// (the backend's representation of "no policy") reads nil.
func TestGetRetention_ZeroPolicyIsNil(t *testing.T) {
	for name, policy := range map[string]*service.TSRetentionPolicy{
		"empty":            {},
		"aggregation none": {Aggregation: "none"},
	} {
		resp, err := newRetentionClient(t, &retentionMock{getResp: &service.TSGetRetentionPolicyResponse{
			Metric: "cpu.usage", Policy: policy,
		}}).GetRetention(context.Background(), &timeseries.GetRetentionRequest{Metric: "cpu.usage"})
		if err != nil {
			t.Fatalf("%s: GetRetention: %v", name, err)
		}
		if resp.Policy != nil {
			t.Errorf("%s: got policy %+v, want nil", name, resp.Policy)
		}
	}
}

func TestRetention_ServiceErrorIsReturned(t *testing.T) {
	boom := errors.New("boom")
	c := newRetentionClient(t, &retentionMock{err: boom})
	ctx := context.Background()
	if err := c.SetRetention(ctx, &timeseries.SetRetentionRequest{
		Metric: "cpu.usage", Policy: timeseries.RetentionPolicy{Duration: time.Hour},
	}); err == nil {
		t.Error("SetRetention: want the service error")
	}
	if _, err := c.GetRetention(ctx, &timeseries.GetRetentionRequest{Metric: "cpu.usage"}); err == nil {
		t.Error("GetRetention: want the service error")
	}
}

func TestRetention_WrongRequestTypeIsRefused(t *testing.T) {
	tr, err := New(&mockQuicBaaS{})
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"SetRetention", "GetRetention"} {
		_, err := tr.Request(context.Background(), "quicbaas.TimeSeries", method, "cpu.usage")
		var invalidReqErr *InvalidRequestTypeError
		if !errors.As(err, &invalidReqErr) {
			t.Errorf("%s: error = %v, want *InvalidRequestTypeError", method, err)
		}
	}
}
