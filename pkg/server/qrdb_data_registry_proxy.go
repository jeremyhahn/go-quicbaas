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

package server

import (
	"sync/atomic"

	qrdbserver "github.com/jeremyhahn/go-qrdb/pkg/server"
	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
)

// qrdbDataRegistryProxy is the qrdb service registry the qrdb.v1.QRDBService
// registration resolves its data services through. It reads the embedded
// go-qrdb node on every call, so it follows the node across init, seal and
// unseal, and fails closed while the node, or its server, is absent.
type qrdbDataRegistryProxy struct {
	s      *Server
	cached atomic.Pointer[qrdbDataRegistryForServer]
}

// qrdbDataRegistryForServer pairs a go-qrdb server with the service registry
// built from it. go-qrdb's ServiceRegistry builds a fresh registry per call,
// so the proxy caches one per server and rebuilds it when the server changes.
type qrdbDataRegistryForServer struct {
	srv *qrdbserver.Server
	reg qrdbservice.Registry
}

// newQRDBDataRegistryProxy returns a registry proxy over s's embedded node.
func newQRDBDataRegistryProxy(s *Server) *qrdbDataRegistryProxy {
	return &qrdbDataRegistryProxy{s: s}
}

// current returns the registry of the node's running server. It fails closed
// with DataPlaneRecyclingError while the node is absent or sealed, and with
// CapabilityUnsupportedError when the server has no database registry.
func (p *qrdbDataRegistryProxy) current() (qrdbservice.Registry, error) {
	node := p.s.getQRDBNode()
	if node == nil {
		return nil, &qrdbservice.DataPlaneRecyclingError{Service: "database registry"}
	}
	srv := node.Server()
	if srv == nil {
		return nil, &qrdbservice.DataPlaneRecyclingError{Service: "database registry"}
	}
	if c := p.cached.Load(); c != nil && c.srv == srv {
		return c.reg, nil
	}
	reg := srv.ServiceRegistry()
	if reg == nil {
		return nil, &qrdbservice.CapabilityUnsupportedError{Message: "the embedded qrdb node has no database registry"}
	}
	p.cached.Store(&qrdbDataRegistryForServer{srv: srv, reg: reg})
	return reg, nil
}

// KVService resolves the node's KV service for (tenant, db).
func (p *qrdbDataRegistryProxy) KVService(tenant, db string) (qrdbservice.KVServicer, error) {
	reg, err := p.current()
	if err != nil {
		return nil, err
	}
	return reg.KVService(tenant, db)
}

// MVCCService resolves the node's MVCC KV service for (tenant, db).
func (p *qrdbDataRegistryProxy) MVCCService(tenant, db string) (qrdbservice.MVCCKVServicer, error) {
	reg, err := p.current()
	if err != nil {
		return nil, err
	}
	return reg.MVCCService(tenant, db)
}

// WatchService resolves the node's watch service for (tenant, db).
func (p *qrdbDataRegistryProxy) WatchService(tenant, db string) (qrdbservice.WatchServicer, error) {
	reg, err := p.current()
	if err != nil {
		return nil, err
	}
	return reg.WatchService(tenant, db)
}

// LeaseService resolves the node's lease service for (tenant, db).
func (p *qrdbDataRegistryProxy) LeaseService(tenant, db string) (qrdbservice.LeaseServicer, error) {
	reg, err := p.current()
	if err != nil {
		return nil, err
	}
	return reg.LeaseService(tenant, db)
}

// IndexService resolves the node's index service for (tenant, db).
func (p *qrdbDataRegistryProxy) IndexService(tenant, db string) (qrdbservice.IndexServicer, error) {
	reg, err := p.current()
	if err != nil {
		return nil, err
	}
	return reg.IndexService(tenant, db)
}

// TimeSeriesService resolves the node's time-series service for (tenant, db).
func (p *qrdbDataRegistryProxy) TimeSeriesService(tenant, db string) (qrdbservice.TimeSeriesServicer, error) {
	reg, err := p.current()
	if err != nil {
		return nil, err
	}
	return reg.TimeSeriesService(tenant, db)
}

// VectorService resolves the node's vector service for (tenant, db).
func (p *qrdbDataRegistryProxy) VectorService(tenant, db string) (qrdbservice.VectorServicer, error) {
	reg, err := p.current()
	if err != nil {
		return nil, err
	}
	return reg.VectorService(tenant, db)
}

// TransactionService resolves the node's transaction service for (tenant, db).
func (p *qrdbDataRegistryProxy) TransactionService(tenant, db string) (qrdbservice.TransactionServicer, error) {
	reg, err := p.current()
	if err != nil {
		return nil, err
	}
	return reg.TransactionService(tenant, db)
}

// BatchService resolves the node's batch service for (tenant, db).
func (p *qrdbDataRegistryProxy) BatchService(tenant, db string) (qrdbservice.BatchServicer, error) {
	reg, err := p.current()
	if err != nil {
		return nil, err
	}
	return reg.BatchService(tenant, db)
}

// TableService resolves the node's table service for (tenant, db).
func (p *qrdbDataRegistryProxy) TableService(tenant, db string) (qrdbservice.TableServicer, error) {
	reg, err := p.current()
	if err != nil {
		return nil, err
	}
	return reg.TableService(tenant, db)
}

// SequenceService resolves the node's sequence service for (tenant, db).
func (p *qrdbDataRegistryProxy) SequenceService(tenant, db string) (qrdbservice.SequenceServicer, error) {
	reg, err := p.current()
	if err != nil {
		return nil, err
	}
	return reg.SequenceService(tenant, db)
}

// PrivilegeService resolves the node's privilege service for (tenant, db).
func (p *qrdbDataRegistryProxy) PrivilegeService(tenant, db string) (qrdbservice.PrivilegeServicer, error) {
	reg, err := p.current()
	if err != nil {
		return nil, err
	}
	return reg.PrivilegeService(tenant, db)
}

// CollectionService resolves the node's collection service for (tenant, db).
func (p *qrdbDataRegistryProxy) CollectionService(tenant, db string) (qrdbservice.CollectionServicer, error) {
	reg, err := p.current()
	if err != nil {
		return nil, err
	}
	return reg.CollectionService(tenant, db)
}

// LockService resolves the node's lock service for (tenant, db).
func (p *qrdbDataRegistryProxy) LockService(tenant, db string) (qrdbservice.LockServicer, error) {
	reg, err := p.current()
	if err != nil {
		return nil, err
	}
	return reg.LockService(tenant, db)
}

var _ qrdbservice.Registry = (*qrdbDataRegistryProxy)(nil)
