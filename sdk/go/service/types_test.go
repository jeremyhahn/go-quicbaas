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

package service

import (
	"context"
	stderrors "errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	pkgservice "github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// pkgServiceDir is the origin package this file re-exports, relative to the
// directory `go test` runs this package in.
const pkgServiceDir = "../../../pkg/service"

// =============================================================================
// Attestation alias fidelity
// =============================================================================
//
// The attestation subsystem is the seam an embedding application fills to
// supply container attestation evidence. If these aliases ever stop resolving
// to the pkg/service originals, an embedder can no longer implement the seam
// from the SDK, so each one is asserted in both directions at compile time.

var (
	_ AttestationService            = (pkgservice.AttestationService)(nil)
	_ pkgservice.AttestationService = (AttestationService)(nil)

	_ ContainerAttestor            = (pkgservice.ContainerAttestor)(nil)
	_ pkgservice.ContainerAttestor = (ContainerAttestor)(nil)

	_ *VerifyAttestationRequest            = (*pkgservice.VerifyAttestationRequest)(nil)
	_ *pkgservice.VerifyAttestationRequest = (*VerifyAttestationRequest)(nil)

	_ *VerifyAttestationOptions            = (*pkgservice.VerifyAttestationOptions)(nil)
	_ *pkgservice.VerifyAttestationOptions = (*VerifyAttestationOptions)(nil)

	_ *VerifyAttestationResponse            = (*pkgservice.VerifyAttestationResponse)(nil)
	_ *pkgservice.VerifyAttestationResponse = (*VerifyAttestationResponse)(nil)

	_ *VerifyAttestationChainRequest            = (*pkgservice.VerifyAttestationChainRequest)(nil)
	_ *pkgservice.VerifyAttestationChainRequest = (*VerifyAttestationChainRequest)(nil)

	_ *VerifyAttestationChainResponse            = (*pkgservice.VerifyAttestationChainResponse)(nil)
	_ *pkgservice.VerifyAttestationChainResponse = (*VerifyAttestationChainResponse)(nil)

	_ *AttestContainerRequest            = (*pkgservice.AttestContainerRequest)(nil)
	_ *pkgservice.AttestContainerRequest = (*AttestContainerRequest)(nil)

	_ *AttestContainerResponse            = (*pkgservice.AttestContainerResponse)(nil)
	_ *pkgservice.AttestContainerResponse = (*AttestContainerResponse)(nil)
)

// sameType fails to compile unless a and b have the identical type. Generic
// inference must unify both arguments onto one T, so a re-export that drifts
// from its original — a constructor whose signature changed, a sentinel
// retyped — breaks the build here rather than at a consumer.
func sameType[T any](_, _ T) {}

// The function and sentinel re-exports carry types that cannot be spelled
// from the SDK (NewRuntimeContainerAttestor takes a pkg/orchestration seam),
// so identity is asserted structurally instead of by naming the type.
func TestAttestationAliases_MatchOriginalSignatures(t *testing.T) {
	sameType(NewAttestationService, pkgservice.NewAttestationService)
	sameType(NewRuntimeContainerAttestor, pkgservice.NewRuntimeContainerAttestor)

	sameType(ErrAttestationNilRequest, pkgservice.ErrAttestationNilRequest)
	sameType(ErrAttestationEmptyChain, pkgservice.ErrAttestationEmptyChain)
	sameType(ErrAttestationEmptyFormat, pkgservice.ErrAttestationEmptyFormat)
	sameType(ErrAttestationNoSignature, pkgservice.ErrAttestationNoSignature)
	sameType(ErrAttestationNoAttestedKey, pkgservice.ErrAttestationNoAttestedKey)
	sameType(ErrAttestationEmptyImage, pkgservice.ErrAttestationEmptyImage)
	sameType(ErrAttestationNoAttestor, pkgservice.ErrAttestationNoAttestor)
}

// A sentinel re-export is only useful if it is the same value the service
// returns: consumers compare with errors.Is against the SDK name.
func TestAttestationSentinels_AreTheOriginalValues(t *testing.T) {
	sentinels := []struct {
		name string
		sdk  error
		pkg  error
	}{
		{"ErrAttestationNilRequest", ErrAttestationNilRequest, pkgservice.ErrAttestationNilRequest},
		{"ErrAttestationEmptyChain", ErrAttestationEmptyChain, pkgservice.ErrAttestationEmptyChain},
		{"ErrAttestationEmptyFormat", ErrAttestationEmptyFormat, pkgservice.ErrAttestationEmptyFormat},
		{"ErrAttestationNoSignature", ErrAttestationNoSignature, pkgservice.ErrAttestationNoSignature},
		{"ErrAttestationNoAttestedKey", ErrAttestationNoAttestedKey, pkgservice.ErrAttestationNoAttestedKey},
		{"ErrAttestationEmptyImage", ErrAttestationEmptyImage, pkgservice.ErrAttestationEmptyImage},
		{"ErrAttestationNoAttestor", ErrAttestationNoAttestor, pkgservice.ErrAttestationNoAttestor},
	}

	for _, s := range sentinels {
		t.Run(s.name, func(t *testing.T) {
			if s.sdk != s.pkg {
				t.Fatalf("%s re-export is not the pkg/service value: %v != %v", s.name, s.sdk, s.pkg)
			}
			if s.sdk == nil {
				t.Fatalf("%s re-export is nil", s.name)
			}
		})
	}
}

// The point of the ContainerAttestor alias is that an embedder can implement
// the seam using only SDK names and have the service accept it.
type stubContainerAttestor struct {
	resp *AttestContainerResponse
	err  error
}

func (a *stubContainerAttestor) AttestContainer(context.Context, *AttestContainerRequest) (*AttestContainerResponse, error) {
	return a.resp, a.err
}

// A ContainerAttestor written against the SDK aliases must satisfy the
// pkg/service seam and be reachable through the aliased constructor.
func TestContainerAttestorAlias_FillsTheSeam(t *testing.T) {
	var _ pkgservice.ContainerAttestor = (*stubContainerAttestor)(nil)

	attestor := &stubContainerAttestor{
		resp: &AttestContainerResponse{Attested: true, TrustLevel: "attested"},
	}
	svc := NewAttestationService(nil, attestor)

	resp, err := svc.AttestContainer(context.Background(), &AttestContainerRequest{Image: "ghcr.io/example/app:v1"})
	if err != nil {
		t.Fatalf("AttestContainer through SDK aliases: %v", err)
	}
	if !resp.Attested || resp.TrustLevel != "attested" {
		t.Fatalf("attestor evidence did not survive the seam: %+v", resp)
	}
}

// The unfilled-seam and invalid-input contracts must surface through the
// re-exported sentinels, which is what an embedder branches on.
func TestAttestationService_ErrorContract(t *testing.T) {
	svc := NewAttestationService(nil, nil)
	ctx := context.Background()

	tests := []struct {
		name string
		req  *AttestContainerRequest
		want error
	}{
		{"nil request", nil, ErrAttestationNilRequest},
		{"empty image", &AttestContainerRequest{}, ErrAttestationEmptyImage},
		{"no attestor registered", &AttestContainerRequest{Image: "ghcr.io/example/app:v1"}, ErrAttestationNoAttestor},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := svc.AttestContainer(ctx, tt.req)
			if resp != nil {
				t.Fatalf("expected no evidence, got %+v", resp)
			}
			if !stderrors.Is(err, tt.want) {
				t.Fatalf("got error %v, want %v", err, tt.want)
			}
		})
	}
}

