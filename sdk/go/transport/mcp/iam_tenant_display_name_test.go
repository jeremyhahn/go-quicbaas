package mcp

import "testing"

func TestTransformResponse_TenantGetLiftsSettingsDisplayName(t *testing.T) {
	h := newIAMHandlerForTest(t)

	out, err := h.transformResponse("quicbaas.IAM.Tenant", "Get", map[string]interface{}{
		"tenant": map[string]interface{}{
			"id":       "acme",
			"name":     "Acme",
			"settings": map[string]interface{}{"display_name": "Acme Corp"},
		},
		"timestamp": "2026-10-18T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("transformResponse: %v", err)
	}

	tenant := out.(map[string]interface{})["tenant"].(map[string]interface{})
	if tenant["display_name"] != "Acme Corp" {
		t.Errorf("display_name = %v, want Acme Corp", tenant["display_name"])
	}
	if tenant["name"] != "Acme" {
		t.Errorf("name = %v, want Acme unchanged", tenant["name"])
	}
}

func TestTransformResponse_TenantUpdateWithoutSettingsIsUnchanged(t *testing.T) {
	h := newIAMHandlerForTest(t)

	out, err := h.transformResponse("quicbaas.IAM.Tenant", "Update", map[string]interface{}{
		"id":   "acme",
		"name": "Acme",
	})
	if err != nil {
		t.Fatalf("transformResponse: %v", err)
	}

	tenant := out.(map[string]interface{})
	if _, ok := tenant["display_name"]; ok {
		t.Errorf("display_name must not be invented without settings: %v", tenant)
	}
}
