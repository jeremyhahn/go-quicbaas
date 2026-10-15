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

package main

import (
	"bytes"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	accountSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/page"
	projectSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	sdktransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	resttransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/rest"
)

// =============================================================================
// pageFlags / fetchPages / writePageFooter
// =============================================================================

// TestPageFlags_BindParsesIntoParams proves the shared flags land in the
// page.Params the SDK request embeds.
func TestPageFlags_BindParsesIntoParams(t *testing.T) {
	var f pageFlags
	cmd := &cobra.Command{Use: "list"}
	f.bind(cmd)

	require.NoError(t, cmd.ParseFlags([]string{"--limit", "10", "--offset", "20", "--sort", "name:desc", "--search", "web", "--all"}))

	assert.Equal(t, page.Params{Limit: 10, Offset: 20, Sort: "name:desc", Query: "web"}, f.Params)
	assert.True(t, f.All)
}

// TestPageFlags_BindRejectsNonNumericLimit proves a malformed --limit is a
// flag-parse error, not a silently ignored value.
func TestPageFlags_BindRejectsNonNumericLimit(t *testing.T) {
	var f pageFlags
	cmd := &cobra.Command{Use: "list"}
	f.bind(cmd)

	require.Error(t, cmd.ParseFlags([]string{"--limit", "many"}))
}

func TestFetchPages_OnePageSendsTheFlagsAsGiven(t *testing.T) {
	f := &pageFlags{Params: page.Params{Limit: 5, Offset: 10, Sort: "name", Query: "x"}}
	var got []page.Params

	records, env, err := fetchPages(f, func(p page.Params) ([]int, page.Envelope, error) {
		got = append(got, p)
		return []int{1, 2}, page.Envelope{Total: 40, Limit: 5, Offset: 10, NextOffset: 15}, nil
	})

	require.NoError(t, err)
	assert.Equal(t, []page.Params{f.Params}, got, "without --all exactly one page is fetched")
	assert.Equal(t, []int{1, 2}, records)
	assert.Equal(t, 15, env.NextOffset, "a single page keeps the server's next_offset")
}

func TestFetchPages_AllWalksToTheEndAtTheMaximumPageSize(t *testing.T) {
	f := &pageFlags{Params: page.Params{Offset: 1, Sort: "name"}, All: true}
	var got []page.Params

	records, env, err := fetchPages(f, func(p page.Params) ([]int, page.Envelope, error) {
		got = append(got, p)
		env := page.Envelope{Total: 5, Limit: 2, Offset: p.Offset}
		if p.Offset+2 < 5 {
			env.NextOffset = p.Offset + 2
		}
		return []int{p.Offset, p.Offset + 1}[:min(2, 5-p.Offset)], env, nil
	})

	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, allPageSize, got[0].Limit, "--all without --limit asks for the server's maximum page")
	assert.Equal(t, 1, got[0].Offset)
	assert.Equal(t, 3, got[1].Offset, "the walk follows next_offset")
	assert.Equal(t, "name", got[1].Sort, "every page keeps the sort")
	assert.Equal(t, []int{1, 2, 3, 4}, records)
	assert.Equal(t, page.Envelope{Total: 5, Limit: 2, Offset: 1}, env, "the walk's envelope starts at the first page and names no next page")
}

func TestFetchPages_AllRefusesANextOffsetThatDoesNotAdvance(t *testing.T) {
	f := &pageFlags{All: true}

	_, _, err := fetchPages(f, func(p page.Params) ([]int, page.Envelope, error) {
		return []int{1}, page.Envelope{Total: 9, Offset: 4, NextOffset: 4}, nil
	})

	var stalled *ListPageStalledError
	require.ErrorAs(t, err, &stalled)
	assert.Equal(t, 4, stalled.NextOffset)
}

func TestFetchPages_AllStopsOnAPageError(t *testing.T) {
	f := &pageFlags{All: true}
	boom := stderrors.New("page two failed")
	calls := 0

	_, _, err := fetchPages(f, func(p page.Params) ([]int, page.Envelope, error) {
		calls++
		if calls == 2 {
			return nil, page.Envelope{}, boom
		}
		return []int{1}, page.Envelope{Total: 9, NextOffset: 1}, nil
	})

	require.ErrorIs(t, err, boom)
	assert.Equal(t, 2, calls)
}

