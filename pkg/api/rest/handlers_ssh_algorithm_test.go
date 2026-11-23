package rest

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func sshGenerateKeyWithAlgorithm(t *testing.T, server *Server, name, algorithm string) {
	t.Helper()
	body := `{"scope":"user","owner":"alice","name":"` + name + `","algorithm":"` + algorithm + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/keys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.HandleGenerateSSHKey(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "gen body=%s", w.Body.String())
}

func signSSHDigestWithAlgorithm(server *Server, name, algorithm string) *httptest.ResponseRecorder {
	digest := base64.StdEncoding.EncodeToString(make([]byte, 32))
	body := `{"digest":"` + digest + `","algorithm":"` + algorithm + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/keys/user/alice/"+name+"/sign",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withChiURLParams(req, map[string]string{"scope": "user", "owner": "alice", "name": name})
	w := httptest.NewRecorder()
	server.HandleSignSSHDigest(w, req)
	return w
}

func TestSSHSignDigest_RSAKeySignsWithRequestedSHA2Algorithm(t *testing.T) {
	t.Parallel()
	server, _, cleanup := newSSHTestServer(t)
	defer cleanup()
	sshGenerateKeyWithAlgorithm(t, server, "rsa", "rsa")

	for _, alg := range []string{"rsa-sha2-256", "rsa-sha2-512"} {
		w := signSSHDigestWithAlgorithm(server, "rsa", alg)
		require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
		var resp sshSignResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		require.Equal(t, alg, resp.Format)
	}
}

func TestSSHSignDigest_AlgorithmForOtherKeyTypeIsBadRequest(t *testing.T) {
	t.Parallel()
	server, _, cleanup := newSSHTestServer(t)
	defer cleanup()
	sshGenerateKeyWithAlgorithm(t, server, "laptop", "ed25519")

	w := signSSHDigestWithAlgorithm(server, "laptop", "rsa-sha2-256")
	require.Equal(t, http.StatusBadRequest, w.Code, "body=%s", w.Body.String())
}
