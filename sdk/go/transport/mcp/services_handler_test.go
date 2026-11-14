//go:build mcp

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

package mcp_test

import (
	"context"
	"encoding/json"
	"testing"

	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane/serviceplanetest"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	servicesclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/services"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

type servicesQuicBaaS struct {
	*testutil.MockQuicBaaS
	services service.ServicesService
}

func (s *servicesQuicBaaS) Services() service.ServicesService { return s.services }

// servicesOverMCP encodes each call as the SDK does, runs the tool the SDK
// names on the real server registry over a real plane, and decodes the
// result as the SDK does.
func servicesOverMCP(t *testing.T) (func(method string, req interface{}) (interface{}, error), *serviceplanetest.Runtime) {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatal(err)
	}
	rt := serviceplanetest.NewRuntime(t)
	registry := mcpapi.NewToolRegistry(&servicesQuicBaaS{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		services:     service.NewServicesService(rt.Resolver(t)),
	}, log)
	ctx := tenantctx.WithTenant(context.Background(), "acme")
	return func(method string, req interface{}) (interface{}, error) {
		args, err := mcp.ServicesArguments(method, req)
		if err != nil {
			return nil, err
		}
		result, err := registry.ExecuteTool(ctx, mcp.ToolName(servicesclient.ServiceName, method), args)
		if err != nil {
			return nil, err
		}
		// Cross the wire: the transport sees the result as decoded JSON.
		raw, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		var wire interface{}
		if err := json.Unmarshal(raw, &wire); err != nil {
			return nil, err
		}
		return mcp.ServicesResult(method, wire)
	}, rt
}

func TestMCPServices_TheSevenVerbsOnTheRealTools(t *testing.T) {
	call, rt := servicesOverMCP(t)
	sign := func(name string) *servicesclient.DeployRequest {
		sm := rt.Sign(t, "acme", name)
		return &servicesclient.DeployRequest{Manifest: &servicesclient.SignedManifest{Payload: sm.Payload, KeyID: sm.KeyID, Signature: sm.Signature}}
	}
	out, err := call("Deploy", sign("web"))
	if err != nil {
		t.Fatal(err)
	}
	rec := out.(*servicesclient.Record)
	var m struct{ Name string }
	if err := json.Unmarshal(rec.Manifest, &m); err != nil || m.Name != "web" || rec.State != "running" || rec.Grants["secrets"] != "secrets:acme/web" {
		t.Fatalf("deployed = %+v (%v)", rec, err)
	}
	name := &servicesclient.NameRequest{Name: "web"}
	for _, step := range []struct{ method, state string }{
		{"Stop", "stopped"}, {"Start", "running"}, {"Restart", "running"}, {"Get", "running"},
	} {
		out, err := call(step.method, name)
		if err != nil || out.(*servicesclient.Record).State != step.state {
			t.Fatalf("%s = %+v, %v", step.method, out, err)
		}
	}
	if out, err := call("Redeploy", sign("web")); err != nil || out.(*servicesclient.Record).Revision != 2 {
		t.Fatalf("redeploy = %+v, %v", out, err)
	}
	if out, err := call("List", &servicesclient.ListRequest{}); err != nil || len(out.(*servicesclient.ListResponse).Services) != 1 {
		t.Fatalf("list = %+v, %v", out, err)
	}
	if out, err := call("Undeploy", name); err != nil || out != nil {
		t.Fatalf("undeploy = %+v, %v", out, err)
	}
	if _, err := call("Get", name); err == nil {
		t.Fatal("get after undeploy succeeded")
	}
}

func TestMCPServices_ArgumentErrors(t *testing.T) {
	if _, err := mcp.ServicesArguments("Deploy", &servicesclient.NameRequest{}); !sdkerrors.IsInvalidInput(err) {
		t.Fatalf("deploy without a manifest = %v", err)
	}
	if _, err := mcp.ServicesArguments("Start", "wrong"); !sdkerrors.IsInvalidInput(err) {
		t.Fatalf("start with the wrong type = %v", err)
	}
	if _, err := mcp.ServicesArguments("Watch", nil); sdkerrors.Code(err) != sdkerrors.CodeUnimplemented {
		t.Fatalf("unknown method = %v", err)
	}
	if _, err := mcp.ServicesResult("Get", map[string]interface{}{"revision": "x"}); sdkerrors.Code(err) != sdkerrors.CodeInternal {
		t.Fatalf("malformed result = %v", err)
	}
	if got := mcp.ToolName(servicesclient.ServiceName, "Restart"); got != "quicbaas_services_restart" {
		t.Fatalf("tool name = %s", got)
	}
}
