//go:build rest && mcp

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

package apimodule

import (
	"errors"
	"testing"

	"github.com/jeremyhahn/go-qrdb/pkg/auth/rbac"
	"github.com/jeremyhahn/go-qrdb/pkg/permission"
	"github.com/jeremyhahn/go-qrdb/pkg/user"
)

// The worker's git signing helper (quicbaas-git-sign) reaches the session key
// through these two tools: export for the public half, sign for each commit.
const (
	sshKeyExportTool = "quicbaas_ssh_key_export"
	sshKeySignTool   = "quicbaas_ssh_key_sign"
)

// TestAgentSession_SigningHelperIsRefusedWithoutKeysSign: a session on its
// floor cannot sign, so the helper's commit fails rather than signing with a
// key the session was never granted.
func TestAgentSession_SigningHelperIsRefusedWithoutKeysSign(t *testing.T) {
	cert := agentSessionPeerCert(t, agentSessionTenant, agentSessionCN)
	am := agentSessionMiddleware(map[string]*user.User{
		agentSessionTenant + "/" + agentSessionCN: enrolledAgentSessionUser(t, cert),
	})
	registry := agentSessionToolRegistry(t)
	ctx, _ := agentSessionCallContext(t, am, cert)

	for _, tool := range []string{sshKeyExportTool, sshKeySignTool} {
		err := am.AuthorizeMCP(ctx, registry, tool)
		var denied *rbac.PermissionDeniedError
		if !errors.As(err, &denied) {
			t.Fatalf("%s: AuthorizeMCP = %v, want a *rbac.PermissionDeniedError", tool, err)
		}
	}
}

// TestAgentSession_SigningHelperIsAdmittedWithKeysSign: a session granted the
// key permissions reaches both tools the helper calls.
func TestAgentSession_SigningHelperIsAdmittedWithKeysSign(t *testing.T) {
	cert := agentSessionPeerCert(t, agentSessionTenant, agentSessionCN)
	am := agentSessionMiddleware(map[string]*user.User{
		agentSessionTenant + "/" + agentSessionCN: enrolledAgentSessionUser(t, cert),
	}, permission.Permission("keys:get"), permission.Permission("keys:sign"))
	registry := agentSessionToolRegistry(t)
	ctx, _ := agentSessionCallContext(t, am, cert)

	for _, tool := range []string{sshKeyExportTool, sshKeySignTool} {
		if _, covered := registry.Lookup(tool); !covered {
			t.Fatalf("%s is not in the tool registry", tool)
		}
		if err := am.AuthorizeMCP(ctx, registry, tool); err != nil {
			t.Errorf("%s must be admitted for a session granted keys:get and keys:sign: %v", tool, err)
		}
	}
}
