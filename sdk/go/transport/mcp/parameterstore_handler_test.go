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

	psclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/parameterstore"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

const psStamp = "2026-10-01T12:00:00Z"

var psWhen = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// psResultParameter is one parameter as the server's formatParameterValue
// renders it, with every field populated.
func psResultParameter() map[string]interface{} {
	return map[string]interface{}{
		"path":          "/app/db/host",
		"value":         "db-primary",
		"type":          "string",
		"description":   "database host",
		"tags":          map[string]interface{}{"env": "prod"},
		"is_encrypted":  true,
		"kms_key_id":    "key-1",
		"expires_at":    psStamp,
		"audit_enabled": true,
		"created_at":    psStamp,
		"updated_at":    psStamp,
		"created_by":    "alice",
		"updated_by":    "bob",
	}
}

// assertPSParameter checks that every field of psResultParameter arrived.
func assertPSParameter(t *testing.T, p *psclient.ParameterValue) {
	t.Helper()
	if p == nil {
		t.Fatal("parameter is nil")
	}
	if p.Path != "/app/db/host" || string(p.Value) != "db-primary" || p.Type != "string" ||
		p.Description != "database host" || !reflect.DeepEqual(p.Tags, map[string]string{"env": "prod"}) ||
		!p.IsEncrypted || p.KMSKeyID != "key-1" || !p.AuditEnabled ||
		p.CreatedBy != "alice" || p.UpdatedBy != "bob" {
		t.Errorf("parameter = %+v", p)
	}
	if p.ExpiresAt == nil || !p.ExpiresAt.Equal(psWhen) || !p.CreatedAt.Equal(psWhen) || !p.UpdatedAt.Equal(psWhen) {
		t.Errorf("parameter times: expires=%v created=%v updated=%v", p.ExpiresAt, p.CreatedAt, p.UpdatedAt)
	}
}

// assertPSArgs checks the tool received exactly want.
func assertPSArgs(want map[string]interface{}) func(t *testing.T, args map[string]interface{}) {
	return func(t *testing.T, args map[string]interface{}) {
		if !reflect.DeepEqual(args, want) {
			t.Errorf("tool arguments = %v, want %v", args, want)
		}
	}
}

