package server

import (
	"context"
	"errors"
	"sync"
	"time"

	ggrpc "google.golang.org/grpc"

	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/cas"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane"
	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"
)

// errServicePlaneStore reports a node whose platform shard is not up yet.
var errServicePlaneStore = errors.New("service plane: the platform shard's qrdb client is not available")

// servicePlaneResolver builds the node's service plane on first use, over
// the platform shard: writes through cas.Writer, reads through the shard's KV.
// A failed build is retried on the next call; a built plane is kept.
func (s *Server) servicePlaneResolver(rt service.ServicePlaneRuntime) service.ServicePlaneResolver {
	var mu sync.Mutex
	var plane *serviceplane.Plane
	return func(ctx context.Context) (*serviceplane.Plane, error) {
		mu.Lock()
		defer mu.Unlock()
		if plane != nil {
			return plane, nil
		}
		if s.qrdbClient == nil {
			return nil, errServicePlaneStore
		}
		shard, err := s.qrdbClient.ForShard(qbshards.PlatformShardID)
		if err != nil {
			return nil, err
		}
		tx, err := s.sdlcTransactor(shard)
		if err != nil {
			return nil, err
		}
		writer, err := cas.NewWriter(tx, shard.KV(), nil)
		if err != nil {
			return nil, err
		}
		store, err := serviceplane.NewStore(writer, serviceplane.QRDBReader(shard.KV()))
		if err != nil {
			return nil, err
		}
		gs, err := s.servicePlaneGrants(ctx, rt, shard)
		if err != nil {
			return nil, err
		}
		built, err := serviceplane.NewPlane(serviceplane.Config{
			Store: store, Verifier: rt.Verifier, Launcher: rt.Launcher, Grants: gs,
		})
		if err != nil {
			return nil, err
		}
		plane = built
		return plane, nil
	}
}

// servicePlaneRecoveryInterval is how often the node settles abandoned
// service claims: a verb whose holder crashed, or whose settlement write
// failed, leaves its service transitioning until this recovers it halted.
const servicePlaneRecoveryInterval = time.Minute

// startServicePlaneRecovery runs Plane.Recover now and every
// servicePlaneRecoveryInterval until shutdown. A plane that cannot be built
// yet (the platform shard is not up) is retried on the next tick.
func (s *Server) startServicePlaneRecovery(resolve service.ServicePlaneResolver) {
	s.stopServicePlaneRecovery()
	ctx, stop := context.WithCancel(context.Background())
	loop := &orchestrationSweeps{stop: stop, done: make(chan struct{})}
	s.servicePlaneRecovery = loop
	go func() {
		defer close(loop.done)
		ticker := time.NewTicker(servicePlaneRecoveryInterval)
		defer ticker.Stop()
		s.runServicePlaneRecovery(ctx, resolve, ticker.C)
	}()
}

// runServicePlaneRecovery recovers once, then once per tick until ctx is done.
func (s *Server) runServicePlaneRecovery(ctx context.Context, resolve service.ServicePlaneResolver, tick <-chan time.Time) {
	for {
		s.recoverServicePlane(ctx, resolve)
		select {
		case <-ctx.Done():
			return
		case <-tick:
		}
	}
}

// recoverServicePlane runs one recovery. A failure is logged; the next tick
// retries it.
func (s *Server) recoverServicePlane(ctx context.Context, resolve service.ServicePlaneResolver) {
	plane, err := resolve(ctx)
	if err != nil {
		if !errors.Is(err, errServicePlaneStore) && !errors.Is(err, errServicePlaneGrants) {
			s.logger.WithError(err).Warn("service plane: recovery could not build the plane")
		}
		return
	}
	recovered, err := plane.Recover(ctx)
	if err != nil {
		s.logger.WithError(err).Warn("service plane: abandoned claims not all recovered")
	}
	for _, svc := range recovered {
		s.logger.Infof("service plane: recovered %s/%s halted: %s", svc.Tenant, svc.Name, svc.Reason)
	}
}

// stopServicePlaneRecovery stops the recovery loop and waits for it.
func (s *Server) stopServicePlaneRecovery() {
	if s.servicePlaneRecovery == nil {
		return
	}
	s.servicePlaneRecovery.stop()
	<-s.servicePlaneRecovery.done
	s.servicePlaneRecovery = nil
}

// registerServicesService registers the service plane's gRPC service. It is
// registered on every node, like the registry: a node with no
// ServicePlaneRuntime answers FailedPrecondition from ServicesService, so the
// gRPC surface stays symmetric with REST, QUIC, MCP and embedded.
func (s *Server) registerServicesService(grpcServer *ggrpc.Server) {
	if s.quicBaaSService == nil {
		s.logger.Info("QuicBaaS service not available, skipping ServicesService gRPC registration")
		return
	}
	grpcapi.RegisterServicesServiceServer(grpcServer, grpcapi.NewServicesServer(s.quicBaaSService.Services()))
	s.logger.Info("Registered ServicesService gRPC service")
}
