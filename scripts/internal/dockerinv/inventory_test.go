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

package dockerinv

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// repoFile is one file to write into a fixture repository.
type repoFile struct {
	path    string
	content string
}

// writeRepo builds a fixture repository whose Makefile answers print-% and
// carries the three variables a producer may name.
func writeRepo(t *testing.T, makefileBody string, extra ...repoFile) string {
	t.Helper()
	dir := t.TempDir()
	header := "DOCKER_IMAGE := quicbaas\n" +
		"DOCKER_TAG := 9.9.9\n" +
		"DOCKER_REGISTRY := registry.invalid\n\n" +
		"print-%:\n\t@echo \"$($*)\"\n\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(header+makefileBody), 0o644); err != nil {
		t.Fatalf("writing the fixture Makefile: %v", err)
	}
	for _, f := range extra {
		full := filepath.Join(dir, f.path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("creating %s: %v", filepath.Dir(f.path), err)
		}
		if err := os.WriteFile(full, []byte(f.content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", f.path, err)
		}
	}
	return dir
}

func TestDeriveFindsEveryProducerAndNothingElse(t *testing.T) {
	// Every shape a producer takes in this tree, in one fixture: a build spread
	// over continuation lines, a build naming a Makefile variable, a buildx
	// build, a `docker tag` destination, a producer in a script well below the
	// top level, a shell variable assigned a literal elsewhere in that script,
	// and — as the negative — a removal and a pulled compose image.
	repo := writeRepo(t,
		".PHONY: multiline\nmultiline:\n"+
			"\tdocker build \\\n"+
			"\t\t-f Dockerfile \\\n"+
			"\t\t-t fixture-multiline:latest \\\n"+
			"\t\t.\n\n"+
			".PHONY: expanded\nexpanded:\n"+
			"\t$(DOCKER) build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .\n"+
			"\tdocker buildx build -t $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):latest .\n"+
			"\tdocker tag $(DOCKER_IMAGE):$(DOCKER_TAG) fixture-retagged:stable\n\n"+
			".PHONY: remove\nremove:\n"+
			"\tdocker rmi someone-elses/image:latest\n",
		repoFile{
			path: "test/tools/build-image.sh",
			content: "#!/usr/bin/env bash\n" +
				"NESTED_TAG=fixture-nested\n" +
				"docker build -t \"$NESTED_TAG\" .\n",
		},
		repoFile{
			path: "docker-compose.yml",
			content: "services:\n" +
				"  built:\n" +
				"    image: fixture-compose:latest\n" +
				"    build:\n" +
				"      context: .\n" +
				"  pulled:\n" +
				"    image: redis:7-alpine\n",
		},
	)

	got, err := Derive(repo)
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	want := []string{
		"fixture-compose:latest",
		"fixture-multiline:latest",
		// The shell variable assigned a literal in the same script resolves,
		// and a bare name gains the :latest tag Docker gives it.
		"fixture-nested:latest",
		"fixture-retagged:stable",
		"quicbaas:9.9.9",
		"registry.invalid/quicbaas:latest",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Derive =\n  %q\nwant\n  %q", got, want)
	}
}

