// Copyright 2026 Jeremy Hahn
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package quicbaas

import (
	"context"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubDEK implements QRDBDEKClient with signatures written only in the
// sdk/go aliases, so a consumer needs no go-qrdb import to implement or
// call the tenant DEK lifecycle.
type stubDEK struct {
	versions []QRDBTenantDEKVersion
}

var _ QRDBDEKClient = (*stubDEK)(nil)

func (s *stubDEK) status(tenant string) *QRDBTenantDEKStatusResponse {
	var current uint64
	if n := len(s.versions); n > 0 {
		current = s.versions[n-1].Version
	}
	return &QRDBTenantDEKStatusResponse{
		TenantID:       tenant,
		Algorithm:      "AES-256-GCM",
		CurrentVersion: current,
		Versions:       append([]QRDBTenantDEKVersion(nil), s.versions...),
	}
}

func (s *stubDEK) TenantDEKStatus(_ context.Context, req *QRDBTenantDEKRequest) (*QRDBTenantDEKStatusResponse, error) {
	return s.status(req.TenantID), nil
}

func (s *stubDEK) TenantDEKRotate(_ context.Context, req *QRDBTenantDEKRequest) (*QRDBTenantDEKStatusResponse, error) {
	s.versions = append(s.versions, QRDBTenantDEKVersion{
		Version:   uint64(len(s.versions) + 1),
		CreatedAt: time.Unix(1700000000, 0).UTC(),
	})
	return s.status(req.TenantID), nil
}

func (s *stubDEK) TenantDEKRetire(_ context.Context, _ *QRDBTenantDEKRetireRequest) (*QRDBTenantDEKStatusResponse, error) {
	return s.status(""), nil
}

func (s *stubDEK) TenantDEKEncrypt(_ context.Context, _ *QRDBTenantDEKEncryptRequest) (*QRDBTenantDEKEncryptResponse, error) {
	return &QRDBTenantDEKEncryptResponse{}, nil
}

func (s *stubDEK) TenantDEKDecrypt(_ context.Context, _ *QRDBTenantDEKDecryptRequest) (*QRDBTenantDEKDecryptResponse, error) {
	return &QRDBTenantDEKDecryptResponse{}, nil
}

func (s *stubDEK) TenantDEKRewrap(_ context.Context, _ *QRDBTenantDEKRewrapRequest) (*QRDBTenantDEKRewrapResponse, error) {
	return &QRDBTenantDEKRewrapResponse{}, nil
}

func TestQRDBTenantDEKAliases_IdenticalToGoQRDBTypes(t *testing.T) {
	// Each assignment compiles only if the alias and the go-qrdb type
	// are the same type.
	var req qrdbtransport.TenantDEKRequest = QRDBTenantDEKRequest{TenantID: "t1"}
	var reqBack QRDBTenantDEKRequest = req
	assert.Equal(t, "t1", reqBack.TenantID)

	var _ qrdbtransport.TenantDEKRetireRequest = QRDBTenantDEKRetireRequest{}
	var _ QRDBTenantDEKRetireRequest = qrdbtransport.TenantDEKRetireRequest{}
	var _ qrdbtransport.TenantDEKVersion = QRDBTenantDEKVersion{}
	var _ QRDBTenantDEKVersion = qrdbtransport.TenantDEKVersion{}
	var _ qrdbtransport.TenantDEKStatusResponse = QRDBTenantDEKStatusResponse{}
	var _ QRDBTenantDEKStatusResponse = qrdbtransport.TenantDEKStatusResponse{}
	var _ qrdbtransport.TenantDEKEncryptRequest = QRDBTenantDEKEncryptRequest{}
	var _ QRDBTenantDEKEncryptRequest = qrdbtransport.TenantDEKEncryptRequest{}
	var _ qrdbtransport.TenantDEKEncryptResponse = QRDBTenantDEKEncryptResponse{}
	var _ QRDBTenantDEKEncryptResponse = qrdbtransport.TenantDEKEncryptResponse{}
	var _ qrdbtransport.TenantDEKDecryptRequest = QRDBTenantDEKDecryptRequest{}
	var _ QRDBTenantDEKDecryptRequest = qrdbtransport.TenantDEKDecryptRequest{}
	var _ qrdbtransport.TenantDEKDecryptResponse = QRDBTenantDEKDecryptResponse{}
	var _ QRDBTenantDEKDecryptResponse = qrdbtransport.TenantDEKDecryptResponse{}
	var _ qrdbtransport.TenantDEKRewrapRequest = QRDBTenantDEKRewrapRequest{}
	var _ QRDBTenantDEKRewrapRequest = qrdbtransport.TenantDEKRewrapRequest{}
	var _ qrdbtransport.TenantDEKRewrapResponse = QRDBTenantDEKRewrapResponse{}
	var _ QRDBTenantDEKRewrapResponse = qrdbtransport.TenantDEKRewrapResponse{}

	var _ qrdbtransport.DEKClient = QRDBDEKClient(nil)
}

func TestQRDBDEKClient_StubRoundTripsThroughAliases(t *testing.T) {
	var dek QRDBDEKClient = &stubDEK{}
	ctx := context.Background()

	_, err := dek.TenantDEKRotate(ctx, &QRDBTenantDEKRequest{})
	require.NoError(t, err)
	st, err := dek.TenantDEKRotate(ctx, &QRDBTenantDEKRequest{TenantID: "t1"})
	require.NoError(t, err)

	assert.Equal(t, "t1", st.TenantID)
	assert.Equal(t, uint64(2), st.CurrentVersion)
	require.Len(t, st.Versions, 2)
	assert.Equal(t, uint64(1), st.Versions[0].Version)
	assert.Equal(t, uint64(2), st.Versions[1].Version)
	assert.Nil(t, st.Versions[1].RetiredAt)
}
