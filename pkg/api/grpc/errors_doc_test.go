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

package grpc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

var grpcErrorDocPath = filepath.Join("..", "..", "..", "docs", "multiprotocol-api", "api.md")

const grpcStatusTableHeader = "| Status | `pkg/errors` codes |"

// grpcStatusTableRows returns the gRPC status table of the API reference as
// a map from each documented pkg/errors code name to the status it is listed
// under.
func grpcStatusTableRows(t *testing.T, doc string) map[string]codes.Code {
	t.Helper()
	_, table, found := strings.Cut(doc, "\n"+grpcStatusTableHeader+"\n")
	if !found {
		t.Fatalf("%s: no table headed %q", grpcErrorDocPath, grpcStatusTableHeader)
	}
	documented := make(map[string]codes.Code)
	for _, line := range strings.Split(table, "\n") {
		if !strings.HasPrefix(line, "| `") {
			if strings.HasPrefix(line, "|-") {
				continue
			}
			break
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) != 2 {
			t.Fatalf("row %q: want a status and a code list", line)
		}
		var grpcCode codes.Code
		statusName := strings.TrimSpace(cells[0])
		if err := grpcCode.UnmarshalJSON([]byte(`"` + strings.Trim(statusName, "`") + `"`)); err != nil {
			t.Fatalf("row %q: unknown gRPC status %s: %v", line, statusName, err)
		}
		for _, name := range strings.Split(cells[1], ",") {
			name = strings.Trim(strings.TrimSpace(name), "`")
			if _, dup := documented[name]; dup {
				t.Errorf("code %q is listed under more than one status", name)
			}
			documented[name] = grpcCode
		}
	}
	return documented
}

// TestGRPCStatusDocTableMatchesCodeMap checks the API reference's gRPC status
// table against grpcCodes: every pkg/errors code is listed exactly once,
// under the status the server sends for it.
func TestGRPCStatusDocTableMatchesCodeMap(t *testing.T) {
	doc, err := os.ReadFile(grpcErrorDocPath)
	if err != nil {
		t.Fatalf("read %s: %v", grpcErrorDocPath, err)
	}
	documented := grpcStatusTableRows(t, string(doc))

	unknown := dberrors.ErrUnknown.String()
	listed := 0
	for code := dberrors.ErrUnknown; code == dberrors.ErrUnknown || code.String() != unknown; code++ {
		got, ok := documented[code.String()]
		if !ok {
			t.Errorf("%s: code %q is not in the gRPC status table", grpcErrorDocPath, code)
			continue
		}
		listed++
		if want := errorCodeToGRPCCode(code); got != want {
			t.Errorf("%s: %q is listed under %v, the server sends %v", grpcErrorDocPath, code, got, want)
		}
	}
	if listed != len(documented) {
		t.Errorf("%s: the gRPC status table lists %d codes, pkg/errors has %d", grpcErrorDocPath, len(documented), listed)
	}
}
