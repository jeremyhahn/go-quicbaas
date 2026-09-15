//go:build openvino_live

package orchestration

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestOpenVINOBackend_Live drives a real OpenVINO Model Server. It is behind a
// build tag because it needs one: run with
//
//	go test -tags openvino_live -run TestOpenVINOBackend_Live ./pkg/orchestration/
//
// and OPENVINO_BASE_URL / OPENVINO_MODEL set.
func TestOpenVINOBackend_Live(t *testing.T) {
	base := os.Getenv("OPENVINO_BASE_URL")
	model := os.Getenv("OPENVINO_MODEL")
	if base == "" || model == "" {
		t.Skip("OPENVINO_BASE_URL and OPENVINO_MODEL are required")
	}
	b := NewOpenVINOBackend(OpenVINOBackendConfig{BaseURL: base, DefaultModel: model, RequestTimeout: 5 * time.Minute})
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	resp, err := b.Prompt(ctx, &PromptRequest{Prompt: "Reply with exactly: BACKEND_OK"})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if len(resp.Content) == 0 {
		t.Fatal("no content")
	}
	t.Logf("answer=%q usage=%+v", resp.Content[0].Text, resp.Usage)
}
