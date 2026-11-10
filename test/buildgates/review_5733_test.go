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

package buildgates

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	integrationComposePath = filepath.Join(repoRoot, "test", "integration", "docker-compose.integration.yml")
	makefilePath           = filepath.Join(repoRoot, "Makefile")
)

// requiredVarStart is the opening of a `${NAME:?...}` or `${NAME?...}`
// expression.
var requiredVarStart = regexp.MustCompile(`^\$\{[A-Za-z_][A-Za-z0-9_]*:?\?`)

// nestedRequiredVars returns each `${NAME:?...}` that sits inside another
// `${...}` expression. Compose evaluates a nested expression even when the
// outer variable is set, so a nested requirement fails a run that sets only
// the outer variable.
func nestedRequiredVars(text string) []string {
	var nested []string
	depth := 0
	for i := 0; i < len(text); i++ {
		switch {
		case strings.HasPrefix(text[i:], "$$"):
			i++
		case strings.HasPrefix(text[i:], "${"):
			if depth > 0 {
				if m := requiredVarStart.FindString(text[i:]); m != "" {
					nested = append(nested, m)
				}
			}
			depth++
			i++
		case text[i] == '}' && depth > 0:
			depth--
		case text[i] == '\n':
			depth = 0
		}
	}
	return nested
}

func TestIntegrationComposeHasNoNestedRequiredVariable(t *testing.T) {
	if nested := nestedRequiredVars(readFile(t, integrationComposePath)); len(nested) > 0 {
		t.Errorf("%s nests required variables %v inside another default; compose evaluates them even when the outer variable is set",
			integrationComposePath, nested)
	}
}

func TestNestedRequiredVarsFindsOnlyNestedRequirements(t *testing.T) {
	cases := map[string]struct {
		text string
		want []string
	}{
		"nested required":          {text: "image: ${A:-${B:?set B}}", want: []string{"${B:?"}},
		"nested required no colon": {text: "image: ${A:-${B?set B}}", want: []string{"${B?"}},
		"top-level required":       {text: "image: ${A:?run through make}"},
		"nested plain default":     {text: "image: ${A:-${B}}"},
		"escaped dollar":           {text: "cmd: $${A:-$${B:?x}}"},
		"two lines":                {text: "a: ${A:-x}\nb: ${B:?y}"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := nestedRequiredVars(tc.text)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// credentialSource is a shell `.` or `source` of the gomodauth secret.
var credentialSource = regexp.MustCompile(`(?:^|[\s;])(?:\.|source)\s+/run/secrets/gomodauth\b`)

// unexportedCredentialSources returns the 1-based lines of a Dockerfile that
// source the module credential file without `set -a` earlier on that line.
// ci.yml writes the file as plain assignments (the ci-template's form), so a
// source without `set -a` never hands the git credential rewrite
// to the go subprocess.
func unexportedCredentialSources(dockerfile string) []int {
	var lines []int
	for i, line := range strings.Split(dockerfile, "\n") {
		at := credentialSource.FindStringIndex(line)
		if at == nil {
			continue
		}
		before := line[:at[0]]
		setA := strings.LastIndex(before, "set -a")
		if setA < 0 || strings.LastIndex(before, "set +a") > setA {
			lines = append(lines, i+1)
		}
	}
	return lines
}

func TestDockerfilesExportTheModuleCredentialsTheySource(t *testing.T) {
	checked := 0
	for _, dir := range []string{"build", "test"} {
		root := filepath.Join(repoRoot, dir)
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasPrefix(d.Name(), "Dockerfile") {
				return nil
			}
			checked++
			if lines := unexportedCredentialSources(readFile(t, path)); len(lines) > 0 {
				t.Errorf("%s sources /run/secrets/gomodauth without `set -a` on lines %v", path, lines)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if checked == 0 {
		t.Fatal("found no Dockerfile under build/ or test/")
	}
}

func TestUnexportedCredentialSourcesFindsASourceWithoutSetA(t *testing.T) {
	cases := map[string]struct {
		dockerfile string
		want       []int
	}{
		"plain source": {
			dockerfile: "RUN set -eu; \\\n    if [ -f /run/secrets/gomodauth ]; then . /run/secrets/gomodauth; fi; \\\n",
			want:       []int{2},
		},
		"source keyword": {
			dockerfile: "RUN source /run/secrets/gomodauth && go mod download\n",
			want:       []int{1},
		},
		"set -a closed before the source": {
			dockerfile: "RUN set -a; set +a; . /run/secrets/gomodauth\n",
			want:       []int{1},
		},
		"under set -a": {
			dockerfile: "RUN if [ -f /run/secrets/gomodauth ]; then set -a; . /run/secrets/gomodauth; set +a; fi\n",
		},
		"no source": {
			dockerfile: "RUN --mount=type=secret,id=gomodauth go mod download\n",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := unexportedCredentialSources(tc.dockerfile)
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// docker-build builds the devcontainer images; the product image the
// JavaScript SDK's integration stack runs is image-build.
func TestJSIntegrationBuildsTheProductImage(t *testing.T) {
	makefile := readFile(t, makefilePath)
	start := strings.Index(makefile, "define SDK_JS_INTEGRATION_RUN\n")
	if start < 0 {
		t.Fatal("Makefile has no SDK_JS_INTEGRATION_RUN")
	}
	end := strings.Index(makefile[start:], "\nendef\n")
	if end < 0 {
		t.Fatal("SDK_JS_INTEGRATION_RUN has no endef")
	}
	run := makefile[start : start+end]
	if !strings.Contains(run, "$(MAKE) --no-print-directory image-build ") {
		t.Error("SDK_JS_INTEGRATION_RUN does not build the product image with image-build")
	}
	if strings.Contains(run, "docker-build") {
		t.Error("SDK_JS_INTEGRATION_RUN calls docker-build, which builds the devcontainer images, not the product image")
	}
}
