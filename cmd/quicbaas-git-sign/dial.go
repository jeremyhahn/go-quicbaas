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

package main

import (
	"context"
	"errors"
	"io"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc/gitsign"
	sshclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/ssh"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// requestTimeout bounds each call to quicbaas, so a hung server fails the
// commit rather than hanging git.
const requestTimeout = 30 * time.Second

// mcpSigner is the gitsign.OpenSigner that dials quicbaas over MCP with the
// session's client certificate and returns the session key's remote signer.
// The signer asks quicbaas for each signature; quicbaas authorizes the call as
// keys:sign against the certificate's principal and audits it.
func mcpSigner(cfg *gitsign.Config) gitsign.OpenSigner {
	return func(ctx context.Context) (gossh.Signer, io.Closer, error) {
		tlsConfig, err := transport.LoadTLSConfigFromFiles(cfg.CertFile, cfg.KeyFile, cfg.CAFile)
		if err != nil {
			return nil, nil, &gitsign.InvalidConfigError{Variable: orchestration.MCPCertEnvVar, Message: "load the session certificate", Cause: err}
		}
		conn, err := mcp.New(&transport.Config{Address: cfg.MCPURL, TLSConfig: tlsConfig, Timeout: requestTimeout})
		if err != nil {
			return nil, nil, &gitsign.InvalidConfigError{Variable: gitsign.EnvMCPURL, Message: "build the MCP transport", Cause: err}
		}
		key := sshclient.GetKeyRequest{Name: cfg.Key.Name, Scope: cfg.Key.Scope, Owner: cfg.Key.Owner}
		signer, err := sshclient.NewClient(conn).Keys().Signer(ctx, key)
		if err != nil {
			return nil, nil, &gitsign.SignError{Message: "fetch the session key", Cause: errors.Join(err, conn.Close())}
		}
		return signer, conn, nil
	}
}
