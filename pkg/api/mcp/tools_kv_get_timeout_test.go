//go:build mcp

package mcp

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// getTimeoutRecorder returns a registry whose Get records the TimeoutMs each
// call reaches the service with.
func getTimeoutRecorder(t *testing.T) (*ToolRegistry, *[]uint64) {
	t.Helper()
	rec := newKVIDRecorder()
	timeouts := []uint64{}
	rec.svc.getFunc = func(_ context.Context, req *service.GetRequest) (*service.GetResponse, error) {
		timeouts = append(timeouts, req.TimeoutMs)
		return &service.GetResponse{}, nil
	}
	return rec.registry(t), &timeouts
}

func TestEntityKVGet_RequestedTimeoutReachesService(t *testing.T) {
	cases := map[string]struct {
		args map[string]interface{}
		want uint64
	}{
		"timeout_ms number":    {map[string]interface{}{"timeout_ms": float64(10)}, 10},
		"timeout_ms above 5s":  {map[string]interface{}{"timeout_ms": float64(30000)}, 30000},
		"timeout_ms decimal":   {map[string]interface{}{"timeout_ms": "250"}, 250},
		"timeoutMs alias":      {map[string]interface{}{"timeoutMs": float64(10)}, 10},
		"absent means default": {map[string]interface{}{}, 0},
	}
	for _, tool := range []string{"qrdb_get", kvEntityToolPrefix + "get"} {
		for name, tc := range cases {
			t.Run(tool+"/"+name, func(t *testing.T) {
				registry, timeouts := getTimeoutRecorder(t)
				args := map[string]interface{}{"entityType": "users", "id": float64(7)}
				for k, v := range tc.args {
					args[k] = v
				}
				if _, err := registry.ExecuteTool(context.Background(), tool, args); err != nil {
					t.Fatalf("%s: %v", tool, err)
				}
				if len(*timeouts) != 1 || (*timeouts)[0] != tc.want {
					t.Fatalf("service saw timeouts %v, want [%d]", *timeouts, tc.want)
				}
			})
		}
	}
}

func TestEntityKVGet_RefusesMalformedTimeout(t *testing.T) {
	for name, timeout := range map[string]interface{}{
		"negative":         float64(-1),
		"not a number":     "soon",
		"overflows uint64": "18446744073709551616",
	} {
		t.Run(name, func(t *testing.T) {
			registry, timeouts := getTimeoutRecorder(t)
			args := map[string]interface{}{"entityType": "users", "id": float64(7), "timeout_ms": timeout}
			if _, err := registry.executeGet(context.Background(), args); err == nil {
				t.Fatalf("timeout_ms %v: want an error, got none", timeout)
			}
			if len(*timeouts) != 0 {
				t.Fatalf("timeout_ms %v: service was called with %v", timeout, *timeouts)
			}
		})
	}
}

func TestEntityKVGet_SchemaAdvertisesTimeout(t *testing.T) {
	registry, _ := getTimeoutRecorder(t)
	want := map[string]bool{"qrdb_get": false, kvEntityToolPrefix + "get": false}
	for _, tool := range registry.ListTools() {
		if _, ok := want[tool.Name]; !ok {
			continue
		}
		props, _ := tool.InputSchema["properties"].(map[string]interface{})
		if _, ok := props["timeout_ms"]; !ok {
			t.Errorf("%s schema does not advertise timeout_ms", tool.Name)
		}
		want[tool.Name] = true
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("%s is not registered", name)
		}
	}
}
