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

package timeseries

import (
	"context"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.TimeSeries"
)

// AggregateFunction represents the aggregation function for time-series queries.
type AggregateFunction int

const (
	// AggregateNone returns raw data points without aggregation.
	AggregateNone AggregateFunction = iota
	// AggregateSum returns the sum of values in each interval.
	AggregateSum
	// AggregateAvg returns the average of values in each interval.
	AggregateAvg
	// AggregateMin returns the minimum value in each interval.
	AggregateMin
	// AggregateMax returns the maximum value in each interval.
	AggregateMax
	// AggregateCount returns the count of values in each interval.
	AggregateCount
)

// String returns the string representation of the aggregate function.
func (f AggregateFunction) String() string {
	switch f {
	case AggregateNone:
		return "NONE"
	case AggregateSum:
		return "SUM"
	case AggregateAvg:
		return "AVG"
	case AggregateMin:
		return "MIN"
	case AggregateMax:
		return "MAX"
	case AggregateCount:
		return "COUNT"
	default:
		return "UNKNOWN"
	}
}

// Valid returns true if the aggregate function is valid.
func (f AggregateFunction) Valid() bool {
	return f >= AggregateNone && f <= AggregateCount
}

// WriteRequest is a request to write a time-series data point.
type WriteRequest struct {
	// Metric is the metric name (required).
	Metric string

	// Tags are optional key-value pairs for filtering and grouping.
	Tags map[string]string

	// Timestamp is the data point timestamp (required).
	Timestamp time.Time

	// Value is the numeric value of the data point.
	Value float64
}

// QueryRequest is a request to query time-series data.
type QueryRequest struct {
	// Metric is the metric name to query (required).
	Metric string

	// Tags are optional filters for the query.
	Tags map[string]string

	// Start is the start of the time range (required).
	Start time.Time

	// End is the end of the time range (required).
	End time.Time

	// Aggregate is the aggregation function to apply.
	Aggregate AggregateFunction

	// Interval is the aggregation bucket size.
	// Required when Aggregate is not AggregateNone.
	Interval time.Duration
}

// QueryResponse is the response from a time-series query.
type QueryResponse struct {
	// Series contains the resulting data points.
	Series []Point
}

// Point represents a single time-series data point.
type Point struct {
	// Timestamp is the data point timestamp.
	Timestamp time.Time

	// Value is the numeric value.
	Value float64
}

// Client provides time-series data operations.
type Client interface {
	// Write writes a time-series data point.
	Write(ctx context.Context, req *WriteRequest) error

	// Query queries time-series data with optional aggregation.
	Query(ctx context.Context, req *QueryRequest) (*QueryResponse, error)

	// SetRetention sets the retention policy of a metric: how long its
	// points are kept, and how they are downsampled.
	SetRetention(ctx context.Context, req *SetRetentionRequest) error

	// GetRetention returns the retention policy of a metric. Policy is
	// nil when the metric has none; a zero policy (infinite duration, no
	// downsampling) is the same thing and also reads nil.
	GetRetention(ctx context.Context, req *GetRetentionRequest) (*GetRetentionResponse, error)
}

// RetentionPolicy is how long a metric's points are kept, and how they are
// downsampled.
type RetentionPolicy struct {
	// Duration is how long points are kept, in whole seconds. Zero keeps
	// points forever (infinite retention).
	Duration time.Duration

	// Resolution is the bucket size points are downsampled to. Zero keeps
	// raw points.
	Resolution time.Duration

	// Aggregation is the downsampling function. AggregateNone keeps raw
	// points.
	Aggregation AggregateFunction
}

// SetRetentionRequest sets the retention policy of a metric.
type SetRetentionRequest struct {
	// Metric is the metric name (required).
	Metric string

	// Policy is the retention policy to set.
	Policy RetentionPolicy
}

// GetRetentionRequest asks for the retention policy of a metric.
type GetRetentionRequest struct {
	// Metric is the metric name (required).
	Metric string
}

// GetRetentionResponse carries a metric's retention policy.
type GetRetentionResponse struct {
	// Metric is the metric name.
	Metric string

	// Policy is the metric's retention policy. Policy is nil when the metric
	// has none; a zero policy (infinite duration, no downsampling) is the
	// same thing and also reads nil.
	Policy *RetentionPolicy
}

// ParseAggregateFunction maps a wire aggregation name ("sum", "AVG", ...)
// to its AggregateFunction. An empty or unknown name is AggregateNone.
func ParseAggregateFunction(name string) AggregateFunction {
	switch strings.ToUpper(name) {
	case "SUM":
		return AggregateSum
	case "AVG":
		return AggregateAvg
	case "MIN":
		return AggregateMin
	case "MAX":
		return AggregateMax
	case "COUNT":
		return AggregateCount
	default:
		return AggregateNone
	}
}

