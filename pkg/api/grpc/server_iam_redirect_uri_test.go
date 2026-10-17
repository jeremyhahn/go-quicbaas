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

package grpc

import (
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// newRedirectURITestServer serves the IAM gRPC verbs over a real app manager,
// so the redirect URI refusal under test is the one pkg/iam produces.
func newRedirectURITestServer(t *testing.T) *IAMServer {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	appManager := iam.NewAppManager(iam.NewMemoryStorage(), nil)
	return NewIAMServer(NewIAMServiceAdapter(nil, nil, nil, appManager, nil, nil), log)
}

var refusedRedirectURIs = []struct {
	name   string
	uri    string
	reason string
}{
	{"fragment", "https://app.example/callback#frag", "redirect URI must not contain a fragment"},
	{"ftp scheme", "ftp://app.example/callback", "URI scheme must be HTTPS (or HTTP for localhost)"},
}

func TestIAMServer_RegisterApp_RefusedRedirectURIIsInvalidArgument(t *testing.T) {
	t.Parallel()
	srv := newRedirectURITestServer(t)

	for _, tc := range refusedRedirectURIs {
		t.Run(tc.name, func(t *testing.T) {
			_, err := srv.RegisterApp(callerContext("t1"), &iampb.RegisterAppRequest{
				TenantId: "t1", Name: "MyApp", RedirectUris: []string{tc.uri},
			})
			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, codes.InvalidArgument, st.Code())
			assert.Contains(t, st.Message(), tc.reason)
		})
	}
}

func TestIAMServer_UpdateApp_RefusedRedirectURIIsInvalidArgument(t *testing.T) {
	t.Parallel()
	srv := newRedirectURITestServer(t)
	app, err := srv.RegisterApp(callerContext("t1"), &iampb.RegisterAppRequest{
		TenantId: "t1", Name: "MyApp", RedirectUris: []string{"https://app.example/callback"},
	})
	require.NoError(t, err)

	for _, tc := range refusedRedirectURIs {
		t.Run(tc.name, func(t *testing.T) {
			_, err := srv.UpdateApp(callerContext("t1"), &iampb.UpdateAppRequest{
				TenantId: "t1", ClientId: app.ClientId, Name: "MyApp", RedirectUris: []string{tc.uri},
			})
			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, codes.InvalidArgument, st.Code())
			assert.Contains(t, st.Message(), tc.reason)
		})
	}
}

func TestConvertIAMError_RedirectURIRefusal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want codes.Code
	}{
		{
			name: "wrapped redirect URI refusal is invalid argument",
			err:  &iam.IAMContextError{Message: "register app", Cause: &iam.InvalidRedirectURIError{Reason: "redirect URI cannot be empty"}},
			want: codes.InvalidArgument,
		},
		{
			name: "other IAM context error stays internal",
			err:  &iam.IAMContextError{Message: "register app", Cause: stderrors.New("storage down")},
			want: codes.Internal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, status.Code(convertIAMError(tt.err)))
		})
	}
}