func TestFetchPages_RefusesNegativeFlagsBeforeFetching(t *testing.T) {
	for _, f := range []*pageFlags{
		{Params: page.Params{Limit: -1}},
		{Params: page.Params{Offset: -3}},
	} {
		_, _, err := fetchPages(f, func(page.Params) ([]int, page.Envelope, error) {
			t.Fatal("fetch must not run for a negative flag")
			return nil, page.Envelope{}, nil
		})
		var flagErr *ListPageFlagError
		require.ErrorAs(t, err, &flagErr)
		assert.Contains(t, flagErr.Error(), "must not be negative")
	}
}

func TestWritePageFooter(t *testing.T) {
	cases := []struct {
		name  string
		shown int
		env   page.Envelope
		want  string
	}{
		{"middle page", 25, page.Envelope{Total: 132, Offset: 25, NextOffset: 50},
			"Showing 26–50 of 132. Next page: --offset 50 (or --all for every page).\n"},
		{"last page", 7, page.Envelope{Total: 132, Offset: 125}, "Showing 126–132 of 132.\n"},
		{"past the end", 0, page.Envelope{Total: 3, Offset: 40}, "Showing 0 of 3: offset 40 is past the end.\n"},
		{"nothing matched", 0, page.Envelope{}, ""},
		{"no envelope on the response", 2, page.Envelope{}, "Showing 1–2.\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			writePageFooter(&buf, tc.shown, tc.env)
			assert.Equal(t, tc.want, buf.String())
		})
	}
}

// =============================================================================
// Every list command, end to end over the REST transport
// =============================================================================

// pagedListServer is a REST server for one list endpoint that pages a fixed
// set of records the way the real handlers do: limit/offset cut the page, q
// narrows by substring, and a sort field other than "name" is a 400.
type pagedListServer struct {
	*httptest.Server

	mu      sync.Mutex
	queries []url.Values
}

// requests returns the query strings the server has received, in order.
func (s *pagedListServer) requests() []url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]url.Values(nil), s.queries...)
}

