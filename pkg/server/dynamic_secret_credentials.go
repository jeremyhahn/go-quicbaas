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
	"context"
	"errors"
	"log/slog"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/credentials"
	"github.com/jeremyhahn/go-quicbaas/pkg/credentials/credstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// errNoSystemCredentialStore is returned for a system call when the node has
// no system credential service.
var errNoSystemCredentialStore = errors.New("dynamic secrets: no system credential service")

// systemCredentialStore is the part of go-qrdb's service-layer credential
// service the credential generator writes through for a call with no tenant.
type systemCredentialStore interface {
	CredentialStore(ctx context.Context, name string, value []byte) error
	CredentialDelete(ctx context.Context, name string) error
}

// tenantCredentialStore writes to one tenant's barrier-backed credential
// service.
type tenantCredentialStore struct {
	svc *qrdbsdk.VersionedCredentialService
}

func (t tenantCredentialStore) Put(ctx context.Context, name string, value []byte) error {
	return t.svc.StoreCredential(ctx, name, value)
}

func (t tenantCredentialStore) Delete(ctx context.Context, name string) error {
	return t.svc.DeleteCredential(ctx, name)
}

// systemStoreAdapter writes to the system credential service.
type systemStoreAdapter struct {
	svc systemCredentialStore
}

func (a systemStoreAdapter) Put(ctx context.Context, name string, value []byte) error {
	return a.svc.CredentialStore(ctx, name, value)
}

func (a systemStoreAdapter) Delete(ctx context.Context, name string) error {
	return a.svc.CredentialDelete(ctx, name)
}

// dynamicCredentialStores routes the credential generator's writes the way
// go-qrdb's tenant-scoped credential retrieve routes its reads, so a
// generated credential resolves where it was written: a call with a tenant
// goes to that tenant's barrier, a call without one to the system credential
// service. A tenant with no registered barrier is an error; it never falls
// back to the system store. The tenant's credential service is built fresh
// on every call, because a rekey can replace a tenant barrier.
func dynamicCredentialStores(
	br *qrdbsdk.BarrierRegistry,
	system systemCredentialStore,
	lookup func(context.Context) (string, bool),
) credstore.StoreFor {
	return func(ctx context.Context) (credstore.Store, error) {
		if lookup != nil && br != nil {
			if tid, ok := lookup(ctx); ok && tid != "" {
				tb, err := br.GetTenant(tid)
				if err != nil {
					return nil, err
				}
				svc, err := qrdbsdk.NewVersionedCredentialService(
					&qrdbsdk.CredentialConfig{Strategy: qrdbsdk.CredentialStrategyBarrier},
					tb.Barrier(),
					slog.Default(),
				)
				if err != nil {
					return nil, err
				}
				return tenantCredentialStore{svc: svc}, nil
			}
		}
		if system == nil {
			return nil, errNoSystemCredentialStore
		}
		return systemStoreAdapter{svc: system}, nil
	}
}

// credentialTenant is the tenant binding credential routing uses: the
// caller's tenant, except that the system tenant is system scope and goes
// to the system credential service, never to a tenant barrier.
func credentialTenant(ctx context.Context) (string, bool) {
	tid, ok := tenantctx.Lookup(ctx)
	if !ok || tid == "" || tenantctx.IsSystem(tid) {
		return "", false
	}
	return tid, true
}

// registerDynamicSecretGenerators registers the production dynamic-secret
// generators. The credential generator needs the node's credential service
// (seal enabled); without one nothing is registered, and CreateRole refuses
// the type as an unknown generator.
func (s *Server) registerDynamicSecretGenerators(gens *credentials.GeneratorRegistry) error {
	node := s.getQRDBNode()
	if node == nil {
		return nil
	}
	svcs := node.Services()
	if svcs == nil || svcs.Credential == nil {
		return nil
	}
	gen, err := credstore.NewGenerator(dynamicCredentialStores(node.BarrierRegistry(), svcs.Credential, credentialTenant))
	if err != nil {
		return err
	}
	return gens.Register(gen)
}
