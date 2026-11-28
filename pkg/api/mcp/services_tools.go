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

// servicesManifestSchema is the input of deploy and redeploy: the signed
// manifest, its payload and signature base64-encoded as JSON bytes are.
var servicesManifestSchema = InputSchema{
	Type: "object",
	Properties: map[string]Property{
		"manifest": {Type: "object", Description: "Signed service manifest: {payload, key_id, signature}, payload and signature base64-encoded"},
	},
	Required: []string{"manifest"},
}

// servicesNameSchema is the input of the verbs that address a service by name.
var servicesNameSchema = InputSchema{
	Type: "object",
	Properties: map[string]Property{
		"name": {Type: "string", Description: "Name of one of your tenant's services"},
	},
	Required: []string{"name"},
}

// The service plane's tools (#1687): the seven lifecycle verbs, get and list.
var (
	ServicesDeployTool   = Tool{Name: "quicbaas_services_deploy", Description: "Deploy a service from a signed manifest: verify it, grant its identity, certificate, name, RBAC and secrets, and launch it", InputSchema: servicesManifestSchema}
	ServicesRedeployTool = Tool{Name: "quicbaas_services_redeploy", Description: "Redeploy a settled service from a newer signed manifest", InputSchema: servicesManifestSchema}
	ServicesUndeployTool = Tool{Name: "quicbaas_services_undeploy", Description: "Undeploy a settled service: halt it and revoke its grants", InputSchema: servicesNameSchema}
	ServicesStartTool    = Tool{Name: "quicbaas_services_start", Description: "Start a stopped service", InputSchema: servicesNameSchema}
	ServicesStopTool     = Tool{Name: "quicbaas_services_stop", Description: "Stop a running service", InputSchema: servicesNameSchema}
	ServicesResumeTool   = Tool{Name: "quicbaas_services_resume", Description: "Resume a service whose last verb failed", InputSchema: servicesNameSchema}
	ServicesRestartTool  = Tool{Name: "quicbaas_services_restart", Description: "Restart a running or halted service", InputSchema: servicesNameSchema}
	ServicesGetTool      = Tool{Name: "quicbaas_services_get", Description: "Read one of your tenant's services", InputSchema: servicesNameSchema}
	ServicesListTool     = Tool{Name: "quicbaas_services_list", Description: "List your tenant's services", InputSchema: InputSchema{Type: "object", Properties: map[string]Property{}}}
)

// getAllServicesTools returns the service plane's tools (9 tools).
func getAllServicesTools() []Tool {
	return []Tool{
		ServicesDeployTool, ServicesRedeployTool, ServicesUndeployTool,
		ServicesStartTool, ServicesStopTool, ServicesResumeTool, ServicesRestartTool,
		ServicesGetTool, ServicesListTool,
	}
}
