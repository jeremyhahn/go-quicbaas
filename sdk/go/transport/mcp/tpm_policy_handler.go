package mcp

import (
	"strings"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// tpmPolicyServiceName is the SDK service identifier for the go-xkms
// TPMPolicyService verbs.
const tpmPolicyServiceName = "quicbaas.TPMPolicy"

// isMCPTPMPolicyService reports whether service names the TPM-policy surface.
func isMCPTPMPolicyService(service string) bool {
	return strings.HasPrefix(service, tpmPolicyServiceName) || service == "TPMPolicy"
}

// newTPMPolicyUnsupportedError is the typed refusal for every TPM-policy
// verb on MCP. The server's xkms tool set registers no TPM-policy tools, so
// the transport fails fast instead of calling a tool that does not exist;
// REST, QUIC, gRPC and Unix serve these verbs.
func newTPMPolicyUnsupportedError(method string) error {
	return errors.NewUnimplemented("mcp.tpmPolicy."+method,
		"the MCP server registers no TPM-policy tools; use the REST, QUIC, gRPC or Unix transport")
}
