package server

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-qrdb/pkg/consistency"
	qrdbserver "github.com/jeremyhahn/go-qrdb/pkg/server"
	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
	"github.com/jeremyhahn/go-qrdb/pkg/tenant"
	"github.com/jeremyhahn/go-qrdb/pkg/timeseries"
)

// qrdbTimeSeriesSurface names the time-series surface in
// QRDBUserServiceUnavailableError.
const qrdbTimeSeriesSurface = "timeseries"

// qrdbScopedRegistry is one go-qrdb server's per-(tenant, database) service
// registry, remembered with the server it was built from.
type qrdbScopedRegistry struct {
	srv *qrdbserver.Server
	reg qrdbservice.Registry
}

// qrdbScopedRegistryHolder holds the embedded node's scoped service registry.
// go-qrdb's (*server.Server).ServiceRegistry builds a fresh registry on every
// call and asks callers that resolve repeatedly to hold one, so the holder
// caches it per go-qrdb server and rebuilds it when the node's server changes
// (a restart or an Unseal). It is go-qrdb's own nodeServiceRegistry pattern
// (sdk/go node/embedded_registry.go), which is unexported there.
//
// The registry is the only time-series source: a shared-shard tenant is
// served through a namespaced, quota-checked servicer, exactly as go-qrdb's
// own transports do. The node's raw Services().TimeSeries, which serves the
// shared shard unscoped, is never used.
type qrdbScopedRegistryHolder struct {
	s   *Server
	cur atomic.Pointer[qrdbScopedRegistry]
}

// current returns the cached registry of the node's current go-qrdb server.
// It fails closed with QRDBUserServiceUnavailableError{Surface: "timeseries"}
// while the node is absent, sealed (no server yet) or has no database
// registry.
func (h *qrdbScopedRegistryHolder) current() (qrdbservice.Registry, error) {
	unavailable := &QRDBUserServiceUnavailableError{Surface: qrdbTimeSeriesSurface}
	if h == nil || h.s == nil {
		return nil, unavailable
	}
	node := h.s.getQRDBNode()
	if node == nil {
		return nil, unavailable
	}
	srv := node.Server()
	if srv == nil {
		return nil, unavailable
	}
	if cached := h.cur.Load(); cached != nil && cached.srv == srv {
		return cached.reg, nil
	}
	reg := srv.ServiceRegistry()
	if reg == nil {
		return nil, unavailable
	}
	h.cur.Store(&qrdbScopedRegistry{srv: srv, reg: reg})
	return reg, nil
}

// timeSeriesService resolves the time-series servicer of one (tenant,
// database) scope through the registry.
func (h *qrdbScopedRegistryHolder) timeSeriesService(tenantID, db string) (timeseries.TimeSeriesServicer, error) {
	reg, err := h.current()
	if err != nil {
		return nil, err
	}
	svc, err := reg.TimeSeriesService(tenantID, db)
	if err != nil {
		return nil, err
	}
	if svc == nil {
		return nil, &QRDBUserServiceUnavailableError{Surface: qrdbTimeSeriesSurface}
	}
	return svc, nil
}

// qrdbTimeSeriesUnscopedError is returned by the time-series methods that
// carry no context (GetRetentionPolicy, ListMetrics): with no context there
// is no tenant or database to scope the call to, so it is refused rather
// than served from the shared shard. go-qrdb's embedded scoped adapter
// refuses the same two methods.
type qrdbTimeSeriesUnscopedError struct {
	// Method names the refused method.
	Method string
}

func (e *qrdbTimeSeriesUnscopedError) Error() string {
	return "timeseries: " + e.Method + " carries no context, so no tenant and database scope can be resolved; use its context-carrying form"
}

// QRDBTimeSeriesServicer returns a time-series servicer for the embedded
// transport composer. It resolves the caller's (tenant, database) scope from
// each call's context through the node's scoped service registry, so a
// servicer captured before Unseal comes online once the node is unsealed.
func (s *Server) QRDBTimeSeriesServicer() timeseries.TimeSeriesServicer {
	return newQRDBTimeSeriesServicerProxy(s)
}

// qrdbTimeSeriesServicerProxy resolves the scoped time-series servicer per
// call from the call's context (go-qrdb tenant.FromContext and
// tenant.DatabaseFromContext). It fails closed with
// QRDBUserServiceUnavailableError{Surface: "timeseries"} while the node is
// absent or sealed, and with qrdbTimeSeriesUnscopedError for the methods that
// carry no context.
type qrdbTimeSeriesServicerProxy struct {
	registry *qrdbScopedRegistryHolder
}

var _ timeseries.TimeSeriesServicer = (*qrdbTimeSeriesServicerProxy)(nil)

func newQRDBTimeSeriesServicerProxy(s *Server) *qrdbTimeSeriesServicerProxy {
	return &qrdbTimeSeriesServicerProxy{registry: &qrdbScopedRegistryHolder{s: s}}
}

func (p *qrdbTimeSeriesServicerProxy) servicer(ctx context.Context) (timeseries.TimeSeriesServicer, error) {
	return p.registry.timeSeriesService(tenant.FromContext(ctx), tenant.DatabaseFromContext(ctx))
}

// Write implements timeseries.TimeSeriesServicer.
func (p *qrdbTimeSeriesServicerProxy) Write(ctx context.Context, metric string, value float64, timestamp time.Time, tags map[string]string) error {
	svc, err := p.servicer(ctx)
	if err != nil {
		return err
	}
	return svc.Write(ctx, metric, value, timestamp, tags)
}

