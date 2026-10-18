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

package embedded

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	secrets "github.com/jeremyhahn/go-quicbaas/sdk/go/client/secrets"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// newSecretsClient returns the real SDK secrets client over an embedded
// transport backed by the given service double.
func newSecretsClient(t *testing.T, secretsSvc service.SecretsService) secrets.Client {
	t.Helper()
	mock := &mockQuicBaaS{}
	if secretsSvc != nil {
		mock.secretsService = secretsSvc
	}
	tr, err := New(mock)
	require.NoError(t, err)
	return secrets.NewClient(tr)
}

func TestEmbeddedSecrets_Rotate(t *testing.T) {
	var got *service.RotateSecretRequest
	client := newSecretsClient(t, &mockSecretsService{
		rotateFunc: func(_ context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
			got = req
			return &service.RotateSecretResponse{SecretID: req.SecretID, OldVersion: 1, NewVersion: 2}, nil
		},
	})

	resp, err := client.Rotate(context.Background(), &secrets.RotateSecretRequest{
		SecretID: "app/db",
		NewData:  []byte("s3cret"),
		Metadata: map[string]string{"by": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, &secrets.RotateSecretResponse{SecretID: "app/db", OldVersion: 1, NewVersion: 2}, resp)
	require.NotNil(t, got)
	assert.Equal(t, []byte("s3cret"), got.NewData)
	assert.Equal(t, map[string]string{"by": "test"}, got.Metadata)
}

func TestEmbeddedSecrets_UpdateStatus(t *testing.T) {
	var got *service.UpdateSecretStatusRequest
	client := newSecretsClient(t, &mockSecretsService{
		updateStatusFunc: func(_ context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
			got = req
			return &service.UpdateSecretStatusResponse{SecretID: req.SecretID}, nil
		},
	})

	err := client.UpdateStatus(context.Background(), &secrets.UpdateSecretStatusRequest{
		SecretID: "app/db",
		Status:   secrets.SecretStatusDisabled,
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "app/db", got.SecretID)
	assert.Equal(t, service.SecretStatusDisabled, got.Status)
}

func TestEmbeddedSecrets_UpdateStatus_UnknownStatus(t *testing.T) {
	called := false
	client := newSecretsClient(t, &mockSecretsService{
		updateStatusFunc: func(_ context.Context, _ *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
			called = true
			return &service.UpdateSecretStatusResponse{}, nil
		},
	})

	err := client.UpdateStatus(context.Background(), &secrets.UpdateSecretStatusRequest{
		SecretID: "app/db",
		Status:   secrets.SecretStatus("archived"),
	})
	require.Error(t, err)
	assert.True(t, errors.IsInvalidInput(err), "err = %v, want InvalidInput", err)
	assert.False(t, called, "an unknown status must not reach the service")
}

func TestEmbeddedSecrets_SetRotationPolicy(t *testing.T) {
	next := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	var got *service.SetRotationPolicyRequest
	client := newSecretsClient(t, &mockSecretsService{
		setPolicyFunc: func(_ context.Context, req *service.SetRotationPolicyRequest) (*service.SetRotationPolicyResponse, error) {
			got = req
			return &service.SetRotationPolicyResponse{SecretID: req.SecretID}, nil
		},
	})

	err := client.SetRotationPolicy(context.Background(), &secrets.SetRotationPolicyRequest{
		SecretID: "app/db",
		Policy: &secrets.RotationPolicy{
			Enabled:        true,
			Interval:       24 * time.Hour,
			NextRotation:   next,
			RetainVersions: 3,
			RotationLambda: "rotate-db",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "app/db", got.SecretID)
	assert.Equal(t, &service.SecretRotationPolicy{
		Enabled:        true,
		Interval:       24 * time.Hour,
		NextRotation:   next,
		RetainVersions: 3,
		RotationLambda: "rotate-db",
	}, got.Policy)
}

func TestEmbeddedSecrets_GetRotationPolicy(t *testing.T) {
	next := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	client := newSecretsClient(t, &mockSecretsService{
		getPolicyFunc: func(_ context.Context, req *service.GetRotationPolicyRequest) (*service.GetRotationPolicyResponse, error) {
			return &service.GetRotationPolicyResponse{
				SecretID: req.SecretID,
				Policy: &service.SecretRotationPolicy{
					Enabled:        true,
					Interval:       time.Hour,
					NextRotation:   next,
					RetainVersions: 5,
				},
			}, nil
		},
	})

	policy, err := client.GetRotationPolicy(context.Background(), "app/db")
	require.NoError(t, err)
	assert.Equal(t, &secrets.RotationPolicy{
		Enabled:        true,
		Interval:       time.Hour,
		NextRotation:   next,
		RetainVersions: 5,
	}, policy)
}

func TestEmbeddedSecrets_GetRotationPolicy_ServiceError(t *testing.T) {
	notFound := errors.NewNotFound("secrets.GetRotationPolicy", "no policy")
	client := newSecretsClient(t, &mockSecretsService{
		getPolicyFunc: func(_ context.Context, _ *service.GetRotationPolicyRequest) (*service.GetRotationPolicyResponse, error) {
			return nil, notFound
		},
	})

	_, err := client.GetRotationPolicy(context.Background(), "app/db")
	assert.True(t, errors.IsNotFound(err), "err = %v, want NotFound", err)
}

// TestEmbeddedSecrets_RotationVerbs_ServiceNotConfigured drives each verb
// through the SDK client with no secrets service wired.
func TestEmbeddedSecrets_RotationVerbs_ServiceNotConfigured(t *testing.T) {
	client := newSecretsClient(t, nil)
	ctx := context.Background()

	calls := map[string]func() error{
		"Rotate": func() error {
			_, err := client.Rotate(ctx, &secrets.RotateSecretRequest{SecretID: "s", NewData: []byte("d")})
			return err
		},
		"UpdateStatus": func() error {
			return client.UpdateStatus(ctx, &secrets.UpdateSecretStatusRequest{SecretID: "s", Status: secrets.SecretStatusEnabled})
		},
		"SetRotationPolicy": func() error {
			return client.SetRotationPolicy(ctx, &secrets.SetRotationPolicyRequest{SecretID: "s", Policy: &secrets.RotationPolicy{}})
		},
		"GetRotationPolicy": func() error {
			_, err := client.GetRotationPolicy(ctx, "s")
			return err
		},
		"GetVersion": func() error {
			_, err := client.GetVersion(ctx, &secrets.GetSecretVersionRequest{SecretID: "s", Version: 1})
			return err
		},
		"ListVersions": func() error {
			_, err := client.ListVersions(ctx, "s")
			return err
		},
		"Rollback": func() error {
			_, err := client.Rollback(ctx, &secrets.RollbackSecretRequest{SecretID: "s", TargetVersion: 1})
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			var notConfigured *ServiceNotConfiguredError
			assert.ErrorAs(t, call(), &notConfigured)
		})
	}
}

func TestEmbeddedSecrets_RotationVerbs_InvalidRequestType(t *testing.T) {
	tr, err := New(&mockQuicBaaS{secretsService: &mockSecretsService{}})
	require.NoError(t, err)

	for _, method := range []string{"Rotate", "UpdateStatus", "SetRotationPolicy", "GetRotationPolicy", "GetVersion", "ListVersions", "Rollback"} {
		t.Run(method, func(t *testing.T) {
			resp, reqErr := tr.Request(context.Background(), "quicbaas.Secrets", method, "not-a-request")
			assert.Nil(t, resp)
			var invalid *InvalidRequestTypeError
			assert.ErrorAs(t, reqErr, &invalid)
		})
	}
}

// secretsWireMethod maps a secrets.Client method to the wire method it sends,
// where the two names differ.
var secretsWireMethod = map[string]string{
	"GetWithBytes": "GetData",
}

// TestEmbeddedSecrets_CoversClient fails when a secrets.Client method has no
// entry in the embedded handler table.
func TestEmbeddedSecrets_CoversClient(t *testing.T) {
	clientType := reflect.TypeOf((*secrets.Client)(nil)).Elem()
	for i := 0; i < clientType.NumMethod(); i++ {
		method := clientType.Method(i).Name
		wire, renamed := secretsWireMethod[method]
		if !renamed {
			wire = method
		}
		if _, routed := unaryHandlers["quicbaas.Secrets/"+wire]; !routed {
			t.Errorf("secrets.Client.%s has no embedded handler", method)
		}
	}
}
