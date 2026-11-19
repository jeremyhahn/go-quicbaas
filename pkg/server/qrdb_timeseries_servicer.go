package server

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-qrdb/pkg/consistency"
	"github.com/jeremyhahn/go-qrdb/pkg/timeseries"
)

// qrdbTimeSeriesSurface names the time-series surface in
// QRDBUserServiceUnavailableError.
const qrdbTimeSeriesSurface = "timeseries"

// GetQRDBTimeSeriesServicer returns the embedded qrdb node's time-series
// servicer, or nil while the node is absent or its services are not built
// (before Unseal on the seal-enabled boot path).
func (s *Server) GetQRDBTimeSeriesServicer() timeseries.TimeSeriesServicer {
	node := s.getQRDBNode()
	if node == nil {
		return nil
	}
	svcs := node.Services()
	if svcs == nil {
		return nil
	}
	if svcs.TimeSeries == nil {
		return nil
	}
	return svcs.TimeSeries
}

// QRDBTimeSeriesServicer returns a time-series servicer that resolves the
// node's servicer on every call, for the embedded transport composer. A
// servicer captured before Unseal would be nil for the life of the process.
func (s *Server) QRDBTimeSeriesServicer() timeseries.TimeSeriesServicer {
	return newQRDBTimeSeriesServicerProxy(s)
}

// qrdbTimeSeriesServicerProxy resolves the time-series servicer per call and
// fails closed with QRDBUserServiceUnavailableError{Surface: "timeseries"}
// while the node is absent or sealed.
type qrdbTimeSeriesServicerProxy struct {
	resolve func() timeseries.TimeSeriesServicer
}

var _ timeseries.TimeSeriesServicer = (*qrdbTimeSeriesServicerProxy)(nil)

func newQRDBTimeSeriesServicerProxy(s *Server) *qrdbTimeSeriesServicerProxy {
	return &qrdbTimeSeriesServicerProxy{resolve: s.GetQRDBTimeSeriesServicer}
}

func (p *qrdbTimeSeriesServicerProxy) servicer() (timeseries.TimeSeriesServicer, error) {
	if p.resolve != nil {
		if svc := p.resolve(); svc != nil {
			return svc, nil
		}
	}
	return nil, &QRDBUserServiceUnavailableError{Surface: qrdbTimeSeriesSurface}
}

// Write implements timeseries.TimeSeriesServicer.
func (p *qrdbTimeSeriesServicerProxy) Write(ctx context.Context, metric string, value float64, timestamp time.Time, tags map[string]string) error {
	svc, err := p.servicer()
	if err != nil {
		return err
	}
	return svc.Write(ctx, metric, value, timestamp, tags)
}

// WriteBatch implements timeseries.TimeSeriesServicer.
func (p *qrdbTimeSeriesServicerProxy) WriteBatch(ctx context.Context, points []timeseries.DataPoint) error {
	svc, err := p.servicer()
	if err != nil {
		return err
	}
	return svc.WriteBatch(ctx, points)
}

// Query implements timeseries.TimeSeriesServicer.
func (p *qrdbTimeSeriesServicerProxy) Query(ctx context.Context, metric string, start, end time.Time, opts ...timeseries.TSQueryOption) ([]timeseries.DataPoint, error) {
	svc, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return svc.Query(ctx, metric, start, end, opts...)
}

// QueryWithConsistency implements timeseries.TimeSeriesServicer.
func (p *qrdbTimeSeriesServicerProxy) QueryWithConsistency(ctx context.Context, metric string, start, end time.Time, ropts consistency.ReadOptions, opts ...timeseries.TSQueryOption) ([]timeseries.DataPoint, error) {
	svc, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return svc.QueryWithConsistency(ctx, metric, start, end, ropts, opts...)
}

// Aggregate implements timeseries.TimeSeriesServicer.
func (p *qrdbTimeSeriesServicerProxy) Aggregate(ctx context.Context, metric string, start, end time.Time, fn timeseries.AggFunc, interval time.Duration) ([]timeseries.AggResult, error) {
	svc, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return svc.Aggregate(ctx, metric, start, end, fn, interval)
}

// AggregateWithConsistency implements timeseries.TimeSeriesServicer.
func (p *qrdbTimeSeriesServicerProxy) AggregateWithConsistency(ctx context.Context, metric string, start, end time.Time, fn timeseries.AggFunc, interval time.Duration, ropts consistency.ReadOptions) ([]timeseries.AggResult, error) {
	svc, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return svc.AggregateWithConsistency(ctx, metric, start, end, fn, interval, ropts)
}

// WritePoint implements timeseries.TimeSeriesServicer.
func (p *qrdbTimeSeriesServicerProxy) WritePoint(ctx context.Context, point *timeseries.DataPoint) error {
	svc, err := p.servicer()
	if err != nil {
		return err
	}
	return svc.WritePoint(ctx, point)
}

// QueryRich implements timeseries.TimeSeriesServicer.
func (p *qrdbTimeSeriesServicerProxy) QueryRich(ctx context.Context, req *timeseries.QueryRequest) (*timeseries.QueryResult, error) {
	svc, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return svc.QueryRich(ctx, req)
}

// QueryRichWithConsistency implements timeseries.TimeSeriesServicer.
func (p *qrdbTimeSeriesServicerProxy) QueryRichWithConsistency(ctx context.Context, req *timeseries.QueryRequest, ropts consistency.ReadOptions) (*timeseries.QueryResult, error) {
	svc, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return svc.QueryRichWithConsistency(ctx, req, ropts)
}

// DeleteSeries implements timeseries.TimeSeriesServicer.
func (p *qrdbTimeSeriesServicerProxy) DeleteSeries(ctx context.Context, req *timeseries.DeleteRequest) error {
	svc, err := p.servicer()
	if err != nil {
		return err
	}
	return svc.DeleteSeries(ctx, req)
}

// GetRetentionPolicy implements timeseries.TimeSeriesServicer.
func (p *qrdbTimeSeriesServicerProxy) GetRetentionPolicy(metric string) (*timeseries.RetentionPolicy, error) {
	svc, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return svc.GetRetentionPolicy(metric)
}

// SetRetentionPolicy implements timeseries.TimeSeriesServicer.
func (p *qrdbTimeSeriesServicerProxy) SetRetentionPolicy(ctx context.Context, metric string, policy *timeseries.RetentionPolicy) error {
	svc, err := p.servicer()
	if err != nil {
		return err
	}
	return svc.SetRetentionPolicy(ctx, metric, policy)
}

// ListMetrics implements timeseries.TimeSeriesServicer.
func (p *qrdbTimeSeriesServicerProxy) ListMetrics(prefix string, limit int) ([]string, error) {
	svc, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return svc.ListMetrics(prefix, limit)
}

// ListMetricsWithConsistency implements timeseries.TimeSeriesServicer.
func (p *qrdbTimeSeriesServicerProxy) ListMetricsWithConsistency(ctx context.Context, prefix string, limit int, ropts consistency.ReadOptions) ([]string, error) {
	svc, err := p.servicer()
	if err != nil {
		return nil, err
	}
	return svc.ListMetricsWithConsistency(ctx, prefix, limit, ropts)
}
