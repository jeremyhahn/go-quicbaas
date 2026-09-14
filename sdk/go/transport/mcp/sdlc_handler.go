// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"encoding/json"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// SupportsSDLC declares the SDLC service on the MCP transport.
func (t *Transport) SupportsSDLC() bool { return true }

var _ sdlc.SDLCCapableTransport = (*Transport)(nil)

// isMCPSDLCService reports whether the service name is the SDLC service.
func isMCPSDLCService(service string) bool {
	svc := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		svc = service[idx+1:]
	}
	return svc == "SDLC"
}

// sdlcMethodTools maps SDK methods to the server's tool names.
var sdlcMethodTools = map[string]string{
	"Bind":        "sdlc_bind",
	"Unbind":      "sdlc_unbind",
	"Binding":     "sdlc_binding",
	"Delivery":    "sdlc_delivery",
	"ListForges":  "sdlc_forges",
	"Onboard":     "sdlc_onboard",
	"Conformance": "sdlc_conformance",
}

func sdlcToolName(method string) (string, bool) {
	name, ok := sdlcMethodTools[method]
	return name, ok
}

func newSDLCUnsupportedError(method string) error {
	return errors.NewUnimplemented("mcp.sdlc."+method, "sdlc method is not exposed by the MCP tool set")
}

type sdlcMCPTransform func(raw []byte) (interface{}, error)

var sdlcMCPTransforms = map[string]sdlcMCPTransform{
	"Bind":        decodeMCPSDLCBinding,
	"Binding":     decodeMCPSDLCBinding,
	"Unbind":      decodeMCPSDLCUnbound,
	"Delivery":    decodeMCPSDLCDelivery,
	"ListForges":  decodeMCPSDLCForges,
	"Onboard":     decodeMCPSDLCOnboarding,
	"Conformance": decodeMCPSDLCConformance,
}

// transformSDLCResponse decodes a tool result into the SDK's types.
func transformSDLCResponse(method string, result interface{}) (interface{}, error) {
	transform, ok := sdlcMCPTransforms[method]
	if !ok {
		return nil, newSDLCUnsupportedError(method)
	}
	if result == nil {
		return nil, errors.NewInternal("mcp.sdlc."+method, "empty tool result")
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.sdlc."+method, "failed to re-encode tool result")
	}
	return transform(raw)
}

func decodeMCPSDLCBinding(raw []byte) (interface{}, error) {
	var binding sdlc.ProjectBinding
	if err := json.Unmarshal(raw, &binding); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.sdlc.binding", "failed to decode binding")
	}
	return &binding, nil
}

func decodeMCPSDLCUnbound(raw []byte) (interface{}, error) {
	var out struct {
		Unbound bool `json:"unbound"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.sdlc.Unbind", "failed to decode unbind result")
	}
	return &sdlc.UnbindResponse{Unbound: out.Unbound}, nil
}

func decodeMCPSDLCDelivery(raw []byte) (interface{}, error) {
	var delivery sdlc.Delivery
	if err := json.Unmarshal(raw, &delivery); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.sdlc.Delivery", "failed to decode delivery")
	}
	return &delivery, nil
}

func decodeMCPSDLCOnboarding(raw []byte) (interface{}, error) {
	var report sdlc.OnboardingReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.sdlc.Onboard", "failed to decode onboarding report")
	}
	return &report, nil
}

func decodeMCPSDLCConformance(raw []byte) (interface{}, error) {
	var report sdlc.ConformanceReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.sdlc.Conformance", "failed to decode conformance report")
	}
	return &report, nil
}

func decodeMCPSDLCForges(raw []byte) (interface{}, error) {
	var forges sdlc.ListForgesResponse
	if err := json.Unmarshal(raw, &forges); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.sdlc.ListForges", "failed to decode forges")
	}
	return &forges, nil
}
