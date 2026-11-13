//go:build rest

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

package server_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/server"
)

// TestSealedBoot_QUICServesHealth pins the sealed window's QUIC surface:
// quicbaasd serves QUIC as REST over HTTP/3 on the UDP side of the REST
// port, so a sealed node answers the allowlisted health routes over HTTP/3
// the way the full boot does. Without it the init matrix's shamir_quic cell
// never saw its transport come up.
func TestSealedBoot_QUICServesHealth(t *testing.T) {
	cfg := server.NewSealedBootTestConfig(t)
	cfg.Security.TLSEnabled = true

	srv, err := server.NewServer(cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	startErr := make(chan error, 1)
	go func() { startErr <- srv.Start() }()

	address := fmt.Sprintf("127.0.0.1:%d", cfg.Server.REST.Port)
	sdkWaitFor(t, startErr, "the sealed-window REST listener", func() bool {
		conn, dialErr := net.DialTimeout("tcp", address, 200*time.Millisecond)
		if dialErr != nil {
			return false
		}
		_ = conn.Close()
		return true
	})
	require.Nil(t, srv.GetQRDBNodeClient(), "the node must still be sealed")

	h3 := &http3.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13,
		// The node's self-issued certificate is not what this test pins;
		// the HTTP/3 listener is.
		InsecureSkipVerify: true,
	}}
	t.Cleanup(func() { _ = h3.Close() })
	client := &http.Client{Transport: h3, Timeout: 10 * time.Second}

	resp, err := client.Get("https://" + address + "/api/v1/health/live")
	require.NoError(t, err, "a sealed node must serve HTTP/3 on the REST port")
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, http.StatusOK, resp.StatusCode)
}
