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

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane"
)

// servicesToolExecutor serves one service plane tool.
type servicesToolExecutor func(ctx context.Context, svc service.ServicesService, args map[string]interface{}) (interface{}, error)

// servicesExecutors dispatches on the tool name.
var servicesExecutors = map[string]servicesToolExecutor{
	"quicbaas_services_deploy":   servicesManifestTool("services deploy", service.ServicesService.Deploy),
	"quicbaas_services_redeploy": servicesManifestTool("services redeploy", service.ServicesService.Redeploy),
	"quicbaas_services_undeploy": handleServicesUndeploy,
	"quicbaas_services_start":    servicesNameTool("services start", service.ServicesService.Start),
	"quicbaas_services_stop":     servicesNameTool("services stop", service.ServicesService.Stop),
	"quicbaas_services_resume":   servicesNameTool("services resume", service.ServicesService.Resume),
	"quicbaas_services_restart":  servicesNameTool("services restart", service.ServicesService.Restart),
	"quicbaas_services_get":      servicesNameTool("services get", service.ServicesService.Get),
	"quicbaas_services_list":     handleServicesList,
}

// registerServicesTools registers the service plane's tools.
func (r *ToolRegistry) registerServicesTools() {
	for _, tool := range getAllServicesTools() {
		r.addTool(ToolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: convertInputSchema(tool.InputSchema),
		})
	}
}

// isServicesTool reports whether the tool name is a service plane tool.
func isServicesTool(name string) bool {
	_, ok := servicesExecutors[name]
	return ok
}

// executeServicesTool executes a service plane tool.
func (r *ToolRegistry) executeServicesTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	svc := r.service.Services()
	if svc == nil {
		return nil, api.NewServiceNotConfiguredError("services")
	}
	executor, ok := servicesExecutors[name]
	if !ok {
		return nil, api.NewUnknownToolError(name)
	}
	return executor(ctx, svc, args)
}

func servicesManifestTool(op string, verb func(service.ServicesService, context.Context, *service.DeployServiceRequest) (*service.ServiceRecord, error)) servicesToolExecutor {
	return func(ctx context.Context, svc service.ServicesService, args map[string]interface{}) (interface{}, error) {
		raw, ok := args["manifest"]
		if !ok || raw == nil {
			return nil, api.NewRequiredFieldError("manifest")
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			return nil, api.NewInvalidInputCauseError("manifest is not a signed service manifest", err)
		}
		var sm serviceplane.SignedManifest
		if err := json.Unmarshal(encoded, &sm); err != nil {
			return nil, api.NewInvalidInputCauseError("manifest is not a signed service manifest", err)
		}
		rec, err := verb(svc, ctx, &service.DeployServiceRequest{Manifest: &sm})
		if err != nil {
			return nil, api.NewOperationFailedError(op, err)
		}
		return rec, nil
	}
}

func servicesNameTool(op string, verb func(service.ServicesService, context.Context, *service.ServiceNameRequest) (*service.ServiceRecord, error)) servicesToolExecutor {
	return func(ctx context.Context, svc service.ServicesService, args map[string]interface{}) (interface{}, error) {
		name := getString(args, "name")
		if name == "" {
			return nil, api.NewRequiredFieldError("name")
		}
		rec, err := verb(svc, ctx, &service.ServiceNameRequest{Name: name})
		if err != nil {
			return nil, api.NewOperationFailedError(op, err)
		}
		return rec, nil
	}
}

func handleServicesUndeploy(ctx context.Context, svc service.ServicesService, args map[string]interface{}) (interface{}, error) {
	name := getString(args, "name")
	if name == "" {
		return nil, api.NewRequiredFieldError("name")
	}
	if err := svc.Undeploy(ctx, &service.ServiceNameRequest{Name: name}); err != nil {
		return nil, api.NewOperationFailedError("services undeploy", err)
	}
	return map[string]interface{}{"name": name, "undeployed": true}, nil
}

func handleServicesList(ctx context.Context, svc service.ServicesService, _ map[string]interface{}) (interface{}, error) {
	resp, err := svc.List(ctx)
	if err != nil {
		return nil, api.NewOperationFailedError("services list", err)
	}
	return resp, nil
}
