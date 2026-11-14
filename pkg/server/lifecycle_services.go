package server

import (
	"context"
	"errors"
	"sync"

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
	return func(context.Context) (*serviceplane.Plane, error) {
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
		built, err := serviceplane.NewPlane(serviceplane.Config{
			Store: store, Verifier: rt.Verifier, Launcher: rt.Launcher, Grants: rt.Grants,
		})
		if err != nil {
			return nil, err
		}
		plane = built
		return plane, nil
	}
}
