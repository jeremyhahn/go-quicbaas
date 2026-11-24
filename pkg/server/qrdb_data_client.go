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
	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	qrdbembedded "github.com/jeremyhahn/go-qrdb/sdk/go/transport/embedded"
)

// qrdbDefaultDatabaseAlias is the database name the embedded data client is
// scoped to. go-qrdb's embedded WithDatabase returns the unscoped transport,
// whose calls reach the node's raw servicers, for "" and "system", so the
// default database cannot be scoped by its own name. The client is scoped to
// this alias instead, and qrdbDefaultDatabaseRegistry maps it back to the
// default database. The name is never sent to the node.
const qrdbDefaultDatabaseAlias = "quicbaas-default"

// qrdbDefaultDatabaseRegistry resolves the embedded data client's calls for
// (the context's tenant, the default database) through the same registry the
// qrdb.v1 Unix and gRPC data RPCs use, so both reach one keyspace. Every
// method ignores the database it is given and asks inner for the default
// database, so the alias can never pass through.
type qrdbDefaultDatabaseRegistry struct {
	inner qrdbservice.Registry
}

var _ qrdbservice.Registry = (*qrdbDefaultDatabaseRegistry)(nil)

// KVService resolves the default database's KV servicer for tenant.
func (r *qrdbDefaultDatabaseRegistry) KVService(tenant, _ string) (qrdbservice.KVServicer, error) {
	return r.inner.KVService(tenant, qrdbtransport.DefaultDatabaseScope)
}

// MVCCService resolves the default database's MVCC servicer for tenant.
func (r *qrdbDefaultDatabaseRegistry) MVCCService(tenant, _ string) (qrdbservice.MVCCKVServicer, error) {
	return r.inner.MVCCService(tenant, qrdbtransport.DefaultDatabaseScope)
}

// WatchService resolves the default database's watch servicer for tenant.
func (r *qrdbDefaultDatabaseRegistry) WatchService(tenant, _ string) (qrdbservice.WatchServicer, error) {
	return r.inner.WatchService(tenant, qrdbtransport.DefaultDatabaseScope)
}

// LeaseService resolves the default database's lease servicer for tenant.
func (r *qrdbDefaultDatabaseRegistry) LeaseService(tenant, _ string) (qrdbservice.LeaseServicer, error) {
	return r.inner.LeaseService(tenant, qrdbtransport.DefaultDatabaseScope)
}

// IndexService resolves the default database's index servicer for tenant.
func (r *qrdbDefaultDatabaseRegistry) IndexService(tenant, _ string) (qrdbservice.IndexServicer, error) {
	return r.inner.IndexService(tenant, qrdbtransport.DefaultDatabaseScope)
}

// TimeSeriesService resolves the default database's time-series servicer for
// tenant.
func (r *qrdbDefaultDatabaseRegistry) TimeSeriesService(tenant, _ string) (qrdbservice.TimeSeriesServicer, error) {
	return r.inner.TimeSeriesService(tenant, qrdbtransport.DefaultDatabaseScope)
}

// VectorService resolves the default database's vector servicer for tenant.
func (r *qrdbDefaultDatabaseRegistry) VectorService(tenant, _ string) (qrdbservice.VectorServicer, error) {
	return r.inner.VectorService(tenant, qrdbtransport.DefaultDatabaseScope)
}

// TransactionService resolves the default database's transaction servicer
// for tenant.
func (r *qrdbDefaultDatabaseRegistry) TransactionService(tenant, _ string) (qrdbservice.TransactionServicer, error) {
	return r.inner.TransactionService(tenant, qrdbtransport.DefaultDatabaseScope)
}

// BatchService resolves the default database's batch servicer for tenant.
func (r *qrdbDefaultDatabaseRegistry) BatchService(tenant, _ string) (qrdbservice.BatchServicer, error) {
	return r.inner.BatchService(tenant, qrdbtransport.DefaultDatabaseScope)
}

// TableService resolves the default database's table servicer for tenant.
func (r *qrdbDefaultDatabaseRegistry) TableService(tenant, _ string) (qrdbservice.TableServicer, error) {
	return r.inner.TableService(tenant, qrdbtransport.DefaultDatabaseScope)
}

// SequenceService resolves the default database's sequence servicer for
// tenant.
func (r *qrdbDefaultDatabaseRegistry) SequenceService(tenant, _ string) (qrdbservice.SequenceServicer, error) {
	return r.inner.SequenceService(tenant, qrdbtransport.DefaultDatabaseScope)
}

// PrivilegeService resolves the default database's privilege servicer for
// tenant.
func (r *qrdbDefaultDatabaseRegistry) PrivilegeService(tenant, _ string) (qrdbservice.PrivilegeServicer, error) {
	return r.inner.PrivilegeService(tenant, qrdbtransport.DefaultDatabaseScope)
}

// CollectionService resolves the default database's collection servicer for
// tenant.
func (r *qrdbDefaultDatabaseRegistry) CollectionService(tenant, _ string) (qrdbservice.CollectionServicer, error) {
	return r.inner.CollectionService(tenant, qrdbtransport.DefaultDatabaseScope)
}

// LockService resolves the default database's lock servicer for tenant.
func (r *qrdbDefaultDatabaseRegistry) LockService(tenant, _ string) (qrdbservice.LockServicer, error) {
	return r.inner.LockService(tenant, qrdbtransport.DefaultDatabaseScope)
}

// QRDBDataClient returns a go-qrdb client whose KV, batch, transaction,
// MVCC and index calls resolve, on every call, through the same data
// registry the qrdb.v1 Unix and gRPC RPCs use, for the default database.
// An embedded write and a network read of the same key therefore meet in
// one keyspace. It follows the node across seal and unseal, as the
// registry does. It returns an error while the node is absent.
//
// Collection, watch, lease, lock, vector, time-series and RAG services are
// not served by this client.
func (s *Server) QRDBDataClient() (*qrdbsdk.Client, error) {
	node := s.getQRDBNode()
	if node == nil {
		return nil, &qrdbservice.DataPlaneRecyclingError{Service: "qrdb data client"}
	}
	svcs := node.Services()
	if svcs == nil || svcs.KV == nil || svcs.Admin == nil {
		return nil, &qrdbservice.DataPlaneRecyclingError{Service: "qrdb data client"}
	}
	// The node's servicers only mark which families are served: the scoped
	// clone below replaces each with a per-call registry resolution.
	opts := []qrdbembedded.Option{
		qrdbembedded.WithServiceRegistry(&qrdbDefaultDatabaseRegistry{inner: newQRDBDataRegistryProxy(s)}),
	}
	if svcs.MVCC != nil {
		opts = append(opts, qrdbembedded.WithMVCCService(svcs.MVCC))
	}
	if svcs.Transaction != nil {
		opts = append(opts, qrdbembedded.WithTransactionService(svcs.Transaction))
	}
	if svcs.Batch != nil {
		opts = append(opts, qrdbembedded.WithBatchService(svcs.Batch))
	}
	if svcs.Index != nil {
		opts = append(opts, qrdbembedded.WithIndexService(svcs.Index))
	}
	tr, err := qrdbembedded.New(svcs.KV, svcs.Admin, opts...)
	if err != nil {
		return nil, err
	}
	scoped, err := tr.WithDatabase(qrdbDefaultDatabaseAlias)
	if err != nil {
		return nil, err
	}
	// The embedded transport dials nothing, so there is no Connect.
	return qrdbsdk.NewClient(scoped)
}
