//go:build mcp

package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	stderrors "errors"
	"strings"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/qrdbnode"
	mcptransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// kvVersionProvider serves the package's shared in-memory qrdb node to the
// service. The node is not this test's to close.
type kvVersionProvider struct {
	client *qrdbsdk.Client
}

func (p *kvVersionProvider) ForShard(uint64) (*qrdbsdk.Client, error) { return p.client, nil }
func (p *kvVersionProvider) Close() error                             { return nil }

// kvVersionHarness drives the key-value tools of a real service backed by
// a real qrdb node, under one tenant and one entity type of its own.
type kvVersionHarness struct {
	t          *testing.T
	reg        *ToolRegistry
	ctx        context.Context
	entityType string
}

func newKVVersionRegistry(t *testing.T) *kvVersionHarness {
	t.Helper()
	svc, err := service.NewServiceBuilder().
		WithConfig(&service.ServiceConfig{NodeID: 1, RequestTimeout: 5 * time.Second}).
		WithRaftNode(&unusedRaftNode{}).
		WithQRDBClient(&kvVersionProvider{client: qrdbnode.Shared(t).Node.Client()}).
		Build()
	require.NoError(t, err)
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	require.NoError(t, err)
	entityType := strings.NewReplacer("/", "-", "_", "-").Replace("kvver-" + strings.ToLower(t.Name()))
	return &kvVersionHarness{
		t:          t,
		reg:        NewToolRegistry(svc, log),
		ctx:        tenantctx.WithTenant(t.Context(), "t1"),
		entityType: entityType,
	}
}

// args builds tool arguments for id; a nil expected omits expectedVersion.
func (h *kvVersionHarness) args(id uint64, data string, expected interface{}) map[string]interface{} {
	a := map[string]interface{}{"entityType": h.entityType, "id": float64(id)}
	if data != "" {
		a["data"] = base64.StdEncoding.EncodeToString([]byte(data))
	}
	if expected != nil {
		a["expectedVersion"] = expected
	}
	return a
}

func (h *kvVersionHarness) put(id uint64, data string, expected interface{}) (interface{}, error) {
	return h.reg.ExecuteTool(h.ctx, "quicbaas_kv_put", h.args(id, data, expected))
}

func (h *kvVersionHarness) del(id uint64, expected interface{}) (interface{}, error) {
	return h.reg.ExecuteTool(h.ctx, "quicbaas_kv_delete", h.args(id, "", expected))
}

// mustPut puts data and returns the version the tool reports.
func (h *kvVersionHarness) mustPut(id uint64, data string, expected interface{}) string {
	h.t.Helper()
	res, err := h.put(id, data, expected)
	require.NoError(h.t, err)
	m, ok := res.(map[string]interface{})
	require.True(h.t, ok, "put result is %T", res)
	return string(m["version"].(json.Number))
}

func (h *kvVersionHarness) mustDelete(id uint64, expected interface{}) {
	h.t.Helper()
	_, err := h.del(id, expected)
	require.NoError(h.t, err)
}

// requireData asserts get returns want for id.
func (h *kvVersionHarness) requireData(id uint64, want string) {
	h.t.Helper()
	res, err := h.reg.ExecuteTool(h.ctx, "quicbaas_kv_get", h.args(id, "", nil))
	require.NoError(h.t, err)
	m, ok := res.(map[string]interface{})
	require.True(h.t, ok, "get result is %T", res)
	got, err := base64.StdEncoding.DecodeString(m["data"].(string))
	require.NoError(h.t, err)
	require.Equal(h.t, want, string(got))
}

// requireNotFound asserts get reports id as not found.
func (h *kvVersionHarness) requireNotFound(id uint64) {
	h.t.Helper()
	_, err := h.reg.ExecuteTool(h.ctx, "quicbaas_kv_get", h.args(id, "", nil))
	require.Error(h.t, err)
	result := ToolErrorResult(nil, "quicbaas_kv_get", err)
	require.Equal(h.t, mcptransport.ToolError{Code: mcptransport.MCPResourceNotFound}, result.StructuredContent)
}

// requireConflict asserts err is a version conflict surfaced as the
// resource-conflict tool error.
func requireConflict(t *testing.T, tool string, err error) {
	t.Helper()
	require.Error(t, err)
	result := ToolErrorResult(nil, tool, err)
	require.True(t, result.IsError)
	require.Equal(t, mcptransport.ToolError{Code: mcptransport.MCPResourceConflict}, result.StructuredContent)
	require.True(t, stderrors.As(err, new(*service.KVVersionConflictError)), "want a *service.KVVersionConflictError, got %v", err)
}

func TestKVExpectedVersion_MatchingVersionWrites(t *testing.T) {
	h := newKVVersionRegistry(t)
	const id = 1

	h.mustPut(id, "v1", nil)
	require.Equal(t, "2", h.mustPut(id, "v2", float64(1)))
	require.Equal(t, "3", h.mustPut(id, "v3", float64(2)))
	h.requireData(id, "v3")

	h.mustDelete(id, float64(3))
	h.requireNotFound(id)

	require.Equal(t, "1", h.mustPut(id, "v4", float64(0)), "expectedVersion 0 creates an absent key")
	h.requireData(id, "v4")
}

