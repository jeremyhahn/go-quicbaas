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
	"strings"

	servicesclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/services"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// The service plane over MCP: the quicbaas_services_{deploy,redeploy,
// undeploy,start,stop,resume,restart,get,list} tools of
// pkg/api/mcp/services_tools.go. The signed manifest crosses as
// {"manifest":{"payload","key_id","signature"}}, the byte fields base64 as
// encoding/json writes them on both sides.

// isMCPServicesService reports whether the service name targets the service
// plane.
func isMCPServicesService(service string) bool {
	return strings.HasPrefix(service, servicesclient.ServiceName)
}

// servicesArguments converts a services request into tool arguments.
func servicesArguments(method string, req interface{}) (map[string]interface{}, error) {
	op := "mcp.services." + method
	switch method {
	case "Deploy", "Redeploy":
		r, ok := req.(*servicesclient.DeployRequest)
		if !ok || r == nil || r.Manifest == nil {
			return nil, errors.NewInvalidInput(op, "invalid request type, expected *services.DeployRequest with a manifest")
		}
		return map[string]interface{}{"manifest": map[string]interface{}{
			"payload": r.Manifest.Payload, "key_id": r.Manifest.KeyID, "signature": r.Manifest.Signature,
		}}, nil
	case "Undeploy", "Start", "Stop", "Resume", "Restart", "Get":
		r, ok := req.(*servicesclient.NameRequest)
		if !ok || r == nil {
			return nil, errors.NewInvalidInput(op, "invalid request type, expected *services.NameRequest")
		}
		return map[string]interface{}{"name": r.Name}, nil
	case "List":
		return map[string]interface{}{}, nil
	}
	return nil, errors.NewUnimplemented(op, "method not exposed by the services MCP tool set")
}

// servicesResult decodes a tool result into the SDK type: a record for every
// verb but List (a ListResponse) and Undeploy (nothing).
func servicesResult(method string, result interface{}) (interface{}, error) {
	var out interface{}
	switch method {
	case "Undeploy":
		return nil, nil
	case "List":
		out = &servicesclient.ListResponse{}
	default:
		out = &servicesclient.Record{}
	}
	if err := remarshal(result, out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.services."+method, "failed to decode tool result")
	}
	return out, nil
}