func TestDeriveRefusesAnInventoryItCannotComplete(t *testing.T) {
	t.Run("a build tag decided at run time fails rather than dropping out", func(t *testing.T) {
		// THE CASE THAT MATTERS. An image nobody can name cannot be cleaned up
		// by name, so silently omitting it would report the escape as coverage.
		repo := writeRepo(t, ".PHONY: b\nb:\n\tdocker build -t \"$$RUNTIME_TAG\" .\n")
		_, err := Derive(repo)
		var unresolved *UnresolvedTagsError
		if !errors.As(err, &unresolved) {
			t.Fatalf("Derive on an unresolvable tag = %v, want *UnresolvedTagsError", err)
		}
		if len(unresolved.Tags) != 1 {
			t.Fatalf("expected one unresolved producer, got %+v", unresolved.Tags)
		}
		if unresolved.Tags[0].Origin != "Makefile" {
			t.Fatalf("the error does not name the file the producer is in: %+v", unresolved.Tags[0])
		}
	})

	t.Run("a repository with no Makefile cannot be derived", func(t *testing.T) {
		_, err := Derive(t.TempDir())
		if err == nil {
			t.Fatal("Derive on a tree with no Makefile must fail, not answer empty")
		}
	})

	t.Run("an empty DOCKER_IMAGE is refused rather than producing :latest", func(t *testing.T) {
		dir := t.TempDir()
		body := "DOCKER_IMAGE :=\nDOCKER_TAG := 1\nDOCKER_REGISTRY := r\n\nprint-%:\n\t@echo \"$($*)\"\n"
		if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(body), 0o644); err != nil {
			t.Fatalf("writing the fixture Makefile: %v", err)
		}
		if _, err := Derive(dir); err == nil {
			t.Fatal("an empty DOCKER_IMAGE must fail the derivation")
		}
	})
}

func TestDeriveIgnoresWhatIsNotOurs(t *testing.T) {
	// A vendored tree and an installed dependency answer for their own images.
	repo := writeRepo(t, ".PHONY: b\nb:\n\tdocker build -t ours:latest .\n",
		repoFile{path: "vendor/other/build.sh", content: "docker build -t vendored:latest .\n"},
		repoFile{path: "node_modules/pkg/build.sh", content: "docker build -t installed:latest .\n"},
	)
	got, err := Derive(repo)
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"ours:latest"}) {
		t.Fatalf("Derive = %q, want only ours:latest", got)
	}
}

func TestScanRecordReadsTheCommandGrammar(t *testing.T) {
	vars := map[string]string{"DOCKER_IMAGE": "img", "DOCKER_TAG": "1.0", "DOCKER_REGISTRY": "reg"}
	cases := []struct {
		name   string
		record string
		want   []string
	}{
		{name: "build -t", record: "docker build -t a:1 .", want: []string{"a:1"}},
		{name: "buildx build -t", record: "docker buildx build -t a:1 .", want: []string{"a:1"}},
		{name: "absolute docker", record: "/usr/bin/docker build -t a:1 .", want: []string{"a:1"}},
		{name: "make DOCKER variable", record: "$(DOCKER) build -t a:1 .", want: []string{"a:1"}},
		{name: "tag destination only", record: "docker tag a:1 b:2", want: []string{"b:2"}},
		{name: "bare name gains latest", record: "docker build -t a .", want: []string{"a:latest"}},
		// A removal is not a producer: that a repository deletes a tag is the
		// very claim cleanup is being checked against.
		{name: "removal is not a producer", record: "docker rmi a:1", want: nil},
		{name: "pull is not a producer", record: "docker pull redis:7", want: nil},
		// A command that ends resets the scan, so the next word cannot be read
		// as an argument of the one before it.
		{name: "semicolon ends the command", record: "docker build -t a:1 .; echo -t b:2", want: []string{"a:1"}},
		{name: "pipe ends the command", record: "docker build -t a:1 . | tee log", want: []string{"a:1"}},
		{name: "two builds in one record", record: "docker build -t a:1 . && docker build -t b:2 .", want: []string{"a:1", "b:2"}},
		// A Makefile recipe line starts with a tab, which yields an empty first
		// field; the scan must not be thrown by it.
		{name: "leading tab", record: "\tdocker build -t a:1 .", want: []string{"a:1"}},
		{name: "comment", record: "\t# docker build -t a:1 .", want: nil},
		{name: "quoted tag", record: `docker build -t "a:1" .`, want: []string{"a:1"}},
		{name: "not the docker binary", record: "docker-compose build -t a:1 .", want: nil},
		{name: "-t with no value", record: "docker build -t", want: nil},
		{name: "tag flags skipped before positionals", record: "docker tag --quiet a:1 b:2", want: []string{"b:2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			scanRecord(tc.record, func(token string) {
				id, ok := resolve(token, vars, map[string]string{})
				if !ok {
					t.Fatalf("resolve(%q) failed", token)
				}
				if !strings.Contains(id, ":") {
					id += ":latest"
				}
				got = append(got, id)
			})
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("scanRecord(%q) = %q, want %q", tc.record, got, tc.want)
			}
		})
	}
}

