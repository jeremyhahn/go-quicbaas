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

//go:build integration && rag

package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/ollama"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// requireOllama skips the test if Ollama is not available.
func requireOllama(t *testing.T) {
	t.Helper()
	if !env.ollamaAvailable {
		t.Skipf("Ollama service not available at %s", env.ollamaURL)
	}
}

// createTestProject creates a directory at the given path with a CLAUDE.md
// file, simulating an agent-enabled project for discovery tests. The path
// must be an absolute directory path (e.g., filepath.Join(root, "my-project")).
func createTestProject(t *testing.T, path string) string {
	t.Helper()

	require.NoError(t, os.MkdirAll(path, 0o755))

	claudeMD := filepath.Join(path, "CLAUDE.md")
	name := filepath.Base(path)
	content := "# " + name + "\n\nTest project for agent integration tests.\n"
	require.NoError(t, os.WriteFile(claudeMD, []byte(content), 0o644))

	return path
}

// createTestProjectWithAgents creates a directory at the given path with a
// CLAUDE.md file and a .claude/agents/ directory containing agent definition
// files.
func createTestProjectWithAgents(t *testing.T, path string, agentNames []string) string {
	t.Helper()

	dir := createTestProject(t, path)

	agentsDir := filepath.Join(dir, ".claude", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))

	for _, agentName := range agentNames {
		agentFile := filepath.Join(agentsDir, agentName+".md")
		content := "# " + agentName + "\n\nAgent definition for testing.\n"
		require.NoError(t, os.WriteFile(agentFile, []byte(content), 0o644))
	}

	return dir
}

// newOllamaBackend creates an Ollama backend configured to use the test
// environment's Ollama instance.
func newOllamaBackend(t *testing.T) *orchestration.OllamaBackend {
	t.Helper()

	client := ollama.NewClient(ollama.ClientConfig{
		BaseURL:    env.ollamaURL,
		MaxRetries: 2,
	})
	chatSvc := ollama.NewChatService(client, env.ollamaModel)

	return orchestration.NewOllamaBackend(orchestration.OllamaBackendConfig{
		ChatService:  chatSvc,
		DefaultModel: env.ollamaModel,
	})
}

// newAgentService creates a fully wired AgentService with an in-memory store
// and an Ollama backend registered under the name "ollama".
func newAgentService(t *testing.T) orchestration.AgentService {
	t.Helper()

	store := orchestration.NewInMemoryAgentStore()
	backend := newOllamaBackend(t)

	backends := map[string]orchestration.AgentBackend{
		"ollama": backend,
	}

	svc, err := orchestration.NewAgentService(store, backends, &testLogger{t: t})
	require.NoError(t, err, "failed to create agent service")

	return svc
}

// testLogger implements interfaces.Logger and forwards output to testing.T.
type testLogger struct {
	t *testing.T
}

// Compile-time interface satisfaction check.
var _ interfaces.Logger = (*testLogger)(nil)

func (l *testLogger) Debug(msg string) { l.t.Helper(); l.t.Log("[DEBUG] " + msg) }
func (l *testLogger) Debugf(format string, args ...interface{}) {
	l.t.Helper()
	l.t.Logf("[DEBUG] "+format, args...)
}
func (l *testLogger) Info(msg string) { l.t.Helper(); l.t.Log("[INFO] " + msg) }
func (l *testLogger) Infof(format string, args ...interface{}) {
	l.t.Helper()
	l.t.Logf("[INFO] "+format, args...)
}
func (l *testLogger) Warn(msg string) { l.t.Helper(); l.t.Log("[WARN] " + msg) }
func (l *testLogger) Warnf(format string, args ...interface{}) {
	l.t.Helper()
	l.t.Logf("[WARN] "+format, args...)
}
func (l *testLogger) Error(msg string) { l.t.Helper(); l.t.Log("[ERROR] " + msg) }
func (l *testLogger) Errorf(format string, args ...interface{}) {
	l.t.Helper()
	l.t.Logf("[ERROR] "+format, args...)
}
func (l *testLogger) Fatal(msg string) { l.t.Helper(); l.t.Fatal("[FATAL] " + msg) }
func (l *testLogger) Fatalf(format string, args ...interface{}) {
	l.t.Helper()
	l.t.Fatalf("[FATAL] "+format, args...)
}

func (l *testLogger) WithField(key string, value interface{}) interfaces.Logger  { return l }
func (l *testLogger) WithFields(fields map[string]interface{}) interfaces.Logger { return l }
func (l *testLogger) WithError(err error) interfaces.Logger                      { return l }