// A nil runtime must produce a nil attestor so callers can pass an optional
// runtime straight through and keep the not-configured contract.
func TestNewRuntimeContainerAttestor_NilRuntime(t *testing.T) {
	if got := NewRuntimeContainerAttestor(nil); got != nil {
		t.Fatalf("expected nil attestor for nil runtime, got %#v", got)
	}
}

// =============================================================================
// Alias closure guard
// =============================================================================

// unparseableDirError reports a directory whose Go sources could not be read.
type unparseableDirError struct {
	Dir string
	Err error
}

func (e *unparseableDirError) Error() string {
	return fmt.Sprintf("service: cannot parse Go sources in %q: %v", e.Dir, e.Err)
}

func (e *unparseableDirError) Unwrap() error { return e.Err }

// parsePackage parses every non-test Go file in dir.
func parsePackage(dir string) ([]*ast.File, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, &unparseableDirError{Dir: dir, Err: err}
	}
	if len(paths) == 0 {
		return nil, &unparseableDirError{Dir: dir, Err: stderrors.New("no Go source files")}
	}

	fset := token.NewFileSet()
	files := make([]*ast.File, 0, len(paths))
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, &unparseableDirError{Dir: dir, Err: err}
		}
		files = append(files, file)
	}
	return files, nil
}

