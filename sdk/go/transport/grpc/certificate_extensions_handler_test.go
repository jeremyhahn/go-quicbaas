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
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/certificate"
)

// TestCertificateExtensionsAreDispatched proves the three extension verbs are
// registered in the handler map. An unregistered method falls into the
// "not exposed by CertificateService gRPC proto" branch, which is exactly the
// silent-failure shape this change removes. The Unix transport delegates its
// whole Request path here, so this also covers Unix.
func TestCertificateExtensionsAreDispatched(t *testing.T) {
	for _, method := range []string{"CrossSign", "Chain", "IssueDeviceIdentity"} {
		_, ok := certificateHandlerMap[method]
		assert.True(t, ok, "method %s must be dispatched by the gRPC certificate handler", method)
	}
}

// TestCertificateExtensionsRejectWrongRequestType exercises the validation
// that runs before any RPC is attempted, so a nil connection is never dialled.
func TestCertificateExtensionsRejectWrongRequestType(t *testing.T) {
	h := &certificateHandler{}
	ctx := context.Background()

	t.Run("CrossSign", func(t *testing.T) {
		_, err := h.handleCrossSign(ctx, nil, &certificate.ChainRequest{})
		require.Error(t, err)

		_, err = h.handleCrossSign(ctx, nil, &certificate.CrossSignRequest{})
		require.Error(t, err, "an empty foreign root must be rejected client-side")
	})

	t.Run("Chain", func(t *testing.T) {
		_, err := h.handleChain(ctx, nil, &certificate.CrossSignRequest{})
		require.Error(t, err)

		_, err = h.handleChain(ctx, nil, &certificate.ChainRequest{})
		require.Error(t, err, "an empty certificate must be rejected client-side")
	})

	t.Run("IssueDeviceIdentity", func(t *testing.T) {
		_, err := h.handleIssueDeviceIdentity(ctx, nil, &certificate.ChainRequest{})
		require.Error(t, err)

		_, err = h.handleIssueDeviceIdentity(ctx, nil, &certificate.DeviceIdentityRequest{DeviceID: "d"})
		require.Error(t, err, "a missing identity type must be rejected client-side")

		_, err = h.handleIssueDeviceIdentity(ctx, nil, &certificate.DeviceIdentityRequest{
			Type: certificate.DeviceIdentityEK,
		})
		require.Error(t, err, "a missing device ID must be rejected client-side")
	})
}

// TestCertificateExtensionsRouteThroughCertificateService confirms the service
// name predicate accepts the extension surface, which is what makes
// Transport.Request branch into the typed handler rather than the untyped
// conn.Invoke path.
func TestCertificateExtensionsRouteThroughCertificateService(t *testing.T) {
	assert.True(t, isCertificateService("quicbaas.Certificate"))
}