// TestParameterStoreMCP_RoundTrip drives each ParameterStore verb through the
// SDK client and the MCP transport to the tool the server registers for it,
// asserting the string-typed arguments the server parses and every field of
// the typed result.
func TestParameterStoreMCP_RoundTrip(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name   string
		exp    mcpToolExpectation
		invoke func(t *testing.T, c psclient.Client)
	}{
		{
			name: "GetParameter",
			exp: mcpToolExpectation{
				wantTool:   "quicbaas_parameter_get",
				assertArgs: assertPSArgs(map[string]interface{}{"path": "/app/db/host", "with_decryption": "true"}),
				result:     map[string]interface{}{"parameter": psResultParameter(), "response_timestamp": psStamp},
			},
			invoke: func(t *testing.T, c psclient.Client) {
				resp, err := c.GetParameter(ctx, &psclient.GetParameterRequest{Path: "/app/db/host", WithDecryption: true})
				if err != nil {
					t.Fatalf("GetParameter: %v", err)
				}
				assertPSParameter(t, resp.Parameter)
				if !resp.Timestamp.Equal(psWhen) {
					t.Errorf("Timestamp = %v", resp.Timestamp)
				}
			},
		},
		{
			name: "SetParameter",
			exp: mcpToolExpectation{
				wantTool: "quicbaas_parameter_set",
				assertArgs: assertPSArgs(map[string]interface{}{
					"path": "/app/db/host", "value": "db-primary", "type": "string",
					"description": "database host", "kms_key_id": "key-1", "tags": `{"env":"prod"}`,
					"ttl_seconds": "3600", "audit_enabled": "true", "overwrite": "true",
				}),
				result: map[string]interface{}{"path": "/app/db/host", "created": true, "timestamp": psStamp},
			},
			invoke: func(t *testing.T, c psclient.Client) {
				resp, err := c.SetParameter(ctx, &psclient.SetParameterRequest{
					Path: "/app/db/host", Value: []byte("db-primary"), Type: "string",
					Description: "database host", KMSKeyID: "key-1", Tags: map[string]string{"env": "prod"},
					TTLSeconds: 3600, AuditEnabled: true, Overwrite: true,
				})
				if err != nil {
					t.Fatalf("SetParameter: %v", err)
				}
				if resp.Path != "/app/db/host" || !resp.Created || !resp.Timestamp.Equal(psWhen) {
					t.Errorf("response = %+v", resp)
				}
			},
		},
		{
			name: "DeleteParameter",
			exp: mcpToolExpectation{
				wantTool:   "quicbaas_parameter_delete",
				assertArgs: assertPSArgs(map[string]interface{}{"path": "/app/db/host"}),
				result:     map[string]interface{}{"path": "/app/db/host", "deleted": true, "timestamp": psStamp},
			},
			invoke: func(t *testing.T, c psclient.Client) {
				resp, err := c.DeleteParameter(ctx, &psclient.DeleteParameterRequest{Path: "/app/db/host"})
				if err != nil {
					t.Fatalf("DeleteParameter: %v", err)
				}
				if resp.Path != "/app/db/host" || !resp.Deleted || !resp.Timestamp.Equal(psWhen) {
					t.Errorf("response = %+v", resp)
				}
			},
		},
		{
			name: "ListParameters",
			exp: mcpToolExpectation{
				wantTool: "quicbaas_parameter_list",
				assertArgs: assertPSArgs(map[string]interface{}{
					"prefix": "/app/", "recursive": "true", "with_decryption": "true",
					"with_values": "true", "limit": "10", "cursor": "c1",
				}),
				result: map[string]interface{}{
					"parameters":  []interface{}{psResultParameter()},
					"next_cursor": "c2", "has_more": true, "total_count": 42, "timestamp": psStamp,
				},
			},
			invoke: func(t *testing.T, c psclient.Client) {
				resp, err := c.ListParameters(ctx, &psclient.ListParametersRequest{
					Prefix: "/app/", Recursive: true, WithDecryption: true, WithValues: true, Limit: 10, Cursor: "c1",
				})
				if err != nil {
					t.Fatalf("ListParameters: %v", err)
				}
				if len(resp.Parameters) != 1 {
					t.Fatalf("Parameters = %d, want 1", len(resp.Parameters))
				}
				assertPSParameter(t, resp.Parameters[0])
				if resp.NextCursor != "c2" || !resp.HasMore || resp.TotalCount != 42 || !resp.Timestamp.Equal(psWhen) {
					t.Errorf("response = %+v", resp)
				}
			},
		},
		{
			name: "SearchByTags",
			exp: mcpToolExpectation{
				wantTool: "quicbaas_parameter_search",
				assertArgs: assertPSArgs(map[string]interface{}{
					"tags": `{"env":"prod"}`, "prefix": "/app/", "with_values": "true",
					"with_decryption": "true", "limit": "5", "cursor": "c1",
				}),
				result: map[string]interface{}{
					"parameters":  []interface{}{psResultParameter()},
					"next_cursor": "c2", "has_more": true, "timestamp": psStamp,
				},
			},
			invoke: func(t *testing.T, c psclient.Client) {
				resp, err := c.SearchByTags(ctx, &psclient.SearchByTagsRequest{
					Tags: map[string]string{"env": "prod"}, Prefix: "/app/", WithValues: true,
					WithDecryption: true, Limit: 5, Cursor: "c1",
				})
				if err != nil {
					t.Fatalf("SearchByTags: %v", err)
				}
				if len(resp.Parameters) != 1 {
					t.Fatalf("Parameters = %d, want 1", len(resp.Parameters))
				}
				assertPSParameter(t, resp.Parameters[0])
				if resp.NextCursor != "c2" || !resp.HasMore || !resp.Timestamp.Equal(psWhen) {
					t.Errorf("response = %+v", resp)
				}
			},
		},
		{
			name: "GetParameters",
			exp: mcpToolExpectation{
				wantTool:   "quicbaas_parameter_batch_get",
				assertArgs: assertPSArgs(map[string]interface{}{"paths": `["/app/db/host","/app/missing"]`, "with_decryption": "true"}),
				result: map[string]interface{}{
					"parameters": []interface{}{psResultParameter()},
					"errors":     map[string]interface{}{"/app/missing": "not found"},
					"timestamp":  psStamp,
				},
			},
			invoke: func(t *testing.T, c psclient.Client) {
				resp, err := c.GetParameters(ctx, &psclient.GetParametersRequest{
					Paths: []string{"/app/db/host", "/app/missing"}, WithDecryption: true,
				})
				if err != nil {
					t.Fatalf("GetParameters: %v", err)
				}
				if len(resp.Parameters) != 1 {
					t.Fatalf("Parameters = %d, want 1", len(resp.Parameters))
				}
				assertPSParameter(t, resp.Parameters[0])
				if resp.Errors["/app/missing"] != "not found" || !resp.Timestamp.Equal(psWhen) {
					t.Errorf("response = %+v", resp)
				}
			},
		},
		{
			name: "SetParameters",
			exp: mcpToolExpectation{
				wantTool: "quicbaas_parameter_batch_set",
				assertArgs: assertPSArgs(map[string]interface{}{
					"parameters": `[{"path":"/a","value":"1","type":"number","description":"d","kms_key_id":"k",` +
						`"tags":{"t":"v"},"ttl_seconds":60,"audit_enabled":true,"overwrite":true},{"path":"/b","value":"2"}]`,
				}),
				result: map[string]interface{}{
					"results": []interface{}{
						map[string]interface{}{"path": "/a", "created": true, "success": true},
						map[string]interface{}{"path": "/b", "created": false, "success": false, "error": "denied"},
					},
					"timestamp": psStamp,
				},
			},
			invoke: func(t *testing.T, c psclient.Client) {
				resp, err := c.SetParameters(ctx, &psclient.SetParametersRequest{Parameters: []*psclient.SetParameterRequest{
					{
						Path: "/a", Value: []byte("1"), Type: "number", Description: "d", KMSKeyID: "k",
						Tags: map[string]string{"t": "v"}, TTLSeconds: 60, AuditEnabled: true, Overwrite: true,
					},
					{Path: "/b", Value: []byte("2")},
				}})
				if err != nil {
					t.Fatalf("SetParameters: %v", err)
				}
				want := []*psclient.SetParameterResult{
					{Path: "/a", Created: true, Success: true},
					{Path: "/b", Error: "denied"},
				}
				if !reflect.DeepEqual(resp.Results, want) || !resp.Timestamp.Equal(psWhen) {
					t.Errorf("response = %+v", resp)
				}
			},
		},
		{
			name: "DeleteParameters",
			exp: mcpToolExpectation{
				wantTool:   "quicbaas_parameter_batch_delete",
				assertArgs: assertPSArgs(map[string]interface{}{"paths": `["/a","/b"]`}),
				result: map[string]interface{}{
					"results": []interface{}{
						map[string]interface{}{"path": "/a", "deleted": true},
						map[string]interface{}{"path": "/b", "deleted": false, "error": "not found"},
					},
					"deleted_count": 1,
					"timestamp":     psStamp,
				},
			},
			invoke: func(t *testing.T, c psclient.Client) {
				resp, err := c.DeleteParameters(ctx, &psclient.DeleteParametersRequest{Paths: []string{"/a", "/b"}})
				if err != nil {
					t.Fatalf("DeleteParameters: %v", err)
				}
				want := []*psclient.DeleteParameterResult{
					{Path: "/a", Deleted: true},
					{Path: "/b", Error: "not found"},
				}
				if !reflect.DeepEqual(resp.Results, want) || resp.DeletedCount != 1 || !resp.Timestamp.Equal(psWhen) {
					t.Errorf("response = %+v", resp)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, tt.exp)
			defer cleanup()
			tt.invoke(t, psclient.NewClient(tr))
		})
	}
}

