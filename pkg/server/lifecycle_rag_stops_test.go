//go:build rag && raft && !integration

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// lifecycle_rag.go stop methods — nil guard paths
// ---------------------------------------------------------------------------

func TestLifecycle_StopRAG_Nil(t *testing.T) {
	t.Run("nil RAG system returns nil", func(t *testing.T) {
		srv := withTempServer(t)
		srv.ragSystem = nil
		err := srv.stopRAG()
		assert.NoError(t, err)
	})
}

func TestLifecycle_StopOllama_Nil(t *testing.T) {
	t.Run("nil ollama service returns nil", func(t *testing.T) {
		srv := withTempServer(t)
		srv.ollamaService = nil
		err := srv.stopOllama()
		assert.NoError(t, err)
	})
}

func TestLifecycle_StartRAG_Disabled(t *testing.T) {
	t.Run("skips when RAG is disabled", func(t *testing.T) {
		srv := withTempServer(t)
		srv.config.RAG.Enabled = false
		err := srv.startRAG()
		assert.NoError(t, err)
	})
}

func TestLifecycle_StartOllama_Disabled(t *testing.T) {
	t.Run("skips when Ollama is disabled", func(t *testing.T) {
		srv := withTempServer(t)
		srv.config.Ollama.Enabled = false
		err := srv.startOllama()
		assert.NoError(t, err)
	})
}