// WriteBatch implements timeseries.TimeSeriesServicer.
func (p *qrdbTimeSeriesServicerProxy) WriteBatch(ctx context.Context, points []timeseries.DataPoint) error {
	svc, err := p.servicer(ctx)
	if err != nil {
		return err
	}
	return svc.WriteBatch(ctx, points)
}

// Query implements timeseries.TimeSeriesServicer.
func (p *qrdbTimeSeriesServicerProxy) Query(ctx context.Context, metric string, start, end time.Time, opts ...timeseries.TSQueryOption) ([]timeseries.DataPoint, error) {
	svc, err := p.servicer(ctx)
	if err != nil {
		return nil, err
	}
	return svc.Query(ctx, metric, start, end, opts...)
}

// QueryWithConsistency implements timeseries.TimeSeriesServicer.
func (p *qrdbTimeSeriesServicerProxy) QueryWithConsistency(ctx context.Context, metric string, start, end time.Time, ropts consistency.ReadOptions, opts ...timeseries.TSQueryOption) ([]timeseries.DataPoint, error) {
	svc, err := p.servicer(ctx)
	if err != nil {
		return nil, err
	}
	return svc.QueryWithConsistency(ctx, metric, start, end, ropts, opts...)
}

// Aggregate implements timeseries.TimeSeriesServicer.
func (p *qrdbTimeSeriesServicerProxy) Aggregate(ctx context.Context, metric string, start, end time.Time, fn timeseries.AggFunc, interval time.Duration) ([]timeseries.AggResult, error) {
	svc, err := p.servicer(ctx)
	if err != nil {
		return nil, err
	}
	return svc.Aggregate(ctx, metric, start, end, fn, interval)
}

// AggregateWithConsistency implements timeseries.TimeSeriesServicer.
func (p *qrdbTimeSeriesServicerProxy) AggregateWithConsistency(ctx context.Context, metric string, start, end time.Time, fn timeseries.AggFunc, interval time.Duration, ropts consistency.ReadOptions) ([]timeseries.AggResult, error) {
	svc, err := p.servicer(ctx)
	if err != nil {
		return nil, err
	}
	return svc.AggregateWithConsistency(ctx, metric, start, end, fn, interval, ropts)
}

// WritePoint implements timeseries.TimeSeriesServicer.
func (p *qrdbTimeSeriesServicerProxy) WritePoint(ctx context.Context, point *timeseries.DataPoint) error {
	svc, err := p.servicer(ctx)
	if err != nil {
		return err
	}
	return svc.WritePoint(ctx, point)
}

// QueryRich implements timeseries.TimeSeriesServicer.
func (p *qrdbTimeSeriesServicerProxy) QueryRich(ctx context.Context, req *timeseries.QueryRequest) (*timeseries.QueryResult, error) {
	svc, err := p.servicer(ctx)
	if err != nil {
		return nil, err
	}
	return svc.QueryRich(ctx, req)
}

// QueryRichWithConsistency implements timeseries.TimeSeriesServicer.
func (p *qrdbTimeSeriesServicerProxy) QueryRichWithConsistency(ctx context.Context, req *timeseries.QueryRequest, ropts consistency.ReadOptions) (*timeseries.QueryResult, error) {
	svc, err := p.servicer(ctx)
	if err != nil {
		return nil, err
	}
	return svc.QueryRichWithConsistency(ctx, req, ropts)
}

// DeleteSeries implements timeseries.TimeSeriesServicer.
func (p *qrdbTimeSeriesServicerProxy) DeleteSeries(ctx context.Context, req *timeseries.DeleteRequest) error {
	svc, err := p.servicer(ctx)
	if err != nil {
		return err
	}
	return svc.DeleteSeries(ctx, req)
}

// GetRetentionPolicy implements timeseries.TimeSeriesServicer. It carries no
// context, so it always fails closed with qrdbTimeSeriesUnscopedError; the
// qrdb.v1 TSGetRetention RPC reads the policy through the scoped registry.
func (p *qrdbTimeSeriesServicerProxy) GetRetentionPolicy(string) (*timeseries.RetentionPolicy, error) {
	return nil, &qrdbTimeSeriesUnscopedError{Method: "GetRetentionPolicy"}
}

// SetRetentionPolicy implements timeseries.TimeSeriesServicer.
func (p *qrdbTimeSeriesServicerProxy) SetRetentionPolicy(ctx context.Context, metric string, policy *timeseries.RetentionPolicy) error {
	svc, err := p.servicer(ctx)
	if err != nil {
		return err
	}
	return svc.SetRetentionPolicy(ctx, metric, policy)
}

// ListMetrics implements timeseries.TimeSeriesServicer. It carries no
// context, so it always fails closed with qrdbTimeSeriesUnscopedError; use
// ListMetricsWithConsistency.
func (p *qrdbTimeSeriesServicerProxy) ListMetrics(string, int) ([]string, error) {
	return nil, &qrdbTimeSeriesUnscopedError{Method: "ListMetrics"}
}

// ListMetricsWithConsistency implements timeseries.TimeSeriesServicer.
func (p *qrdbTimeSeriesServicerProxy) ListMetricsWithConsistency(ctx context.Context, prefix string, limit int, ropts consistency.ReadOptions) ([]string, error) {
	svc, err := p.servicer(ctx)
	if err != nil {
		return nil, err
	}
	return svc.ListMetricsWithConsistency(ctx, prefix, limit, ropts)
}
