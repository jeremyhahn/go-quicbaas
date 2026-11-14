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

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane/serviceplanetest"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

func servicesToolRegistry(t *testing.T) (*ToolRegistry, *serviceplanetest.Runtime) {
	t.Helper()
	rt := serviceplanetest.NewRuntime(t)
	return &ToolRegistry{service: &mockService{servicesSvc: service.NewServicesService(rt.Resolver(t))}}, rt
}

// manifestArg is a signed manifest as an MCP client sends it: decoded JSON.
func manifestArg(t *testing.T, sm *serviceplane.SignedManifest) map[string]interface{} {
	t.Helper()
	raw, err := json.Marshal(sm)
	require.NoError(t, err)
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &m))
	return map[string]interface{}{"manifest": m}
}

func qbCode(t *testing.T, err error) qberrors.ErrorCode {
	t.Helper()
	var qb *qberrors.QBError
	require.True(t, errors.As(err, &qb), "%v is not a QBError", err)
	return qb.Code
}

func TestServicesTools_TheSevenVerbs(t *testing.T) {
	reg, rt := servicesToolRegistry(t)
	ctx := tenantctx.WithTenant(context.Background(), "acme")
	name := map[string]interface{}{"name": "web"}

	out, err := reg.executeServicesTool(ctx, "quicbaas_services_deploy", manifestArg(t, rt.Sign(t, "acme", "web")))
	require.NoError(t, err)
	rec := out.(*service.ServiceRecord)
	assert.Equal(t, "running", rec.State)
	assert.Equal(t, "secrets:acme/web", rec.Grants["secrets"])

	for _, step := range []struct{ tool, state string }{
		{"quicbaas_services_stop", "stopped"}, {"quicbaas_services_start", "running"},
		{"quicbaas_services_restart", "running"}, {"quicbaas_services_get", "running"},
	} {
		out, err = reg.executeServicesTool(ctx, step.tool, name)
		require.NoError(t, err, step.tool)
		assert.Equal(t, step.state, out.(*service.ServiceRecord).State, step.tool)
	}
	out, err = reg.executeServicesTool(ctx, "quicbaas_services_redeploy", manifestArg(t, rt.Sign(t, "acme", "web")))
	require.NoError(t, err)
	assert.EqualValues(t, 2, out.(*service.ServiceRecord).Revision)

	out, err = reg.executeServicesTool(ctx, "quicbaas_services_list", nil)
	require.NoError(t, err)
	assert.Len(t, out.(*service.ListServicesResponse).Services, 1)

	_, err = reg.executeServicesTool(ctx, "quicbaas_services_undeploy", name)
	require.NoError(t, err)
	_, err = reg.executeServicesTool(ctx, "quicbaas_services_get", name)
	assert.Equal(t, qberrors.ErrNotFound, qbCode(t, err))
}

func TestServicesTools_ResumeAfterAFailedLaunch(t *testing.T) {
	reg, rt := servicesToolRegistry(t)
	ctx := tenantctx.WithTenant(context.Background(), "acme")
	rt.Launcher.Fail(errors.New("image pull failed"))
	_, err := reg.executeServicesTool(ctx, "quicbaas_services_deploy", manifestArg(t, rt.Sign(t, "acme", "web")))
	require.Error(t, err)
	rt.Launcher.Fail(nil)
	out, err := reg.executeServicesTool(ctx, "quicbaas_services_resume", map[string]interface{}{"name": "web"})
	require.NoError(t, err)
	assert.Equal(t, "running", out.(*service.ServiceRecord).State)
}

func TestServicesTools_Refusals(t *testing.T) {
	reg, rt := servicesToolRegistry(t)
	ctx := tenantctx.WithTenant(context.Background(), "acme")
	deploy := manifestArg(t, rt.Sign(t, "acme", "web"))
	_, err := reg.executeServicesTool(ctx, "quicbaas_services_deploy", deploy)
	require.NoError(t, err)

	_, err = reg.executeServicesTool(ctx, "quicbaas_services_deploy", deploy)
	assert.Equal(t, qberrors.ErrAlreadyExists, qbCode(t, err))
	_, err = reg.executeServicesTool(ctx, "quicbaas_services_deploy", manifestArg(t, rt.Sign(t, "other", "api")))
	assert.Error(t, err)
	_, err = reg.executeServicesTool(ctx, "quicbaas_services_deploy", map[string]interface{}{})
	assert.Equal(t, qberrors.ErrInvalidInput, qbCode(t, err))
	_, err = reg.executeServicesTool(ctx, "quicbaas_services_deploy", map[string]interface{}{"manifest": "not an object"})
	assert.Equal(t, qberrors.ErrInvalidInput, qbCode(t, err))
	_, err = reg.executeServicesTool(ctx, "quicbaas_services_stop", map[string]interface{}{})
	assert.Equal(t, qberrors.ErrInvalidInput, qbCode(t, err))

	_, err = (&ToolRegistry{service: &mockService{}}).executeServicesTool(ctx, "quicbaas_services_list", nil)
	assert.Error(t, err, "no service plane configured")
}

func TestServicesTools_RegisteredMappedAndDispatched(t *testing.T) {
	for _, tool := range getAllServicesTools() {
		assert.True(t, isServicesTool(tool.Name), tool.Name)
		perm, ok := qrdbToolExactMapping[tool.Name]
		require.True(t, ok, "%s has no qrdb permission", tool.Name)
		assert.Equal(t, "services", perm.resource, tool.Name)
	}
	assert.Len(t, getAllServicesTools(), len(servicesExecutors))
}
