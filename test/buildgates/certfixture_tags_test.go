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
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

var (
	// composeFileName is a docker compose file: compose.yaml,
	// docker-compose.yml, docker-compose.integration.yml and the like.
	composeFileName = regexp.MustCompile(`(^|[.-])compose([.-][^/]*)?\.ya?ml$`)
	// certfixtureGoCommand is one shell command that builds or runs the
	// certfixture generator.
	certfixtureGoCommand = regexp.MustCompile(`\bgo\s+(?:run|build)\b[^;&|\n]*certfixture[^;&|\n]*`)
	// codecJSONTag is a -tags flag whose list carries codec_json.
	codecJSONTag = regexp.MustCompile(`-tags[=\s]+\S*\bcodec_json\b`)
)

// composeServices is the part of a compose file this gate reads: each
// service's command and entrypoint, in string or list form.
type composeServices struct {
	Services map[string]struct {
		Command    any `yaml:"command"`
		Entrypoint any `yaml:"entrypoint"`
	} `yaml:"services"`
}

// commandLine joins a compose command or entrypoint, string or list, into
// one line.
func commandLine(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case []any:
		parts := make([]string, 0, len(c))
		for _, p := range c {
			if s, ok := p.(string); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

// certfixtureTagFindings parses one compose file and returns every certfixture
// go run or go build, as "service: command", and the subset of them that
// lacks -tags codec_json. Without that tag the CA signer store cannot build
// its JSON codec and certs-init exits before any test runs (#1727).
func certfixtureTagFindings(compose []byte) (calls, untagged []string, err error) {
	var file composeServices
	if err := yaml.Unmarshal(compose, &file); err != nil {
		return nil, nil, err
	}
	names := make([]string, 0, len(file.Services))
	for name := range file.Services {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		svc := file.Services[name]
		line := commandLine(svc.Entrypoint) + " " + commandLine(svc.Command)
		for _, cmd := range certfixtureGoCommand.FindAllString(line, -1) {
			call := name + ": " + strings.TrimSpace(cmd)
			calls = append(calls, call)
			if !codecJSONTag.MatchString(cmd) {
				untagged = append(untagged, call)
			}
		}
	}
	return calls, untagged, nil
}

// composeFiles lists every compose file in the repository.
func composeFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if composeFileName.MatchString(d.Name()) {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", repoRoot, err)
	}
	return files
}

// Every compose service that runs the certfixture generator builds it with
// codec_json, as the Makefile's certs target does.
func TestComposeCertfixtureCallsCarryCodecJSON(t *testing.T) {
	total := 0
	for _, path := range composeFiles(t) {
		calls, untagged, err := certfixtureTagFindings([]byte(readFile(t, path)))
		if err != nil {
			t.Errorf("%s: parse compose file: %v", path, err)
			continue
		}
		total += len(calls)
		for _, call := range untagged {
			t.Errorf("%s: %s builds certfixture without -tags codec_json", path, call)
		}
	}
	if total == 0 {
		t.Error("no compose file runs test/integration/certfixture: the gate no longer finds the call it guards")
	}
}

func TestCertfixtureTagFindings(t *testing.T) {
	cases := map[string]struct {
		compose  string
		calls    int
		untagged []string
	}{
		"list without the tag": {
			compose:  "services:\n  certs-init:\n    command: [\"go\", \"run\", \"./test/integration/certfixture/cmd\", \"/out\"]\n",
			calls:    1,
			untagged: []string{"certs-init: go run ./test/integration/certfixture/cmd /out"},
		},
		"list with the tag": {
			compose: "services:\n  certs-init:\n    command: [\"go\", \"run\", \"-tags\", \"codec_json\", \"./test/integration/certfixture/cmd\", \"/out\"]\n",
			calls:   1,
		},
		"shell string with a tag list": {
			compose: "services:\n  certs:\n    entrypoint: [\"sh\", \"-c\"]\n    command: \"cd /src && go build -tags=integration,codec_json -o /bin/cf ./test/integration/certfixture/cmd\"\n",
			calls:   1,
		},
		"shell string with another tag only": {
			compose:  "services:\n  certs:\n    command: \"go build -tags integration ./test/integration/certfixture/cmd && /bin/cf\"\n",
			calls:    1,
			untagged: []string{"certs: go build -tags integration ./test/integration/certfixture/cmd"},
		},
		"tag on a different command": {
			compose:  "services:\n  certs:\n    command: \"go build -tags codec_json ./cmd/x; go run ./test/integration/certfixture/cmd /out\"\n",
			calls:    1,
			untagged: []string{"certs: go run ./test/integration/certfixture/cmd /out"},
		},
		"no certfixture call": {
			compose: "services:\n  app:\n    command: [\"go\", \"run\", \"./cmd/quicbaas\"]\n",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			calls, untagged, err := certfixtureTagFindings([]byte(tc.compose))
			if err != nil {
				t.Fatalf("certfixtureTagFindings: %v", err)
			}
			if len(calls) != tc.calls {
				t.Errorf("calls = %v, want %d", calls, tc.calls)
			}
			if strings.Join(untagged, "|") != strings.Join(tc.untagged, "|") {
				t.Errorf("untagged = %v, want %v", untagged, tc.untagged)
			}
		})
	}
}

func TestCertfixtureTagFindingsRejectsBrokenYAML(t *testing.T) {
	if _, _, err := certfixtureTagFindings([]byte("services: [unclosed\n")); err == nil {
		t.Error("certfixtureTagFindings accepted malformed YAML")
	}
}
