package mcp

import "testing"

// TestTrustImportToolsRequireTrustWrite pins #2070 on MCP: every tool that
// imports issuer certificates or publishes trust records is gated trust:write,
// as CertificateService/Chain and TrustService/AutoImportAIA and /PublishDANE
// are on gRPC and POST /api/v1/certificate/chain and /api/v1/trust/* on REST.
// Before the fix MCP gated all three under certs:generate, so a caller allowed
// to issue leaf certificates could also write the trust store.
func TestTrustImportToolsRequireTrustWrite(t *testing.T) {
	for _, tool := range []string{
		"quicbaas_certificate_chain",
		"quicbaas_trust_auto_import_aia",
		"quicbaas_trust_publish_dane",
	} {
		resource, action, ok := getToolQRDBPermission(tool)
		if !ok {
			t.Errorf("tool %q resolves to no permission", tool)
			continue
		}
		if resource != "trust" || action != "write" {
			t.Errorf("tool %q is gated %s:%s, want trust:write as on gRPC and REST", tool, resource, action)
		}
	}
}
