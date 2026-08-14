// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package quic

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/timeseries"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

func TestIsTimeseriesService(t *testing.T) {
	tests := []struct {
		svc      string
		expected bool
	}{
		{"quicbaas.TimeSeries", true},
		{"TimeSeries", true},
		{"quicbaas.TimeSeries.Sub", true},
		{"quicbaas.KV", false},
		{"", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, isTimeseriesService(tt.svc), tt.svc)
	}
}

// TestTimeseriesOpcodesMatchServer is the regression guard for the opcode
// misalignment: the SDK's TS opcodes must match the server's authoritative
// values (pkg/api/quic/protocol.go), where Aggregate/TimeSeriesQuery occupy
// 0xA0/0xA1 and the modern TS verbs occupy 0xA2-0xA8.
func TestTimeseriesOpcodesMatchServer(t *testing.T) {
	assert.Equal(t, byte(0xA0), OpAggregate)
	assert.Equal(t, byte(0xA1), OpTimeSeriesQuery)
	assert.Equal(t, byte(0xA2), OpTSWritePoint)
	assert.Equal(t, byte(0xA3), OpTSWriteBatch)
	assert.Equal(t, byte(0xA4), OpTSQuery)
	assert.Equal(t, byte(0xA5), OpTSDeleteSeries)
	assert.Equal(t, byte(0xA6), OpTSGetRetentionPolicy)
	assert.Equal(t, byte(0xA7), OpTSSetRetentionPolicy)
	assert.Equal(t, byte(0xA8), OpTSListMetrics)
}

// TestTimeseriesMethodToOpcode proves the client's short method names ("Write",
// "Query") resolve to the correct server opcodes (WritePoint=0xA2, TSQuery=0xA4),
// reconciling the client/server method-name mismatch.
func TestTimeseriesMethodToOpcode(t *testing.T) {
	cases := []struct {
		method string
		want   byte
	}{
		{"Write", OpTSWritePoint},
		{"WritePoint", OpTSWritePoint},
		{"WriteBatch", OpTSWriteBatch},
		{"Query", OpTSQuery},
		{"Aggregate", OpAggregate},
		{"DeleteSeries", OpTSDeleteSeries},
		{"GetRetentionPolicy", OpTSGetRetentionPolicy},
		{"SetRetentionPolicy", OpTSSetRetentionPolicy},
		{"ListMetrics", OpTSListMetrics},
	}
	for _, c := range cases {
		op, ok := MethodToOpcode("quicbaas.TimeSeries", c.method)
		require.True(t, ok, "method %s should resolve", c.method)
		assert.Equal(t, c.want, op, c.method)
	}
}

func TestTransformTimeseriesRequest_Write(t *testing.T) {
	ts := time.Unix(1700000000, 0).UTC()
	out, err := transformTimeseriesRequest("Write", &timeseries.WriteRequest{
		Metric:    "cpu.load",
		Tags:      map[string]string{"host": "a"},
		Value:     3.14,
		Timestamp: ts,
	})
	require.NoError(t, err)
	req, ok := out.(*service.WritePointRequest)
	require.True(t, ok, "got %T", out)
	assert.Equal(t, "cpu.load", req.Metric)
	assert.Equal(t, 3.14, req.Value)
	assert.Equal(t, ts, req.Timestamp)
	assert.Equal(t, "a", req.Tags["host"])
}

func TestTransformTimeseriesRequest_Query(t *testing.T) {
	start := time.Unix(1700000000, 0).UTC()
	end := time.Unix(1700003600, 0).UTC()
	out, err := transformTimeseriesRequest("Query", &timeseries.QueryRequest{
		Metric:    "cpu.load",
		Tags:      map[string]string{"host": "a"},
		Start:     start,
		End:       end,
		Aggregate: timeseries.AggregateAvg,
		Interval:  5 * time.Minute,
	})
	require.NoError(t, err)
	req, ok := out.(*service.TSQueryRequest)
	require.True(t, ok, "got %T", out)
	assert.Equal(t, "cpu.load", req.Metric)
	assert.Equal(t, start, req.StartTime)
	assert.Equal(t, end, req.EndTime)
	assert.Equal(t, "AVG", req.Aggregation)
	assert.Equal(t, int64(300), req.IntervalSec)
}