func TestKVExpectedVersion_StalePutConflictsAndChangesNothing(t *testing.T) {
	h := newKVVersionRegistry(t)
	const id = 2

	h.mustPut(id, "first", nil)
	h.mustPut(id, "second", float64(1))

	_, err := h.put(id, "stale", float64(1))
	requireConflict(t, "quicbaas_kv_put", err)
	h.requireData(id, "second")
}

func TestKVExpectedVersion_StaleDeleteConflictsAndKeepsKey(t *testing.T) {
	h := newKVVersionRegistry(t)
	const id = 3

	h.mustPut(id, "first", nil)
	h.mustPut(id, "second", nil)

	_, err := h.del(id, float64(1))
	requireConflict(t, "quicbaas_kv_delete", err)
	h.requireData(id, "second")
}

func TestKVExpectedVersion_OmittedKeepsUnconditionalWrites(t *testing.T) {
	h := newKVVersionRegistry(t)
	const id = 4

	h.mustPut(id, "first", nil)
	h.mustPut(id, "second", nil)
	h.requireData(id, "second")
	h.mustDelete(id, nil)
	h.requireNotFound(id)
}

func TestKVExpectedVersion_AbsentKeyNonZeroConflicts(t *testing.T) {
	h := newKVVersionRegistry(t)
	const id = 5

	_, err := h.put(id, "never", float64(1))
	requireConflict(t, "quicbaas_kv_put", err)
	h.requireNotFound(id)

	_, err = h.del(id, float64(1))
	requireConflict(t, "quicbaas_kv_delete", err)
	h.requireNotFound(id)
}

func TestKVExpectedVersion_ArgParsing(t *testing.T) {
	type call struct {
		reached  bool
		expected *uint64
	}
	newRegistry := func(t *testing.T, c *call) *ToolRegistry {
		t.Helper()
		svc := &mockService{}
		svc.putFunc = func(_ context.Context, req *service.PutRequest) (*service.PutResponse, error) {
			c.reached, c.expected = true, req.ExpectedVersion
			return &service.PutResponse{ID: req.ID}, nil
		}
		svc.deleteFunc = func(_ context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
			c.reached, c.expected = true, req.ExpectedVersion
			return &service.DeleteResponse{}, nil
		}
		log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
		require.NoError(t, err)
		return NewToolRegistry(svc, log)
	}
	argsFor := func(tool string, expected interface{}, set bool) map[string]interface{} {
		a := map[string]interface{}{"entityType": "users", "id": float64(7)}
		if strings.HasSuffix(tool, "put") {
			a["data"] = base64.StdEncoding.EncodeToString([]byte("x"))
		}
		if set {
			a["expectedVersion"] = expected
		}
		return a
	}
	u := func(v uint64) *uint64 { return &v }

	accepted := map[string]struct {
		value interface{}
		set   bool
		want  *uint64
	}{
		"omitted":     {set: false, want: nil},
		"null":        {value: nil, set: true, want: nil},
		"float 3":     {value: float64(3), set: true, want: u(3)},
		"string 3":    {value: "3", set: true, want: u(3)},
		"json number": {value: json.Number("3"), set: true, want: u(3)},
		"float 0":     {value: float64(0), set: true, want: u(0)},
	}
	refused := map[string]interface{}{
		"fractional":   float64(1.5),
		"negative":     float64(-1),
		"empty string": "",
		"not a number": "x",
		"bool":         true,
		"array":        []interface{}{},
	}

	for _, tool := range []string{"quicbaas_kv_put", "quicbaas_kv_delete"} {
		for name, tc := range accepted {
			t.Run(tool+"/"+name, func(t *testing.T) {
				var c call
				_, err := newRegistry(t, &c).ExecuteTool(context.Background(), tool, argsFor(tool, tc.value, tc.set))
				require.NoError(t, err)
				require.True(t, c.reached)
				require.Equal(t, tc.want, c.expected)
			})
		}
		for name, value := range refused {
			t.Run(tool+"/refuses "+name, func(t *testing.T) {
				var c call
				_, err := newRegistry(t, &c).ExecuteTool(context.Background(), tool, argsFor(tool, value, true))
				require.Error(t, err)
				result := ToolErrorResult(nil, tool, err)
				require.Equal(t, mcptransport.ToolError{Code: mcptransport.MCPInvalidInput}, result.StructuredContent)
				require.False(t, c.reached, "the service was called for expectedVersion %v", value)
			})
		}
	}
}

func TestKVExpectedVersion_SchemaDeclaresArgument(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	require.NoError(t, err)
	reg := NewToolRegistry(&mockService{}, log)

	want := map[string]bool{"qrdb_put": false, "quicbaas_kv_put": false, "qrdb_delete": false, "quicbaas_kv_delete": false}
	for _, tool := range reg.ListTools() {
		if _, ok := want[tool.Name]; !ok {
			continue
		}
		want[tool.Name] = true
		props, _ := tool.InputSchema["properties"].(map[string]interface{})
		require.Contains(t, props, "expectedVersion", "%s schema does not declare expectedVersion", tool.Name)
		required, _ := tool.InputSchema["required"].([]string)
		require.NotContains(t, required, "expectedVersion", "%s requires expectedVersion", tool.Name)
	}
	for name, seen := range want {
		require.True(t, seen, "tool %s is not registered", name)
	}
}
