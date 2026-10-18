package embedded

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/timeseries"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestWrite_SDKPointReachesTheService(t *testing.T) {
	var got *service.WritePointRequest
	tr, err := New(&mockQuicBaaS{writePointFunc: func(_ context.Context, req *service.WritePointRequest) (*service.WritePointResponse, error) {
		got = req
		return &service.WritePointResponse{Success: true, Timestamp: req.Timestamp}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	err = timeseries.NewClient(tr).Write(context.Background(), &timeseries.WriteRequest{
		Metric:    "cpu.usage",
		Tags:      map[string]string{"host": "web-1"},
		Timestamp: at,
		Value:     42.5,
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got == nil || got.Metric != "cpu.usage" || got.Tags["host"] != "web-1" || !got.Timestamp.Equal(at) || got.Value != 42.5 {
		t.Fatalf("service received %+v", got)
	}
}

func TestWrite_InvalidPointIsRefusedByTheService(t *testing.T) {
	invalid := sdkerrors.NewInvalidInput("timeseries.WritePoint", "value must be finite")
	tr, err := New(&mockQuicBaaS{writePointFunc: func(_ context.Context, req *service.WritePointRequest) (*service.WritePointResponse, error) {
		if math.IsNaN(req.Value) {
			return nil, invalid
		}
		return &service.WritePointResponse{Success: true}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}

	err = timeseries.NewClient(tr).Write(context.Background(), &timeseries.WriteRequest{
		Metric:    "cpu.usage",
		Timestamp: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC),
		Value:     math.NaN(),
	})
	if !errors.Is(err, invalid) {
		t.Fatalf("Write error = %v, want %v", err, invalid)
	}
}

func TestWrite_WrongOrNilRequestIsRefused(t *testing.T) {
	tr, err := New(&mockQuicBaaS{})
	if err != nil {
		t.Fatal(err)
	}

	for name, req := range map[string]interface{}{
		"string":    "cpu.usage",
		"nil point": (*timeseries.WriteRequest)(nil),
	} {
		_, err := tr.Request(context.Background(), "quicbaas.TimeSeries", "Write", req)
		var invalidReqErr *InvalidRequestTypeError
		if !errors.As(err, &invalidReqErr) {
			t.Errorf("%s: error = %v, want *InvalidRequestTypeError", name, err)
		}
	}
}

func TestWrite_ServiceRequestIsServedLikeWritePoint(t *testing.T) {
	tr, err := New(&mockQuicBaaS{})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := tr.Request(context.Background(), "quicbaas.TimeSeries", "Write", &service.WritePointRequest{Metric: "cpu.usage", Timestamp: time.Now()})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if r, ok := resp.(*service.WritePointResponse); !ok || !r.Success {
		t.Fatalf("response = %#v", resp)
	}
}