// WireName is the aggregation name the server's retention API takes:
// lower case, empty for AggregateNone.
func (f AggregateFunction) WireName() string {
	if f == AggregateNone || !f.Valid() {
		return ""
	}
	return strings.ToLower(f.String())
}

// client implements the Client interface.
type client struct {
	transport transport.Transport
}

// NewClient creates a new TimeSeries client.
// Returns nil if transport is nil.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// Write writes a time-series data point.
func (c *client) Write(ctx context.Context, req *WriteRequest) error {
	if err := validateWriteRequest(req); err != nil {
		return err
	}

	_, err := c.transport.Request(ctx, serviceName, "Write", req)
	if err != nil {
		return err
	}

	return nil
}

// Query queries time-series data with optional aggregation.
func (c *client) Query(ctx context.Context, req *QueryRequest) (*QueryResponse, error) {
	if err := validateQueryRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "Query", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &QueryResponse{Series: []Point{}}, nil
	}

	queryResp, ok := resp.(*QueryResponse)
	if !ok {
		return nil, errors.NewInternal("timeseries.Query", "invalid response type")
	}

	return queryResp, nil
}

// validateWriteRequest validates a WriteRequest.
func validateWriteRequest(req *WriteRequest) error {
	if req == nil {
		return errors.NewInvalidInput("timeseries.Write", "request is required")
	}
	if req.Metric == "" {
		return errors.NewInvalidInput("timeseries.Write", "metric name is required")
	}
	if req.Timestamp.IsZero() {
		return errors.NewInvalidInput("timeseries.Write", "timestamp is required")
	}
	return nil
}

// validateQueryRequest validates a QueryRequest.
func validateQueryRequest(req *QueryRequest) error {
	if req == nil {
		return errors.NewInvalidInput("timeseries.Query", "request is required")
	}
	if req.Metric == "" {
		return errors.NewInvalidInput("timeseries.Query", "metric name is required")
	}
	if req.Start.IsZero() {
		return errors.NewInvalidInput("timeseries.Query", "start time is required")
	}
	if req.End.IsZero() {
		return errors.NewInvalidInput("timeseries.Query", "end time is required")
	}
	if req.End.Before(req.Start) {
		return errors.NewInvalidInput("timeseries.Query", "end time must be after start time")
	}
	if !req.Aggregate.Valid() {
		return errors.NewInvalidInput("timeseries.Query", "invalid aggregate function")
	}
	if req.Aggregate != AggregateNone && req.Interval <= 0 {
		return errors.NewInvalidInput("timeseries.Query", "interval is required when using aggregation")
	}
	return nil
}

// SetRetention sets the retention policy of a metric.
func (c *client) SetRetention(ctx context.Context, req *SetRetentionRequest) error {
	if err := validateSetRetentionRequest(req); err != nil {
		return err
	}
	_, err := c.transport.Request(ctx, serviceName, "SetRetention", req)
	return err
}

// GetRetention returns the retention policy of a metric.
func (c *client) GetRetention(ctx context.Context, req *GetRetentionRequest) (*GetRetentionResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("timeseries.GetRetention", "request is required")
	}
	if req.Metric == "" {
		return nil, errors.NewInvalidInput("timeseries.GetRetention", "metric name is required")
	}
	resp, err := c.transport.Request(ctx, serviceName, "GetRetention", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return &GetRetentionResponse{Metric: req.Metric}, nil
	}
	out, ok := resp.(*GetRetentionResponse)
	if !ok {
		return nil, errors.NewInternal("timeseries.GetRetention", "invalid response type")
	}
	if out.Policy != nil && out.Policy.isZero() {
		out.Policy = nil
	}
	return out, nil
}

// isZero reports whether p is the zero policy (infinite duration, no
// downsampling), which means the same as no policy.
func (p *RetentionPolicy) isZero() bool {
	return p.Duration == 0 && p.Resolution == 0 && p.Aggregation == AggregateNone
}

func validateSetRetentionRequest(req *SetRetentionRequest) error {
	const op = "timeseries.SetRetention"
	if req == nil {
		return errors.NewInvalidInput(op, "request is required")
	}
	if req.Metric == "" {
		return errors.NewInvalidInput(op, "metric name is required")
	}
	p := req.Policy
	if p.Duration < 0 || p.Duration%time.Second != 0 {
		return errors.NewInvalidInput(op, "duration must be zero (infinite) or a positive whole number of seconds")
	}
	if p.Resolution < 0 || p.Resolution%time.Second != 0 {
		return errors.NewInvalidInput(op, "resolution must be a whole number of seconds")
	}
	if !p.Aggregation.Valid() {
		return errors.NewInvalidInput(op, "invalid aggregate function")
	}
	if p.Aggregation != AggregateNone && p.Resolution == 0 {
		return errors.NewInvalidInput(op, "resolution is required when downsampling with an aggregate")
	}
	return nil
}
