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
	"regexp"
	"strings"
	"testing"
)

// integrationTarget is the recipe whose compose build must reuse the layer
// cache: a `--no-cache` build and a `docker rmi` pre-clean of its images cost
// 8.2 min of Integration job 233026 (#1594).
const integrationTarget = "integration-test"

var (
	noCacheRe    = regexp.MustCompile(`--no-cache\b`)
	dockerRmiRe  = regexp.MustCompile(`(\bdocker|\$\(DOCKER\))\s+rmi\b`)
	recipeLineRe = regexp.MustCompile(`^\t`)
)

// makeRecipeLines returns the recipe lines of target in makefile, or nil when the
// target is not defined. The recipe runs from the line after `target:` to the
// first line that is neither blank nor tab-indented.
func makeRecipeLines(makefile, target string) []string {
	lines := strings.Split(makefile, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, target+":") {
			continue
		}
		recipe := []string{}
		for _, next := range lines[i+1:] {
			if strings.TrimSpace(next) == "" {
				continue
			}
			if !recipeLineRe.MatchString(next) {
				break
			}
			recipe = append(recipe, next)
		}
		return recipe
	}
	return nil
}

// cacheDefeats returns every recipe line that throws the layer cache away.
func cacheDefeats(recipe []string) []string {
	var defeats []string
	for _, line := range recipe {
		if noCacheRe.MatchString(line) || dockerRmiRe.MatchString(line) {
			defeats = append(defeats, strings.TrimSpace(line))
		}
	}
	return defeats
}

func TestIntegrationBuildKeepsItsLayerCache(t *testing.T) {
	recipe := makeRecipeLines(readFile(t, makefilePath), integrationTarget)
	if len(recipe) == 0 {
		t.Fatalf("Makefile defines no %s recipe", integrationTarget)
	}
	for _, line := range cacheDefeats(recipe) {
		t.Errorf("%s throws the layer cache away: %s", integrationTarget, line)
	}
}

func TestCacheDefeatsFindsEveryCacheDiscard(t *testing.T) {
	tests := []struct {
		name     string
		makefile string
		want     int
		defined  bool
	}{
		{
			name:     "cached build passes",
			makefile: "integration-test: ## run\n\t@$(DOCKER_COMPOSE) build --build-arg X=1\n\t$(DOCKER_COMPOSE) down --volumes --remove-orphans --rmi local\n",
			want:     0,
			defined:  true,
		},
		{
			name:     "no-cache build fails",
			makefile: "integration-test:\n\t$(DOCKER_COMPOSE) build --no-cache --pull\n",
			want:     1,
			defined:  true,
		},
		{
			name:     "docker rmi pre-clean fails",
			makefile: "integration-test:\n\tdocker rmi -f proj-test-node1 2>/dev/null || true; \\\n\t$(DOCKER_COMPOSE) build\n",
			want:     1,
			defined:  true,
		},
		{
			name:     "DOCKER variable rmi fails",
			makefile: "integration-test:\n\t$(DOCKER) rmi -f proj-test-node1\n",
			want:     1,
			defined:  true,
		},
		{
			name:     "recipe ends at the next target",
			makefile: "integration-test:\n\t$(DOCKER_COMPOSE) build\n\n.PHONY: soak\nsoak:\n\t$(DOCKER_COMPOSE) build --no-cache\n",
			want:     0,
			defined:  true,
		},
		{
			name:     "missing target has no recipe",
			makefile: "soak:\n\t$(DOCKER_COMPOSE) build --no-cache\n",
			want:     0,
			defined:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recipe := makeRecipeLines(tt.makefile, integrationTarget)
			if got := recipe != nil; got != tt.defined {
				t.Fatalf("recipe defined = %v, want %v", got, tt.defined)
			}
			if got := cacheDefeats(recipe); len(got) != tt.want {
				t.Errorf("cacheDefeats = %q, want %d lines", got, tt.want)
			}
		})
	}
}