// exportedNames returns the exported top-level identifiers declared by files.
func exportedNames(files []*ast.File) map[string]bool {
	out := make(map[string]bool)
	for _, file := range files {
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil && d.Name.IsExported() {
					out[d.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if s.Name.IsExported() {
							out[s.Name.Name] = true
						}
					case *ast.ValueSpec:
						for _, name := range s.Names {
							if name.IsExported() {
								out[name.Name] = true
							}
						}
					}
				}
			}
		}
	}
	return out
}

// declaration pairs the declaring identifier with the AST that may reference
// further pkg/service identifiers: a type's underlying expression, or the
// signature of a function or of a method on that type.
type declaration struct {
	owner string
	node  ast.Node
}

// declarations collects every owner/signature pair in files.
func declarations(files []*ast.File) []declaration {
	var out []declaration
	for _, file := range files {
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				owner := d.Name.Name
				if d.Recv != nil {
					if !d.Name.IsExported() || len(d.Recv.List) == 0 {
						continue
					}
					recv := d.Recv.List[0].Type
					if star, ok := recv.(*ast.StarExpr); ok {
						recv = star.X
					}
					ident, ok := recv.(*ast.Ident)
					if !ok {
						continue
					}
					owner = ident.Name
				}
				out = append(out, declaration{owner: owner, node: d.Type})
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok {
						out = append(out, declaration{owner: ts.Name.Name, node: ts.Type})
					}
				}
			}
		}
	}
	return out
}

// referencedNames reports the pkg/service identifiers node mentions. Selector
// expressions are skipped: those name types owned by other packages.
func referencedNames(node ast.Node, universe map[string]bool) []string {
	var out []string
	ast.Inspect(node, func(n ast.Node) bool {
		if _, ok := n.(*ast.SelectorExpr); ok {
			return false
		}
		ident, ok := n.(*ast.Ident)
		if ok && universe[ident.Name] {
			out = append(out, ident.Name)
		}
		return true
	})
	return out
}

// TestAliasClosure enforces the completeness contract in the package doc:
// every exported pkg/service identifier reachable from an alias declared here
// must itself be aliased here. It is the guard that keeps this file honest —
// the attestation subsystem was silently absent for an entire release because
// nothing checked. A failure lists exactly what to add and which alias needs
// it.
func TestAliasClosure(t *testing.T) {
	originFiles, err := parsePackage(pkgServiceDir)
	if err != nil {
		t.Fatalf("parse pkg/service: %v", err)
	}
	aliasFiles, err := parsePackage(".")
	if err != nil {
		t.Fatalf("parse sdk/go/service: %v", err)
	}

	origin := exportedNames(originFiles)
	aliased := exportedNames(aliasFiles)
	decls := declarations(originFiles)

	// Grow the aliased set to a fixed point: anything an already-aliased
	// declaration mentions is itself required by the contract.
	pulledInBy := make(map[string]string)
	for {
		grew := false
		for _, decl := range decls {
			if !aliased[decl.owner] {
				continue
			}
			for _, name := range referencedNames(decl.node, origin) {
				if aliased[name] {
					continue
				}
				aliased[name] = true
				pulledInBy[name] = decl.owner
				grew = true
			}
		}
		if !grew {
			break
		}
	}

	if len(pulledInBy) == 0 {
		return
	}

	missing := make([]string, 0, len(pulledInBy))
	for name, owner := range pulledInBy {
		missing = append(missing, fmt.Sprintf("\t%s = pkgservice.%s // reachable from %s", name, name, owner))
	}
	sort.Strings(missing)
	t.Fatalf("%d pkg/service identifiers are reachable from this package's aliases but are not aliased here; add:\n%s",
		len(missing), strings.Join(missing, "\n"))
}

// parsePackage must report a typed error rather than an empty result when the
// directory holds no Go sources, so a mis-set path cannot make TestAliasClosure
// pass vacuously.
func TestParsePackage_InvalidDirectory(t *testing.T) {
	for _, dir := range []string{filepath.Join(t.TempDir(), "does-not-exist"), t.TempDir()} {
		files, err := parsePackage(dir)
		if err == nil {
			t.Fatalf("parsePackage(%q) succeeded with %d files, want an error", dir, len(files))
		}
		var dirErr *unparseableDirError
		if !stderrors.As(err, &dirErr) {
			t.Fatalf("parsePackage(%q) returned %T, want *unparseableDirError", dir, err)
		}
		if dirErr.Dir != dir {
			t.Fatalf("error reports dir %q, want %q", dirErr.Dir, dir)
		}
	}
}
