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
	"strconv"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// embeddedUpdateUserEnvelope mirrors the SDK client's internal transport wrapper
// {TenantID, UserID, Request}; the handler extracts all three via reflection.
type embeddedUpdateUserEnvelope struct {
	TenantID string
	UserID   string
	Request  *iamsdk.UpdateUserRequest
}

// TestEmbedded_CreateUserKeepsUsernameAndDisplayNameApart proves the in-process
// transport carries the SDK's two distinct fields to the store as two distinct
// fields — and that the returned SDK user reports both.
func TestEmbedded_CreateUserKeepsUsernameAndDisplayNameApart(t *testing.T) {
	svc, store := newIAMTestService()
	ctx := context.Background()

	out, err := handleIAMUserCreate(ctx, svc, &embeddedCreateUserEnvelope{
		TenantID: "t-1",
		Request: &iamsdk.CreateUserRequest{
			Email:       "karl@acme.test",
			Username:    "karl",
			DisplayName: "Karl Marx",
			Lifecycle:   string(iam.LifecycleActive),
		},
	})
	if err != nil {
		t.Fatalf("handleIAMUserCreate: %v", err)
	}
	created, ok := out.(*iamsdk.User)
	if !ok {
		t.Fatalf("handler returned %T, want *iamsdk.User", out)
	}
	if created.Username != "karl" {
		t.Fatalf("Username = %q, want the requested username", created.Username)
	}
	if created.DisplayName != "Karl Marx" {
		t.Fatalf("DisplayName = %q, want the requested display name", created.DisplayName)
	}

	found, err := store.GetUserByUsername(ctx, "t-1", "karl")
	if err != nil {
		t.Fatalf("the account must resolve by the username the request asked for: %v", err)
	}
	if found.Name != "Karl Marx" {
		t.Fatalf("Name = %q, want the display name preserved separately", found.Name)
	}
	if _, err := store.GetUserByUsername(ctx, "t-1", "Karl Marx"); err == nil {
		t.Fatal("the display name resolved as a username")
	}
}

// TestEmbedded_UpdateUserChangesTheUsername pins the update side.
func TestEmbedded_UpdateUserChangesTheUsername(t *testing.T) {
	svc, store := newIAMTestService()
	ctx := context.Background()

	created, err := store.CreateUser(ctx, "t-1", iam.CreateUserOptions{
		Email:     "lena@acme.test",
		Username:  "lena",
		Name:      "Lena One",
		Lifecycle: iam.LifecycleActive,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	out, err := handleIAMUserUpdate(ctx, svc, &embeddedUpdateUserEnvelope{
		TenantID: "t-1",
		UserID:   strconv.FormatUint(created.ID, 10),
		Request: &iamsdk.UpdateUserRequest{
			Username:    "lenab",
			DisplayName: "Lena Two",
		},
	})
	if err != nil {
		t.Fatalf("handleIAMUserUpdate: %v", err)
	}
	updated, ok := out.(*iamsdk.User)
	if !ok {
		t.Fatalf("handler returned %T, want *iamsdk.User", out)
	}
	if updated.Username != "lenab" {
		t.Fatalf("Username = %q, want the updated username", updated.Username)
	}
	if updated.DisplayName != "Lena Two" {
		t.Fatalf("DisplayName = %q, want the updated display name", updated.DisplayName)
	}
	if _, err := store.GetUserByUsername(ctx, "t-1", "lenab"); err != nil {
		t.Fatalf("the renamed account must resolve by its new username: %v", err)
	}
	if _, err := store.GetUserByUsername(ctx, "t-1", "lena"); err == nil {
		t.Fatal("the old username still resolves")
	}
}

// TestEmbedded_UpdateUserRefusesTheDeletingLifecycle proves the in-process
// transport cannot assert the deletion workflow's terminal state either — the
// SDK's own transport must not be the one door left open.
func TestEmbedded_UpdateUserRefusesTheDeletingLifecycle(t *testing.T) {
	svc, store := newIAMTestService()
	ctx := context.Background()

	created, err := store.CreateUser(ctx, "t-1", iam.CreateUserOptions{
		Email:     "milo@acme.test",
		Username:  "milo",
		Name:      "Milo",
		Lifecycle: iam.LifecycleActive,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if _, err := handleIAMUserUpdate(ctx, svc, &embeddedUpdateUserEnvelope{
		TenantID: "t-1",
		UserID:   strconv.FormatUint(created.ID, 10),
		Request:  &iamsdk.UpdateUserRequest{Lifecycle: string(iam.LifecycleDeleting)},
	}); err == nil {
		t.Fatal("an ordinary update was allowed to claim the deleting state")
	}

	after, err := store.GetUser(ctx, "t-1", created.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if after.Lifecycle != iam.LifecycleActive {
		t.Fatalf("Lifecycle = %q, want the account untouched by the refused update", after.Lifecycle)
	}

	// A legitimate transition still reaches the store, so the guard is specific
	// rather than a transport that stopped forwarding the field.
	if _, err := handleIAMUserUpdate(ctx, svc, &embeddedUpdateUserEnvelope{
		TenantID: "t-1",
		UserID:   strconv.FormatUint(created.ID, 10),
		Request:  &iamsdk.UpdateUserRequest{Lifecycle: string(iam.LifecycleSuspended)},
	}); err != nil {
		t.Fatalf("a legitimate transition must still be accepted: %v", err)
	}
	after, err = store.GetUser(ctx, "t-1", created.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if after.Lifecycle != iam.LifecycleSuspended {
		t.Fatalf("Lifecycle = %q, want suspended", after.Lifecycle)
	}
}