func TestJoinContinuationsMatchesTheShellsOwnView(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "a build spread over lines becomes one record",
			content: "docker build \\\n\t-t a:1 \\\n\t.\n",
			want:    []string{"docker build \t-t a:1 \t.", ""},
		},
		{
			name:    "nothing is inserted where the backslash was",
			content: "foo\\\nbar\n",
			want:    []string{"foobar", ""},
		},
		{
			name:    "a plain line is its own record",
			content: "one\ntwo\n",
			want:    []string{"one", "two", ""},
		},
		{
			name:    "a trailing continuation at end of file still yields its record",
			content: "docker build \\",
			want:    []string{"docker build "},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := joinContinuations(tc.content); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("joinContinuations(%q) = %q, want %q", tc.content, got, tc.want)
			}
		})
	}
}

func TestScanComposeFileTakesOnlyTheExplicitImageOfABuildingService(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "a building service with an explicit image",
			content: "services:\n  a:\n    image: x:1\n    build:\n      context: .\n",
			want:    []string{"x:1"},
		},
		{
			// Compose names this `<project>-<service>`, and the project name
			// belongs to whichever checkout ran it last, so it is deliberately
			// not derived.
			name:    "a building service with no explicit image",
			content: "services:\n  a:\n    build:\n      context: .\n",
			want:    nil,
		},
		{
			name:    "a pulled image is not ours",
			content: "services:\n  a:\n    image: redis:7-alpine\n",
			want:    nil,
		},
		{
			name:    "image and build in either order",
			content: "services:\n  a:\n    build:\n      context: .\n    image: x:1\n",
			want:    []string{"x:1"},
		},
		{
			name: "one service's build does not carry to the next",
			content: "services:\n  a:\n    build:\n      context: .\n" +
				"  b:\n    image: pulled:1\n",
			want: nil,
		},
		{
			name: "a top-level key after services ends the section",
			content: "services:\n  a:\n    image: x:1\n    build:\n      context: .\n" +
				"volumes:\n  data:\n",
			want: []string{"x:1"},
		},
		{
			name:    "a quoted image is unquoted",
			content: "services:\n  a:\n    image: \"x:1\"\n    build:\n      context: .\n",
			want:    []string{"x:1"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := scanComposeFile(tc.content); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("scanComposeFile = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveReducesAProducerToALiteral(t *testing.T) {
	vars := map[string]string{"DOCKER_IMAGE": "img", "DOCKER_TAG": "1.0", "DOCKER_REGISTRY": "reg"}
	literals := map[string]string{"AGENT_IMAGE": "agent:latest"}
	cases := []struct {
		token   string
		want    string
		wantOK  bool
		comment string
	}{
		{token: "a:1", want: "a:1", wantOK: true},
		{token: "$(DOCKER_IMAGE):$(DOCKER_TAG)", want: "img:1.0", wantOK: true},
		{token: "$(DOCKER_REGISTRY)/$(DOCKER_IMAGE):latest", want: "reg/img:latest", wantOK: true},
		{token: `"$AGENT_IMAGE"`, want: "agent:latest", wantOK: true},
		{token: "${AGENT_IMAGE}", want: "agent:latest", wantOK: true},
		{token: `"$$RUNTIME_TAG"`, wantOK: false, comment: "assigned nowhere: cannot be cleaned up by name"},
		{token: "$(SOME_OTHER_VAR)", wantOK: false, comment: "a make variable this gate does not expand"},
	}
	for _, tc := range cases {
		t.Run(tc.token, func(t *testing.T) {
			got, ok := resolve(tc.token, vars, literals)
			if ok != tc.wantOK {
				t.Fatalf("resolve(%q) ok = %v, want %v (%s)", tc.token, ok, tc.wantOK, tc.comment)
			}
			if ok && got != tc.want {
				t.Fatalf("resolve(%q) = %q, want %q", tc.token, got, tc.want)
			}
		})
	}
}

func TestCollectAssignmentsTakesOnlyLiterals(t *testing.T) {
	cases := []struct {
		name   string
		record string
		want   map[string]string
	}{
		{name: "a literal", record: "TAG=quicbaas:latest", want: map[string]string{"TAG": "quicbaas:latest"}},
		{name: "a quoted literal", record: `TAG="quicbaas:latest"`, want: map[string]string{"TAG": "quicbaas:latest"}},
		{name: "a trailing semicolon", record: "TAG=a:1;", want: map[string]string{"TAG": "a:1"}},
		// A value built from another variable is not a literal, so it cannot
		// vouch for a producer's name.
		{name: "an expansion", record: "TAG=${OTHER}-suffix", want: map[string]string{}},
		{name: "a default expansion", record: "TAG=${OTHER:-fallback}", want: map[string]string{}},
		{name: "a comment", record: "# TAG=a:1", want: map[string]string{}},
		{name: "not an assignment", record: "docker build -t a:1 .", want: map[string]string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := map[string]string{}
			collectAssignments(tc.record, got)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("collectAssignments(%q) = %v, want %v", tc.record, got, tc.want)
			}
		})
	}
}