func newPagedListServer(t *testing.T, path, key, idField string, total int) *pagedListServer {
	t.Helper()
	s := &pagedListServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != path {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, `{"error":"unexpected path %s"}`, r.URL.Path)
			return
		}
		q := r.URL.Query()
		s.mu.Lock()
		s.queries = append(s.queries, q)
		s.mu.Unlock()

		if sort := q.Get("sort"); sort != "" && strings.TrimSuffix(sort, ":desc") != "name" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"error":"unknown sort field %q","code":"invalid_input"}`, sort)
			return
		}

		var ids []string
		for i := 0; i < total; i++ {
			id := fmt.Sprintf("rec-%02d", i)
			if strings.Contains(id, q.Get("q")) {
				ids = append(ids, id)
			}
		}
		limit, offset := 25, 0
		if v := q.Get("limit"); v != "" {
			limit, _ = strconv.Atoi(v)
		}
		if v := q.Get("offset"); v != "" {
			offset, _ = strconv.Atoi(v)
		}
		end := min(offset+limit, len(ids))
		records := []map[string]interface{}{}
		for _, id := range ids[min(offset, len(ids)):end] {
			records = append(records, map[string]interface{}{idField: id, "name": id})
		}
		body := map[string]interface{}{key: records, "total": len(ids), "limit": limit, "offset": offset}
		if end < len(ids) {
			body["next_offset"] = end
		}
		require.NoError(t, json.NewEncoder(w).Encode(body))
	}))
	t.Cleanup(s.Close)
	return s
}

// newTestRESTTransport builds a real SDK REST transport against server.
func newTestRESTTransport(t *testing.T, server *pagedListServer) *resttransport.Transport {
	t.Helper()
	tr, err := resttransport.New(&sdktransport.Config{Address: server.URL})
	require.NoError(t, err)
	t.Cleanup(func() { tr.Close() })
	return tr
}

// listCommandCase is one CLI list command and the endpoint it reads.
type listCommandCase struct {
	name    string
	path    string
	key     string
	idField string
	newCmd  func() *cobra.Command
	// install points the command's client factory at server and its output
	// flag at format, restoring both when the test ends.
	install func(t *testing.T, server *pagedListServer, format string)
}

func listCommandCases() []listCommandCase {
	orchestrator := func(t *testing.T, server *pagedListServer, format string) {
		saveAndRestoreOrchestratorState(t)
		orchOutputFmt = format
		orchestratorClientFactory = func() (OrchestratorClient, error) {
			return newRESTOrchestratorClient(t, server.Server), nil
		}
	}
	return []listCommandCase{
		{
			name: "orchestrator job list", path: "/api/v1/orchestrator/jobs", key: "jobs", idField: "job_id",
			newCmd: newOrchJobListCommand, install: orchestrator,
		},
		{
			name: "orchestrator task list", path: "/api/v1/orchestrator/tasks", key: "tasks", idField: "id",
			newCmd: newOrchTaskListCommand, install: orchestrator,
		},
		{
			name: "orchestrator sessions", path: "/api/v1/orchestrator/sessions", key: "sessions", idField: "session_id",
			newCmd: newOrchSessionsCommand, install: orchestrator,
		},
		{
			name: "fleet list", path: "/api/v1/fleet", key: "nodes", idField: "id",
			newCmd: newFleetListCommand,
			install: func(t *testing.T, server *pagedListServer, format string) {
				saveAndRestoreOrchestratorState(t)
				orig := fleetOutputFmt
				t.Cleanup(func() { fleetOutputFmt = orig })
				fleetOutputFmt = format
				orchestratorClientFactory = func() (OrchestratorClient, error) {
					return newRESTOrchestratorClient(t, server.Server), nil
				}
			},
		},
		{
			name: "project list", path: "/api/v1/projects", key: "projects", idField: "id",
			newCmd: newProjectListCommand,
			install: func(t *testing.T, server *pagedListServer, format string) {
				withMockProject(t, nil)
				projectOutputFmt = format
				tr := newTestRESTTransport(t, server)
				projectClientFactory = func() (projectSDK.Client, error) { return projectSDK.NewClient(tr), nil }
			},
		},
		{
			name: "agent account list", path: "/api/v1/agent-accounts", key: "accounts", idField: "id",
			newCmd: newAccountListCommand,
			install: func(t *testing.T, server *pagedListServer, format string) {
				origFactory, origFmt, origTenant, origTimeout := accountClientFactory, accountOutputFmt, globalTenantID, globalTimeout
				t.Cleanup(func() {
					accountClientFactory, accountOutputFmt, globalTenantID, globalTimeout = origFactory, origFmt, origTenant, origTimeout
				})
				accountOutputFmt = format
				tr := newTestRESTTransport(t, server)
				accountClientFactory = func() (accountSDK.Client, error) { return accountSDK.NewClient(tr), nil }
			},
		},
	}
}

// execListCommand installs tc against a server holding total records, runs
// the command with args, and returns what it printed and the error.
// restoreListPageFlags puts every list command's page flags back when the
// test ends. ParseFlags writes them into package-level variables, so a
// --limit -1 left behind would be refused by the next test, or the next
// -count run, that calls the command's RunE without binding flags.
func restoreListPageFlags(t *testing.T) {
	t.Helper()
	pages := []*pageFlags{
		&orchJobListPage, &orchTaskListPage, &orchSessionsPage,
		&fleetListPage, &projectListPage, &accountListPage,
	}
	saved := make([]pageFlags, len(pages))
	for i, p := range pages {
		saved[i] = *p
	}
	t.Cleanup(func() {
		for i, p := range pages {
			*p = saved[i]
		}
	})
}

func execListCommand(t *testing.T, tc listCommandCase, total int, format string, args ...string) (*pagedListServer, string, error) {
	t.Helper()
	server := newPagedListServer(t, tc.path, tc.key, tc.idField, total)
	tc.install(t, server, format)
	restoreListPageFlags(t)
	globalTenantID = "acme"
	globalTimeout = 5 * time.Second

	cmd := tc.newCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.ParseFlags(args))
	err := cmd.RunE(cmd, nil)
	return server, out.String(), err
}

func TestListCommands_FlagsReachTheWireAndTheFooterRenders(t *testing.T) {
	for _, tc := range listCommandCases() {
		t.Run(tc.name, func(t *testing.T) {
			server, out, err := execListCommand(t, tc, 7, "table",
				"--limit", "2", "--offset", "2", "--sort", "name:desc", "--search", "rec")
			require.NoError(t, err)

			reqs := server.requests()
			require.Len(t, reqs, 1)
			assert.Equal(t, "2", reqs[0].Get("limit"))
			assert.Equal(t, "2", reqs[0].Get("offset"))
			assert.Equal(t, "name:desc", reqs[0].Get("sort"))
			assert.Equal(t, "rec", reqs[0].Get("q"))

			assert.Contains(t, out, "rec-02")
			assert.Contains(t, out, "rec-03")
			assert.NotContains(t, out, "rec-04")
			assert.Contains(t, out, "Showing 3–4 of 7. Next page: --offset 4")
		})
	}
}

func TestListCommands_UnpagedSendsNoPageParameters(t *testing.T) {
	for _, tc := range listCommandCases() {
		t.Run(tc.name, func(t *testing.T) {
			server, out, err := execListCommand(t, tc, 30, "table")
			require.NoError(t, err)

			reqs := server.requests()
			require.Len(t, reqs, 1)
			for _, key := range []string{"limit", "offset", "sort", "q"} {
				assert.False(t, reqs[0].Has(key), "an unpaged list must not send %s", key)
			}
			assert.Contains(t, out, "Showing 1–25 of 30. Next page: --offset 25")
		})
	}
}

func TestListCommands_AllWalksToTheEnd(t *testing.T) {
	for _, tc := range listCommandCases() {
		t.Run(tc.name, func(t *testing.T) {
			server, out, err := execListCommand(t, tc, 7, "table", "--all", "--limit", "3")
			require.NoError(t, err)

			var offsets []string
			for _, q := range server.requests() {
				offsets = append(offsets, q.Get("offset"))
			}
			assert.Equal(t, []string{"", "3", "6"}, offsets)
			for i := 0; i < 7; i++ {
				assert.Contains(t, out, fmt.Sprintf("rec-%02d", i))
			}
			assert.Contains(t, out, "Showing 1–7 of 7.")
			assert.NotContains(t, out, "Next page")
		})
	}
}

func TestListCommands_JSONKeepsTheEnvelope(t *testing.T) {
	for _, tc := range listCommandCases() {
		t.Run(tc.name, func(t *testing.T) {
			_, out, err := execListCommand(t, tc, 7, "json", "--limit", "2")
			require.NoError(t, err)

			var body map[string]interface{}
			require.NoError(t, json.Unmarshal([]byte(out), &body))
			assert.EqualValues(t, 7, body["total"])
			assert.EqualValues(t, 2, body["limit"])
			assert.EqualValues(t, 2, body["next_offset"])
			assert.Len(t, body[tc.key], 2)
			assert.NotContains(t, out, "Showing", "the footer is for people, not for JSON")
		})
	}
}

func TestListCommands_UnknownSortSurfacesTheServers400(t *testing.T) {
	for _, tc := range listCommandCases() {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := execListCommand(t, tc, 7, "table", "--sort", "bogus")

			require.Error(t, err)
			assert.True(t, sdkerrors.IsInvalidInput(err), "err = %v (%T), want the server's 400 as an invalid-input error", err, err)
			assert.Contains(t, err.Error(), `unknown sort field "bogus"`)
		})
	}
}

func TestListCommands_NegativeLimitIsRefused(t *testing.T) {
	for _, tc := range listCommandCases() {
		t.Run(tc.name, func(t *testing.T) {
			server, _, err := execListCommand(t, tc, 7, "table", "--limit", "-1")

			var flagErr *ListPageFlagError
			require.ErrorAs(t, err, &flagErr)
			assert.Empty(t, server.requests(), "a refused flag never reaches the server")
		})
	}
}
