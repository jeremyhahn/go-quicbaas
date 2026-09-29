// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package service

import (
	"context"
	"errors"
	"testing"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"

	quicbaaserrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Fixtures shared by the reference-check tests
// =============================================================================

// errReferenceStoreSealed stands in for a store that cannot answer.
var errReferenceStoreSealed = errors.New("barrier sealed")

// fakeSSHKeys holds keys by "scope/owner/name" and answers the way the xkms
// remote clients do: a missing key is *xkmsssh.NotFoundError.
type fakeSSHKeys struct {
	keys map[string]bool
	err  error
}

func newFakeSSHKeys(addresses ...string) *fakeSSHKeys {
	keys := make(map[string]bool, len(addresses))
	for _, address := range addresses {
		keys[address] = true
	}
	return &fakeSSHKeys{keys: keys}
}

func (f *fakeSSHKeys) GetKey(_ context.Context, req xkmsssh.GetKeyRequest) (*xkmsssh.Key, error) {
	if f.err != nil {
		return nil, f.err
	}
	if !f.keys[req.Scope+"/"+req.Owner+"/"+req.Name] {
		return nil, &xkmsssh.NotFoundError{Resource: "key", Message: req.Name}
	}
	return &xkmsssh.Key{Name: req.Name}, nil
}

// referenceSecrets answers GetSecret from a map, the way the real secrets
// service does: an absent secret is a NotFound QBError.
type referenceSecrets struct {
	SecretsService
	data map[string][]byte
	err  error
}

func (s *referenceSecrets) GetSecret(_ context.Context, req *GetSecretRequest) (*GetSecretResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	data, ok := s.data[req.SecretID]
	if !ok {
		return nil, &quicbaaserrors.QBError{Code: quicbaaserrors.ErrNotFound, Op: "secrets.GetSecret"}
	}
	return &GetSecretResponse{Data: data}, nil
}

// failingFleet is a fleet registry that cannot answer.
type failingFleet struct{}

func (failingFleet) ListFleetNodes(context.Context, string, *orchestration.FleetNodeFilter) ([]*orchestration.FleetNode, error) {
	return nil, errReferenceStoreSealed
}

// newTestFleet is a fleet registry holding one node in tenant-1.
func newTestFleet(t *testing.T) orchestration.FleetRegistry {
	t.Helper()
	fleet := orchestration.NewInMemoryFleetRegistry()
	if err := fleet.RegisterFleetNode(context.Background(), &orchestration.FleetNode{
		ID: "agent-1", TenantID: "tenant-1", Name: "worker", Host: "host-a", NodeType: "worker",
	}); err != nil {
		t.Fatalf("register fleet node: %v", err)
	}
	return fleet
}

// requireReferenceNotFound asserts err is a ReferenceNotFoundError on field.
func requireReferenceNotFound(t *testing.T, err error, field string) {
	t.Helper()
	var notFound *quicbaaserrors.ReferenceNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *ReferenceNotFoundError, got %T: %v", err, err)
	}
	if notFound.Field != field {
		t.Fatalf("field = %q, want %q (%v)", notFound.Field, field, err)
	}
}

// requireReferenceUnavailable asserts err is a ReferenceUnavailableError on
// field, and never a not-found.
func requireReferenceUnavailable(t *testing.T, err error, field string) {
	t.Helper()
	var unavailable *quicbaaserrors.ReferenceUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("expected *ReferenceUnavailableError, got %T: %v", err, err)
	}
	if unavailable.Field != field {
		t.Fatalf("field = %q, want %q (%v)", unavailable.Field, field, err)
	}
	var notFound *quicbaaserrors.ReferenceNotFoundError
	if errors.As(err, &notFound) {
		t.Fatal("an unavailable store must not read as not found")
	}
}

// =============================================================================
// requireSecretRef
// =============================================================================

func TestRequireSecretRef(t *testing.T) {
	ctx := context.Background()
	secrets := &referenceSecrets{data: map[string][]byte{"tok": []byte("value"), "blank": nil}}

	t.Run("exists", func(t *testing.T) {
		data, err := requireSecretRef(ctx, secrets, "tenant-1", "f", "tok")
		if err != nil || string(data) != "value" {
			t.Fatalf("requireSecretRef = %q, %v", data, err)
		}
	})
	t.Run("empty value is returned for the caller to judge", func(t *testing.T) {
		data, err := requireSecretRef(ctx, secrets, "tenant-1", "f", "blank")
		if err != nil || len(data) != 0 {
			t.Fatalf("requireSecretRef = %q, %v", data, err)
		}
	})
	t.Run("missing", func(t *testing.T) {
		_, err := requireSecretRef(ctx, secrets, "tenant-1", "f", "ghost")
		requireReferenceNotFound(t, err, "f")
	})
	t.Run("store fails", func(t *testing.T) {
		_, err := requireSecretRef(ctx, &referenceSecrets{err: errReferenceStoreSealed}, "tenant-1", "f", "tok")
		requireReferenceUnavailable(t, err, "f")
		if !errors.Is(err, errReferenceStoreSealed) {
			t.Fatal("the store failure must stay reachable")
		}
	})
	t.Run("no store attached", func(t *testing.T) {
		_, err := requireSecretRef(ctx, nil, "tenant-1", "f", "tok")
		requireReferenceUnavailable(t, err, "f")
	})
}

// =============================================================================
// requireSSHKeyRef
// =============================================================================

func TestRequireSSHKeyRef(t *testing.T) {
	ctx := context.Background()
	keys := newFakeSSHKeys("user/tenant-1/signing", "service/deploy/access")
	tests := []struct {
		name string
		keys SSHKeyGetter
		ref  orchestration.KMSKeyRef
		want string
	}{
		{"defaults resolve to user scope and the tenant", keys, orchestration.KMSKeyRef{Name: "signing"}, "ok"},
		{"explicit scope and owner", keys, orchestration.KMSKeyRef{Name: "access", Scope: "service", Owner: "deploy"}, "ok"},
		{"missing", keys, orchestration.KMSKeyRef{Name: "ghost"}, "not_found"},
		{"right name, wrong owner", keys, orchestration.KMSKeyRef{Name: "access", Scope: "service", Owner: "other"}, "not_found"},
		{"scope no key can live at", &fakeSSHKeys{err: &xkmsssh.InvalidScopeError{Value: "bogus"}}, orchestration.KMSKeyRef{Name: "k", Scope: "bogus"}, "not_found"},
		{"store fails", &fakeSSHKeys{err: errReferenceStoreSealed}, orchestration.KMSKeyRef{Name: "signing"}, "unavailable"},
		{"key store not configured", &fakeSSHKeys{err: &SSHNotConfiguredError{}}, orchestration.KMSKeyRef{Name: "signing"}, "unavailable"},
		{"no store attached", nil, orchestration.KMSKeyRef{Name: "signing"}, "unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := requireSSHKeyRef(ctx, tt.keys, "tenant-1", "git_ssh_key", tt.ref)
			switch tt.want {
			case "ok":
				if err != nil {
					t.Fatalf("want no error, got %v", err)
				}
			case "not_found":
				requireReferenceNotFound(t, err, "git_ssh_key")
			case "unavailable":
				requireReferenceUnavailable(t, err, "git_ssh_key")
			}
		})
	}
}

func TestRequireSSHKeyRef_NamesTheResolvedAddress(t *testing.T) {
	err := requireSSHKeyRef(context.Background(), newFakeSSHKeys(), "tenant-1", "git_signing_key", orchestration.KMSKeyRef{Name: "k"})
	want := `git_signing_key: ssh key "user/tenant-1/k" does not exist`
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}
