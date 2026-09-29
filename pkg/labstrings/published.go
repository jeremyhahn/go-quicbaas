// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package labstrings

import (
	"bytes"
	"os/exec"
	"path"
	"strings"
)

// exportIgnoreAttr is the git attribute that keeps a tracked file out of every
// archive git builds from the tree: `git archive`, and the source tarballs a
// forge serves for a tag. .gitattributes is therefore the one exclusion list,
// and every published form of the tree applies it.
const exportIgnoreAttr = "export-ignore"

// developmentToolingNames are file names that belong to the private
// development workflow wherever they appear: per-directory agent instructions.
var developmentToolingNames = []string{"CLAUDE.md", "AGENTS.md"}

// developmentToolingDirs are directories, at any depth, that belong to the
// private development workflow: agent definitions, skills and working notes.
var developmentToolingDirs = []string{".claude", ".serena"}

// IsDevelopmentTooling reports whether a slash-separated, root-relative path is
// part of the private development workflow. Such files name the private forge
// and internal identities; they stay in the private repository and are never
// published.
func IsDevelopmentTooling(name string) bool {
	segments := strings.Split(name, "/")
	for _, base := range developmentToolingNames {
		if path.Base(name) == base {
			return true
		}
	}
	for _, segment := range segments[:len(segments)-1] {
		for _, dir := range developmentToolingDirs {
			if segment == dir {
				return true
			}
		}
	}
	return false
}

// PublishedFiles lists the tracked files that are published: the git index
// minus every path .gitattributes marks export-ignore. It is the manifest the
// releasability gate scans.
func PublishedFiles(root string) ([]string, error) {
	tracked, err := TrackedFiles(root)
	if err != nil {
		return nil, err
	}
	ignored, err := exportIgnored(root, tracked)
	if err != nil {
		return nil, err
	}
	published := make([]string, 0, len(tracked))
	for _, name := range tracked {
		if !ignored[name] {
			published = append(published, name)
		}
	}
	return published, nil
}

// exportIgnored asks git which of files carry the export-ignore attribute, so
// the answer follows git's own attribute rules rather than a copy of them.
func exportIgnored(root string, files []string) (map[string]bool, error) {
	var stdin bytes.Buffer
	for _, name := range files {
		stdin.WriteString(name)
		stdin.WriteByte(0)
	}
	cmd := exec.Command("git", "-C", root, "check-attr", "-z", "--stdin", exportIgnoreAttr)
	cmd.Stdin = &stdin
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, &ListError{Root: root, Stderr: strings.TrimSpace(stderr.String()), Err: err}
	}

	// -z output is NUL-separated triples: path, attribute, value.
	fields := strings.Split(string(out), "\x00")
	ignored := make(map[string]bool)
	for i := 0; i+2 < len(fields); i += 3 {
		if fields[i+2] == "set" {
			ignored[fields[i]] = true
		}
	}
	return ignored, nil
}
