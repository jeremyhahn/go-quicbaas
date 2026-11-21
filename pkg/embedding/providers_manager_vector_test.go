//go:build vector && qrdb_vector

package embedding

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The SPEC row `embeddings` names four providers: Ollama, OpenAI, Voyage and
// LM Studio. Each one, configured as the Manager's default provider under its
// own name and registered against a local HTTP server, embeds a text and a
// batch through the Manager, the path every consumer takes.
func TestManager_EachSpecProviderAsDefault(t *testing.T) {
	cases := []struct {
		name string
		dims int
		new  func(t *testing.T, url string) Provider
	}{
		{
			name: "ollama",
			dims: 5,
			new: func(t *testing.T, url string) Provider {
				cfg := DefaultOllamaConfig()
				cfg.BaseURL = url
				cfg.MaxRetries = 0
				p, err := NewOllamaProvider(cfg)
				require.NoError(t, err)
				return p
			},
		},
		{
			name: "openai",
			dims: 4,
			new: func(t *testing.T, url string) Provider {
				p, err := NewOpenAIProvider(Config{APIURL: url, APIKey: "sk-test", Model: "custom-model", MaxRetries: 0})
				require.NoError(t, err)
				return p
			},
		},
		{
			name: "voyage",
			dims: 8,
			new: func(t *testing.T, url string) Provider {
				p, err := NewVoyageProvider(Config{APIURL: url, APIKey: "voyage-key", Model: "voyage-code-2", MaxRetries: 0})
				require.NoError(t, err)
				return p
			},
		},
		{
			name: "lmstudio",
			dims: 6,
			new: func(t *testing.T, url string) Provider {
				cfg := DefaultLMStudioConfig()
				cfg.BaseURL = url
				cfg.MaxRetries = 0
				p, err := NewLMStudioProvider(cfg)
				require.NoError(t, err)
				return p
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var url string
			if tc.name == "voyage" {
				url = voyageHandler(t, tc.dims, nil).URL
			} else {
				url = openAIEmbeddingHandler(t, tc.dims, nil).URL
			}
			p := tc.new(t, url)
			t.Cleanup(func() { _ = p.Close() })
			assert.Equal(t, tc.name, p.Name())

			cfg := validManagerConfig()
			cfg.Provider = tc.name
			// Unreachable on purpose: a vector back proves the registered
			// provider (pointed at the test server) served the call.
			cfg.APIURL = "http://127.0.0.1:1"
			cfg.APIKey = "key"
			cfg.Dimensions = tc.dims
			m, err := NewManager(cfg)
			require.NoError(t, err, "go-qrdb accepts %q as the configured default", tc.name)
			t.Cleanup(func() { _ = m.Close() })
			require.NoError(t, m.RegisterProvider(tc.name, p))

			vec, err := m.Embed(context.Background(), "hello")
			require.NoError(t, err)
			assert.Len(t, vec, tc.dims)

			batch, err := m.EmbedBatch(context.Background(), []string{"a", "b"})
			require.NoError(t, err)
			require.Len(t, batch, 2)
			assert.Len(t, batch[0], tc.dims)
		})
	}
}
