//go:build rest

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

package rest

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSDLCBinding_PausedQueryLeavesAutonomyOff: ?paused=true reaches the
// service, so the project is bound but its autonomy stays off; a later plain
// bind still switches it on, because paused is the request's, not stored.
func TestSDLCBinding_PausedQueryLeavesAutonomyOff(t *testing.T) {
	server, _ := setupSDLCServer(t)
	seedBindableProject("p1")

	rec := sdlcCall(t, server, "tenant-a", http.MethodPut, "/api/v1/sdlc/projects/p1/binding?paused=true", restBinding("p1"))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.False(t, sdlcRESTProjectRecords["p1"].Autonomous, "a paused bind turned autonomy on")

	rec = sdlcCall(t, server, "tenant-a", http.MethodPut, "/api/v1/sdlc/projects/p1/binding", restBinding("p1"))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.True(t, sdlcRESTProjectRecords["p1"].Autonomous, "a plain bind left autonomy off")
}

// TestSDLCBinding_MalformedPausedIsRefused: a paused value that is not a
// boolean is a 400 and binds nothing.
func TestSDLCBinding_MalformedPausedIsRefused(t *testing.T) {
	server, _ := setupSDLCServer(t)
	seedBindableProject("p1")

	rec := sdlcCall(t, server, "tenant-a", http.MethodPut, "/api/v1/sdlc/projects/p1/binding?paused=nope", restBinding("p1"))
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	rec = sdlcCall(t, server, "tenant-a", http.MethodGet, "/api/v1/sdlc/projects/p1/binding", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

// TestProjectREST_CreateKeepsAutonomyOff pins the non-defect #1041 suspected:
// creating a project with autonomous false, or without the field, stores it
// non-autonomous. Only Bind ever forced autonomy on.
func TestProjectREST_CreateKeepsAutonomyOff(t *testing.T) {
	h := storageRESTRouter()
	for name, tc := range map[string]struct {
		body string
		want bool
	}{
		"explicit false": {body: `{"tenant_id": "tenant-1", "name": "off", "autonomous": false}`, want: false},
		"omitted":        {body: `{"tenant_id": "tenant-1", "name": "unset"}`, want: false},
		"explicit true":  {body: `{"tenant_id": "tenant-1", "name": "on", "autonomous": true}`, want: true},
	} {
		t.Run(name, func(t *testing.T) {
			code, body := doStorageREST(t, h, http.MethodPost, "/projects", tc.body)
			require.Equal(t, http.StatusCreated, code, string(body))
			var id string
			require.NoError(t, json.Unmarshal(flatProject(t, body)["id"], &id))

			code, body = doStorageREST(t, h, http.MethodGet, "/projects/"+id+"?tenant_id=tenant-1", "")
			require.Equal(t, http.StatusOK, code, string(body))
			var read struct {
				Project struct {
					Autonomous bool `json:"autonomous"`
				} `json:"project"`
			}
			require.NoError(t, json.Unmarshal(body, &read))
			assert.Equal(t, tc.want, read.Project.Autonomous)
		})
	}
}
