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

package embedded

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// mockQuicBaaSForIAM is a minimal service.QuicBaaS exposing a real
// IdentityStore through the same structural seam the production embedded
// transport uses (identityStoreProvider).
type mockQuicBaaSForIAM struct {
	service.QuicBaaS
	store *iam.IdentityStore
}

func (m *mockQuicBaaSForIAM) GetIdentityStore(_ context.Context) (*iam.IdentityStore, error) {
	return m.store, nil
}

// embeddedCreateUserEnvelope mirrors the SDK client's internal transport
// wrapper {TenantID, Request}; the handler extracts both via reflection.
type embeddedCreateUserEnvelope struct {
	TenantID string
	Request  *iamsdk.CreateUserRequest
}

func newIAMTestService() (*mockQuicBaaSForIAM, *iam.IdentityStore) {
	mem := iam.NewMemoryStorage()
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: mem,
		RoleStorage: mem,
	})
	return &mockQuicBaaSForIAM{store: store}, store
}

// TestHandleIAMUserCreate_ForwardsRolesAndMetadata proves the embedded
// transport does not drop Metadata from the SDK CreateUserRequest: roles are
// persisted and metadata lands as user attributes.
func TestHandleIAMUserCreate_ForwardsRolesAndMetadata(t *testing.T) {
	svc, store := newIAMTestService()
	ctx := context.Background()

	role, err := store.CreateRole(ctx, "t-1", iam.CreateRoleOptions{Name: "auditor"})
	if err != nil {
		t.Fatalf("CreateRole failed: %v", err)
	}

	out, err := handleIAMUserCreate(ctx, svc, &embeddedCreateUserEnvelope{
		TenantID: "t-1",
		Request: &iamsdk.CreateUserRequest{
			Username:    "embeddeduser",
			Email:       "embedded@example.com",
			DisplayName: "Embedded User",
			Roles:       []string{role.ID},
			Metadata:    map[string]string{"department": "engineering"},
		},
	})
	if err != nil {
		t.Fatalf("handleIAMUserCreate failed: %v", err)
	}
	if out == nil {
		t.Fatal("expected a user response")
	}

	stored, err := store.GetUserByEmail(ctx, "t-1", "embedded@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail failed: %v", err)
	}
	if len(stored.Roles) != 1 || stored.Roles[0] != role.ID {
		t.Errorf("Expected persisted roles [%s], got %v", role.ID, stored.Roles)
	}
	if stored.Attributes == nil || stored.Attributes["department"] != "engineering" {
		t.Errorf("Expected metadata forwarded as attributes, got %v", stored.Attributes)
	}
}

// TestMetadataToAttributes verifies the string-map boxing helper: values are
// copied one-to-one and nil/empty inputs yield nil.
func TestMetadataToAttributes(t *testing.T) {
	if metadataToAttributes(nil) != nil {
		t.Error("nil metadata should yield nil attributes")
	}
	if metadataToAttributes(map[string]string{}) != nil {
		t.Error("empty metadata should yield nil attributes")
	}

	attrs := metadataToAttributes(map[string]string{"a": "1", "b": "2"})
	if len(attrs) != 2 || attrs["a"] != "1" || attrs["b"] != "2" {
		t.Errorf("unexpected attributes: %v", attrs)
	}
}