func TestUnresolvedTagsErrorNamesEveryHole(t *testing.T) {
	err := &UnresolvedTagsError{Tags: []UnresolvedTag{
		{Origin: "Makefile", Token: "$$A"},
		{Origin: "scripts/x.sh", Token: "$B"},
	}}
	msg := err.Error()
	for _, want := range []string{"Makefile", "$$A", "scripts/x.sh", "$B"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the error omits %q: %s", want, msg)
		}
	}
}

func TestScanErrorNamesTheFileTheWayAnOperatorSeesIt(t *testing.T) {
	cause := errors.New("permission denied")
	err := &ScanError{Path: "scripts/x.sh", Err: cause}
	if !strings.Contains(err.Error(), "scripts/x.sh") {
		t.Fatalf("ScanError.Error() does not name the file: %s", err.Error())
	}
	if !errors.Is(err, cause) {
		t.Fatal("ScanError must unwrap to the underlying failure")
	}

	cases := []struct {
		name string
		root string
		path string
		want string
	}{
		{name: "under the root", root: "/repo", path: "/repo/scripts/x.sh", want: "scripts/x.sh"},
		{name: "the root itself", root: "/repo", path: "/repo", want: "."},
		// Outside the root there is no relative name, so the absolute path is
		// the honest answer -- a naming choice, not a swallowed failure.
		{name: "outside the root", root: "relative", path: "/elsewhere/x.sh", want: "/elsewhere/x.sh"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := relativeTo(tc.root, tc.path); got != tc.want {
				t.Fatalf("relativeTo(%q, %q) = %q, want %q", tc.root, tc.path, got, tc.want)
			}
		})
	}
}

func TestDeriveFailsOnASourceItCannotRead(t *testing.T) {
	// A producer that was not read is a producer that is not in the inventory,
	// so an unreadable source has to fail the derivation rather than shrink it.
	cases := []struct {
		name string
		path string
	}{
		{name: "a command file", path: "scripts/build.sh"},
		{name: "a compose file", path: "docker-compose.broken.yml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := writeRepo(t, ".PHONY: b\nb:\n\tdocker build -t a:1 .\n")
			full := filepath.Join(repo, tc.path)
			if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
				t.Fatalf("preparing the fixture: %v", err)
			}
			// A dangling symlink: the walk sees a source, and reading it fails.
			if err := os.Symlink("no-such-target", full); err != nil {
				t.Fatalf("preparing the fixture: %v", err)
			}
			_, err := Derive(repo)
			var scan *ScanError
			if !errors.As(err, &scan) {
				t.Fatalf("Derive with an unreadable source = %v, want *ScanError", err)
			}
		})
	}
}
