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
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// stagedComposeFiles are the compose files the staging targets build from.
// Staging the host file is only half the contract: each image they build must
// also be handed the secret by Compose and read it in its Dockerfile, or the
// build fetches private modules unauthenticated however full the file is.
var stagedComposeFiles = []string{
	"test/integration/docker-compose.integration.yml",
	"test/integration/docker-compose.seal.yml",
	"test/e2e/docker-compose.e2e.yml",
}

// gomodauthMount is the Dockerfile instruction that reads the secret.
const gomodauthMount = "--mount=type=secret,id=gomodauth"

// ComposeFileError reports that a compose file the gate must read could not be
// read or parsed.
type ComposeFileError struct {
	Path string
	Err  error
}

func (e *ComposeFileError) Error() string { return "cannot read " + e.Path + ": " + e.Err.Error() }

func (e *ComposeFileError) Unwrap() error { return e.Err }

type composeFile struct {
	Services map[string]struct {
		Build yaml.Node `yaml:"build"`
	} `yaml:"services"`
}

type composeBuild struct {
	Context    string      `yaml:"context"`
	Dockerfile string      `yaml:"dockerfile"`
	Secrets    []yaml.Node `yaml:"secrets"`
}

// checkWiring returns one finding per built service in stagedComposeFiles that
// is not handed the secret or whose Dockerfile never reads it.
func checkWiring(root string) ([]string, error) {
	var findings []string
	for _, rel := range stagedComposeFiles {
		path := filepath.Join(root, rel)
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, &ComposeFileError{Path: rel, Err: err}
		}
		var compose composeFile
		if err := yaml.Unmarshal(raw, &compose); err != nil {
			return nil, &ComposeFileError{Path: rel, Err: err}
		}
		names := make([]string, 0, len(compose.Services))
		for name := range compose.Services {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			node := compose.Services[name].Build
			if node.Kind == 0 {
				continue // pulls an image; builds nothing
			}
			where := rel + ": service " + name
			if node.Kind != yaml.MappingNode {
				findings = append(findings, where+" builds without declaring the gomodauth secret")
				continue
			}
			var build composeBuild
			if err := node.Decode(&build); err != nil {
				return nil, &ComposeFileError{Path: rel, Err: err}
			}
			if !mountsSecret(build.Secrets) {
				findings = append(findings, where+" is not handed the gomodauth secret (build.secrets)")
			}
			dockerfile := build.Dockerfile
			if dockerfile == "" {
				dockerfile = "Dockerfile"
			}
			dfPath := filepath.Join(filepath.Dir(path), build.Context, dockerfile)
			content, err := os.ReadFile(dfPath)
			if err != nil {
				return nil, &ComposeFileError{Path: dfPath, Err: err}
			}
			if !strings.Contains(string(content), gomodauthMount) {
				findings = append(findings, where+": "+dockerfile+" never reads the gomodauth secret ("+gomodauthMount+")")
			}
		}
	}
	return findings, nil
}

// mountsSecret reports whether a build's secrets list names gomodauth, in
// either the short form or the long form's source.
func mountsSecret(secrets []yaml.Node) bool {
	for _, s := range secrets {
		switch s.Kind {
		case yaml.ScalarNode:
			if s.Value == "gomodauth" {
				return true
			}
		case yaml.MappingNode:
			var long struct {
				Source string `yaml:"source"`
			}
			if s.Decode(&long) == nil && long.Source == "gomodauth" {
				return true
			}
		}
	}
	return false
}
