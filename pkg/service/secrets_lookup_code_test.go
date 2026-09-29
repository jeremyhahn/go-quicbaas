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
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaasErrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
)

func TestSecretLookupCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want quicbaasErrors.ErrorCode
	}{
		{"backend absent", secrets.ErrSecretNotFound, quicbaasErrors.ErrNotFound},
		{"backend deleted", secrets.ErrSecretDeleted, quicbaasErrors.ErrNotFound},
		{"backend version absent", secrets.ErrVersionNotFound, quicbaasErrors.ErrNotFound},
		{"manager absent", &quicbaasErrors.QBError{Code: quicbaasErrors.ErrNotFound, Err: secrets.ErrSecretNotFound}, quicbaasErrors.ErrNotFound},
		{"manager storage failure keeps its code", &quicbaasErrors.QBError{Code: quicbaasErrors.ErrInternal, Err: stderrors.New("barrier sealed")}, quicbaasErrors.ErrInternal},
		{"uncoded failure is unavailable", stderrors.New("disk gone"), quicbaasErrors.ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, secretLookupCode(tt.err))
		})
	}
}

// A sealed store must not read as a missing secret: the reference checks on
// save rely on GetSecret telling the two apart.
func TestSecretsService_GetSecret_StoreFailureIsNotNotFound(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.getFn = func(context.Context, string) (map[string][]byte, map[string]string, error) {
		return nil, nil, &quicbaasErrors.QBError{Code: quicbaasErrors.ErrInternal, Err: stderrors.New("barrier sealed")}
	}
	svc := newSecretsService(mockManager, newMockLogger())

	_, err := svc.GetSecret(context.Background(), &GetSecretRequest{SecretID: "forge-token"})

	require.Error(t, err)
	assert.False(t, quicbaasErrors.IsNotFound(err))
	assert.Equal(t, quicbaasErrors.ErrInternal, quicbaasErrors.Code(err))
}