// TestParameterStoreMCP_BadInput checks each verb refuses a request it cannot
// encode with a typed InvalidInput before any tool is called.
func TestParameterStoreMCP_BadInput(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_parameter_get",
		assertArgs: func(t *testing.T, _ map[string]interface{}) {
			t.Error("a refused request must not reach the tool")
		},
	})
	defer cleanup()

	notUTF8 := []byte{0xff, 0xfe}
	tests := []struct {
		method string
		req    interface{}
	}{
		{"GetParameter", &psclient.DeleteParameterRequest{Path: "/a"}},
		{"SetParameter", &psclient.SetParameterRequest{Path: "/a", Value: notUTF8}},
		{"DeleteParameter", (*psclient.DeleteParameterRequest)(nil)},
		{"ListParameters", "wrong"},
		{"SearchByTags", &psclient.ListParametersRequest{}},
		{"GetParameters", &psclient.DeleteParametersRequest{}},
		{"SetParameters", &psclient.SetParametersRequest{Parameters: []*psclient.SetParameterRequest{{Path: "/a", Value: notUTF8}}}},
		{"DeleteParameters", &psclient.GetParametersRequest{}},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			_, err := tr.Request(context.Background(), parameterStoreServiceName, tt.method, tt.req)
			if !sdkerrors.IsInvalidInput(err) {
				t.Errorf("err = %v, want InvalidInput", err)
			}
		})
	}
}