// TestTransformTimeseriesRequest_QueryNoAggregate verifies AggregateNone leaves
// the server Aggregation empty so raw points are returned.
func TestTransformTimeseriesRequest_QueryNoAggregate(t *testing.T) {
	out, err := transformTimeseriesRequest("Query", &timeseries.QueryRequest{
		Metric:    "cpu.load",
		Start:     time.Unix(1, 0),
		End:       time.Unix(2, 0),
		Aggregate: timeseries.AggregateNone,
	})
	require.NoError(t, err)
	req := out.(*service.TSQueryRequest)
	assert.Equal(t, "", req.Aggregation)
	assert.Equal(t, int64(0), req.IntervalSec)
}

func TestTransformTimeseriesRequest_Errors(t *testing.T) {
	// Unsupported method.
	_, err := transformTimeseriesRequest("Bogus", &timeseries.WriteRequest{})
	require.Error(t, err)

	// Wrong request type for Write.
	_, err = transformTimeseriesRequest("Write", "not-a-request")
	require.Error(t, err)

	// Wrong request type for Query.
	_, err = transformTimeseriesRequest("Query", 42)
	require.Error(t, err)
}

// TestDecodeTimeseriesResponse_Query proves the server's TSQueryResponse (emitted
// via EncodePayload) round-trips into the SDK *timeseries.QueryResponse the
// client's Query method asserts on.
func TestDecodeTimeseriesResponse_Query(t *testing.T) {
	t1 := time.Unix(1700000000, 0).UTC()
	t2 := time.Unix(1700000060, 0).UTC()
	want := &service.TSQueryResponse{
		Metric: "cpu.load",
		Points: []*service.TSDataPoint{
			{Metric: "cpu.load", Value: 1.5, Timestamp: t1},
			{Metric: "cpu.load", Value: 2.5, Timestamp: t2},
		},
	}
	payload, err := EncodePayload(want)
	require.NoError(t, err)

	got, err := decodeTimeseriesResponse("Query", payload)
	require.NoError(t, err)
	resp, ok := got.(*timeseries.QueryResponse)
	require.True(t, ok, "got %T", got)
	require.Len(t, resp.Series, 2)
	assert.Equal(t, 1.5, resp.Series[0].Value)
	assert.Equal(t, t1, resp.Series[0].Timestamp)
	assert.Equal(t, 2.5, resp.Series[1].Value)
}

func TestDecodeTimeseriesResponse_QueryEmpty(t *testing.T) {
	got, err := decodeTimeseriesResponse("Query", nil)
	require.NoError(t, err)
	resp, ok := got.(*timeseries.QueryResponse)
	require.True(t, ok, "got %T", got)
	assert.Empty(t, resp.Series)
}

func TestDecodeTimeseriesResponse_Write(t *testing.T) {
	payload, err := EncodePayload(&service.WritePointResponse{Success: true, Timestamp: time.Unix(1, 0)})
	require.NoError(t, err)
	got, err := decodeTimeseriesResponse("Write", payload)
	require.NoError(t, err)
	assert.Nil(t, got)

	// Empty payload is also success.
	got, err = decodeTimeseriesResponse("Write", nil)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestDecodeTimeseriesResponse_Errors(t *testing.T) {
	// Unsupported method.
	_, err := decodeTimeseriesResponse("Bogus", []byte(`{}`))
	require.Error(t, err)

	// Invalid JSON for Query.
	_, err = decodeTimeseriesResponse("Query", []byte(`{not json`))
	require.Error(t, err)

	// Invalid JSON for Write.
	_, err = decodeTimeseriesResponse("Write", []byte(`{not json`))
	require.Error(t, err)
}
