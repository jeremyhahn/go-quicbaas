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

package iam

import (
	"context"
	"errors"
	"testing"
)

// failingWriteAppStorage stores reads normally but refuses every write.
type failingWriteAppStorage struct {
	*mockAppStorage
}

func (s *failingWriteAppStorage) CreateApp(context.Context, *Application) error {
	return &IAMMessageError{Message: "store unavailable"}
}

func (s *failingWriteAppStorage) UpdateApp(context.Context, *Application) error {
	return &IAMMessageError{Message: "store unavailable"}
}

// metadataTestOptions is a valid client_credentials registration.
func metadataTestOptions(method string) RegisterAppOptions {
	return RegisterAppOptions{
		Name:                    "Metadata Test",
		GrantTypes:              []string{GrantTypeClientCredentials},
		TokenEndpointAuthMethod: method,
	}
}

// TestRegisterApp_MetadataRefusalIsAppMetadataError proves a register refused
// for its metadata is an AppMetadataError that still matches its own sentinel.
func TestRegisterApp_MetadataRefusalIsAppMetadataError(t *testing.T) {
	m := NewAppManager(newMockAppStorage(), nil)

	_, err := m.RegisterApp(context.Background(), "t1", metadataTestOptions("invalid_method"))

	var metadataErr *AppMetadataError
	if !errors.As(err, &metadataErr) {
		t.Fatalf("RegisterApp() error = %v, want *AppMetadataError", err)
	}
	if !errors.Is(err, ErrInvalidTokenEndpointAuthMethod) {
		t.Errorf("RegisterApp() error = %v, want it to wrap ErrInvalidTokenEndpointAuthMethod", err)
	}
	if err.Error() != ErrInvalidTokenEndpointAuthMethod.Error() {
		t.Errorf("Error() = %q, want the refusal's own message", err.Error())
	}
}

// TestRegisterApp_StorageFailureIsNotAppMetadataError proves a register whose
// store fails is not reported as a metadata refusal.
func TestRegisterApp_StorageFailureIsNotAppMetadataError(t *testing.T) {
	m := NewAppManager(&failingWriteAppStorage{newMockAppStorage()}, nil)

	_, err := m.RegisterApp(context.Background(), "t1", metadataTestOptions(AuthMethodClientSecretPost))

	if err == nil {
		t.Fatal("RegisterApp() error = nil, want the store failure")
	}
	var metadataErr *AppMetadataError
	if errors.As(err, &metadataErr) {
		t.Errorf("RegisterApp() error = %v, want no *AppMetadataError", err)
	}
}

// TestUpdateAppInTenant_AuthMethodRefusalIsAppMetadataError proves an update
// refused for its method is an AppMetadataError, while an update whose store
// fails is not.
func TestUpdateAppInTenant_AuthMethodRefusalIsAppMetadataError(t *testing.T) {
	ctx := context.Background()
	storage := newMockAppStorage()
	app, err := NewAppManager(storage, nil).RegisterApp(ctx, "t1", metadataTestOptions(""))
	if err != nil {
		t.Fatalf("RegisterApp() error = %v", err)
	}

	invalid := "invalid_method"
	_, err = NewAppManager(storage, nil).UpdateAppInTenant(ctx, "t1", app.ClientID,
		UpdateAppOptions{TokenEndpointAuthMethod: &invalid})
	var metadataErr *AppMetadataError
	if !errors.As(err, &metadataErr) {
		t.Errorf("UpdateAppInTenant(invalid method) error = %v, want *AppMetadataError", err)
	}

	valid := AuthMethodClientSecretPost
	_, err = NewAppManager(&failingWriteAppStorage{storage}, nil).UpdateAppInTenant(ctx, "t1", app.ClientID,
		UpdateAppOptions{TokenEndpointAuthMethod: &valid})
	if err == nil {
		t.Fatal("UpdateAppInTenant(store fails) error = nil, want the store failure")
	}
	if errors.As(err, &metadataErr) {
		t.Errorf("UpdateAppInTenant(store fails) error = %v, want no *AppMetadataError", err)
	}
}

// TestResponseTypeRefusalIsAppMetadataError proves a response type outside
// response_types_supported is a metadata refusal on register and on update,
// so every transport answers it as the client's error, not a server error
// (#1512).
func TestResponseTypeRefusalIsAppMetadataError(t *testing.T) {
	ctx := context.Background()
	storage := newMockAppStorage()
	m := NewAppManager(storage, nil)

	opts := metadataTestOptions("")
	opts.ResponseTypes = []string{"id_token"}
	_, err := m.RegisterApp(ctx, "t1", opts)
	var metadataErr *AppMetadataError
	if !errors.As(err, &metadataErr) || !errors.Is(err, ErrInvalidResponseType) {
		t.Errorf("RegisterApp(id_token) error = %v, want *AppMetadataError wrapping ErrInvalidResponseType", err)
	}

	app, err := m.RegisterApp(ctx, "t1", metadataTestOptions(""))
	if err != nil {
		t.Fatalf("RegisterApp() error = %v", err)
	}
	unsupported := []string{"code", "id_token"}
	_, err = m.UpdateAppInTenant(ctx, "t1", app.ClientID, UpdateAppOptions{ResponseTypes: &unsupported})
	if !errors.As(err, &metadataErr) || !errors.Is(err, ErrInvalidResponseType) {
		t.Errorf("UpdateAppInTenant(id_token) error = %v, want *AppMetadataError wrapping ErrInvalidResponseType", err)
	}
}
