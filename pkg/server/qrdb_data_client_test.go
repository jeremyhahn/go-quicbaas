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
	"testing"

	"github.com/stretchr/testify/require"

	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
)

// recordingQRDBRegistry records the (tenant, db) of each resolution it
// serves. Methods it does not override panic through the nil interface.
type recordingQRDBRegistry struct {
	qrdbservice.Registry
	seen []string
}

func (r *recordingQRDBRegistry) record(name, tenant, db string) {
	r.seen = append(r.seen, name+":"+tenant+"/"+db)
}

func (r *recordingQRDBRegistry) KVService(tenant, db string) (qrdbservice.KVServicer, error) {
	r.record("kv", tenant, db)
	return nil, nil
}

func (r *recordingQRDBRegistry) MVCCService(tenant, db string) (qrdbservice.MVCCKVServicer, error) {
	r.record("mvcc", tenant, db)
	return nil, nil
}

func (r *recordingQRDBRegistry) TransactionService(tenant, db string) (qrdbservice.TransactionServicer, error) {
	r.record("tx", tenant, db)
	return nil, nil
}

func (r *recordingQRDBRegistry) BatchService(tenant, db string) (qrdbservice.BatchServicer, error) {
	r.record("batch", tenant, db)
	return nil, nil
}

func (r *recordingQRDBRegistry) IndexService(tenant, db string) (qrdbservice.IndexServicer, error) {
	r.record("index", tenant, db)
	return nil, nil
}

// TestQRDBDefaultDatabaseRegistry_ResolvesTheDefaultDatabase pins that the
// embedded data client's alias never reaches the data registry: every
// family resolves the caller's tenant in the default database.
func TestQRDBDefaultDatabaseRegistry_ResolvesTheDefaultDatabase(t *testing.T) {
	inner := &recordingQRDBRegistry{}
	r := &qrdbDefaultDatabaseRegistry{inner: inner}

	_, err := r.KVService("acme", qrdbDefaultDatabaseAlias)
	require.NoError(t, err)
	_, err = r.MVCCService("acme", qrdbDefaultDatabaseAlias)
	require.NoError(t, err)
	_, err = r.TransactionService("acme", qrdbDefaultDatabaseAlias)
	require.NoError(t, err)
	_, err = r.BatchService("acme", qrdbDefaultDatabaseAlias)
	require.NoError(t, err)
	_, err = r.IndexService("acme", qrdbDefaultDatabaseAlias)
	require.NoError(t, err)

	require.Equal(t, []string{
		"kv:acme/system",
		"mvcc:acme/system",
		"tx:acme/system",
		"batch:acme/system",
		"index:acme/system",
	}, inner.seen)
}

// TestServer_QRDBDataClient_FailsClosedWithoutNode pins that there is no
// data client while the node is absent.
func TestServer_QRDBDataClient_FailsClosedWithoutNode(t *testing.T) {
	srv := withTempServer(t)
	cl, err := srv.QRDBDataClient()
	require.Error(t, err)
	require.Nil(t, cl)
}
