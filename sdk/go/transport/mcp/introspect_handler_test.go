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

package mcp

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/auth"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

const introspectTool = "quicbaas_introspecttoken"

func TestIntrospectTokenOverMCP_Active(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: introspectTool,
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if args["token"] != "live" {
				t.Errorf("server received %v", args)
			}
		},
		result: map[string]interface{}{
			"Active":       true,
			"Subject":      "user-1",
			"ExpiresAt":    "1970-01-01T00:50:00Z",
			"AttestLevel":  "hardware",
			"AttestAAGUID": "cb69481e8ff7403993ec0a2729a154a8",
		},
	})
	defer cleanup()

	got, err := auth.NewClient(tr).IntrospectToken(context.Background(), "live")
	if err != nil {
		t.Fatalf("IntrospectToken: %v", err)
	}
	want := &service.TokenIntrospection{
		Active:       true,
		Subject:      "user-1",
		ExpiresAt:    time.Unix(3000, 0).UTC(),
		AttestLevel:  "hardware",
		AttestAAGUID: "cb69481e8ff7403993ec0a2729a154a8",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestIntrospectTokenOverMCP_InactiveCarriesOnlyActive(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: introspectTool,
		result:   map[string]interface{}{"Active": false},
	})
	defer cleanup()

	got, err := auth.NewClient(tr).IntrospectToken(context.Background(), "garbage")
	if err != nil {
		t.Fatalf("IntrospectToken: %v", err)
	}
	if !reflect.DeepEqual(got, &service.TokenIntrospection{Active: false}) {
		t.Errorf("inactive introspection must carry only active=false, got %+v", got)
	}
}

func TestIntrospectTokenOverMCP_MissingTokenIsInvalidInput(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:      introspectTool,
		toolError:     true,
		toolErrorCode: MCPInvalidInput,
		rawText:       "token is required",
	})
	defer cleanup()

	_, err := tr.Request(context.Background(), "quicbaas.Auth", "IntrospectToken", &service.IntrospectTokenRequest{})
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid-input error, got %v", err)
	}
}

func TestIntrospectTokenOverMCP_MalformedReplyIsInternal(t *testing.T) {
	_, err := newAuthHandler(nil).transformIntrospectTokenResponse(map[string]interface{}{"Active": "yes"})
	if errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected internal error for a malformed reply, got %v", err)
	}
}
