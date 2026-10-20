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
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var devcontainerDockerfilePath = filepath.Join(repoRoot, "build", "docker", "devcontainer", "Dockerfile")

var (
	// fromLine is a stage header: `FROM <image or stage> [AS <name>]`.
	fromLine = regexp.MustCompile(`(?mi)^FROM\s+(\S+)(?:\s+AS\s+(\S+))?\s*$`)
	// xzExtraction is a tar run that decompresses with xz (-J).
	xzExtraction = regexp.MustCompile(`\btar\s+-[A-Za-z]*J`)
	// xzInstall is an apt-get install whose package list, continued over
	// backslash-newlines up to the next `;`, names xz-utils.
	xzInstall = regexp.MustCompile(`apt-get install[^;]*\bxz-utils\b`)
)

// dockerStage is one build stage: its name, the stage it builds FROM (empty
// for an image), and its instructions.
type dockerStage struct {
	name   string
	parent string
	body   string
}

func parseDockerStages(dockerfile string) []dockerStage {
	headers := fromLine.FindAllStringSubmatchIndex(dockerfile, -1)
	named := map[string]bool{}
	stages := make([]dockerStage, 0, len(headers))
	for i, h := range headers {
		end := len(dockerfile)
		if i+1 < len(headers) {
			end = headers[i+1][0]
		}
		from := dockerfile[h[2]:h[3]]
		stage := dockerStage{body: dockerfile[h[1]:end]}
		if h[4] >= 0 {
			stage.name = dockerfile[h[4]:h[5]]
		}
		if named[from] {
			stage.parent = from
		}
		named[stage.name] = true
		stages = append(stages, stage)
	}
	return stages
}

// stagesExtractingXzWithoutXz names each stage that runs `tar -xJ` with no
// xz-utils installed before it, in that stage or in a stage it builds FROM.
func stagesExtractingXzWithoutXz(dockerfile string) []string {
	stages := parseDockerStages(dockerfile)
	byName := map[string]dockerStage{}
	for _, s := range stages {
		byName[s.name] = s
	}
	var missing []string
	for _, s := range stages {
		at := xzExtraction.FindStringIndex(s.body)
		if at == nil {
			continue
		}
		if xzInstall.MatchString(s.body[:at[0]]) {
			continue
		}
		installed := false
		for p := s.parent; p != "" && !installed; p = byName[p].parent {
			installed = xzInstall.MatchString(byName[p].body)
		}
		if !installed {
			missing = append(missing, s.name)
		}
	}
	return missing
}

// The golang trixie base has no xz, so a stage that unpacks a .tar.xz fails
// with "xz: Cannot exec" unless it installs xz-utils first (#1581).
func TestDevcontainerStagesInstallXzBeforeTheyUnpackTarXz(t *testing.T) {
	if missing := stagesExtractingXzWithoutXz(readFile(t, devcontainerDockerfilePath)); len(missing) > 0 {
		t.Errorf("%s: stages %v run `tar -xJ` without installing xz-utils first", devcontainerDockerfilePath, missing)
	}
}

func TestStagesExtractingXzWithoutXzFindsAStageWithoutXz(t *testing.T) {
	const base = "FROM golang:1 AS base\nRUN apt-get install -y --no-install-recommends jq; \\\n    rm -rf /var/lib/apt/lists/*\n"
	cases := map[string]struct {
		dockerfile string
		want       []string
	}{
		"no xz anywhere": {
			dockerfile: base + "FROM base AS tools\nRUN tar -xJf /tmp/a.tar.xz -C /tmp\n",
			want:       []string{"tools"},
		},
		"xz only in a sibling stage": {
			dockerfile: base + "FROM base AS tools\nRUN tar -xJf /tmp/a.tar.xz\n" +
				"FROM base AS toolchain\nRUN apt-get install -y xz-utils\n",
			want: []string{"tools"},
		},
		"xz installed after the extraction": {
			dockerfile: base + "FROM base AS tools\nRUN tar -xJf /tmp/a.tar.xz\nRUN apt-get install -y xz-utils\n",
			want:       []string{"tools"},
		},
		"xz installed in the stage first": {
			dockerfile: base + "FROM base AS tools\nRUN apt-get install -y --no-install-recommends \\\n    curl xz-utils; \\\n" +
				"    rm -rf /var/lib/apt/lists/*\nRUN tar -xJf /tmp/a.tar.xz\n",
		},
		"xz installed in an ancestor": {
			dockerfile: "FROM golang:1 AS base\nRUN apt-get install -y xz-utils\nFROM base AS tools\nRUN tar -xJf /tmp/a.tar.xz\n",
		},
		"no xz extraction": {
			dockerfile: base + "FROM base AS tools\nRUN tar -xzf /tmp/a.tar.gz\n",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := stagesExtractingXzWithoutXz(tc.dockerfile)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
