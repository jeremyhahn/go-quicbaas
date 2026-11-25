// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package grpc

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoTransportTrustsCallerSuppliedRoles guards SPEC Authorization clean-up
// D3: no gRPC or REST transport reads a caller-supplied roles header
// (`x-roles`) as an authorization input. Roles come from
// the authenticated subject and decisions from go-qrdb's Authorizer.
func TestNoTransportTrustsCallerSuppliedRoles(t *testing.T) {
	forbidden := []string{`"x-roles"`, `"X-Roles"`}
	for _, dir := range []string{".", "../rest"} {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if path != dir {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, lit := range forbidden {
				if strings.Contains(string(src), lit) {
					t.Errorf("%s reads caller-supplied header %s", path, lit)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
}