// TestParameterStoreMCP_UnknownMethod checks a method outside the closed tool
// set fails fast with a typed Unimplemented.
func TestParameterStoreMCP_UnknownMethod(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{wantTool: "quicbaas_parameter_get"})
	defer cleanup()

	_, err := tr.Request(context.Background(), parameterStoreServiceName, "RotateParameter", &psclient.GetParameterRequest{})
	if !sdkerrors.IsUnimplemented(err) {
		t.Errorf("err = %v, want Unimplemented", err)
	}
}

// TestParameterStoreMCP_OptionalArgsOmitted checks unset optional fields are
// left out so the server applies its defaults (limit 100, no cursor).
func TestParameterStoreMCP_OptionalArgsOmitted(t *testing.T) {
	args, err := listParametersArgs(&psclient.ListParametersRequest{Prefix: "/app/"})
	if err != nil {
		t.Fatalf("listParametersArgs: %v", err)
	}
	want := map[string]interface{}{
		"prefix": "/app/", "recursive": "false", "with_decryption": "false", "with_values": "false",
	}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
}

// TestParameterStoreMCP_ToolNames pins the method-to-tool map the contract
// test in pkg/api/mcp checks against the server registry.
func TestParameterStoreMCP_ToolNames(t *testing.T) {
	want := map[string]string{
		"GetParameter":     "quicbaas_parameter_get",
		"SetParameter":     "quicbaas_parameter_set",
		"DeleteParameter":  "quicbaas_parameter_delete",
		"ListParameters":   "quicbaas_parameter_list",
		"SearchByTags":     "quicbaas_parameter_search",
		"GetParameters":    "quicbaas_parameter_batch_get",
		"SetParameters":    "quicbaas_parameter_batch_set",
		"DeleteParameters": "quicbaas_parameter_batch_delete",
	}
	for method, tool := range want {
		if got := ToolName(parameterStoreServiceName, method); got != tool {
			t.Errorf("ToolName(%q) = %q, want %q", method, got, tool)
		}
	}
}
