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

// Package dockerinv derives every Docker image identity a repository creates
// under a name it writes literally.
//
// This is the authoritative answer to "what may `make clean-docker` remove?",
// and it is derived from the commands that CREATE the images rather than from
// the Makefile's DOCKER_LOCAL_IMAGES list. scripts/verifycleandocker compares
// the two, so a new image-producing target fails `make verify` until cleanup
// knows about it. Deriving independently is the point: reading the list under
// test cannot detect an omission from it.
//
// OWNERSHIP IS DERIVED FROM PRODUCERS ONLY — the commands that bring an image
// into existence. A removal command is not evidence of ownership: `docker rmi X`
// in a repository says only that something there deletes X, which is the very
// claim cleanup is being checked against. Deriving from removals would let any
// tag vouch for itself.
//
// Two sources, both literal:
//
//  1. The repository's own Docker commands in the Makefile and in every shell
//     script beneath it — the `-t` of every `docker build` / `buildx build` and
//     the destination of every `docker tag`. Backslash continuations are joined
//     first, so a build spread over several lines is one command here, and a
//     variable assigned a literal in that same file is resolved. The script
//     sweep is recursive on purpose: a producer added under `test/`,
//     `deployments/` or any other subdirectory is as real as one in `scripts/`,
//     and a scan that only looked at `scripts/*.sh` would let it escape both
//     cleanup and the verification.
//
//  2. The explicit `image:` tag of every Compose service that also has a
//     `build:` — the identity Compose gives the image it builds.
//
// Compose services *without* an explicit `image:` are deliberately not derived.
// Compose names those `<project>-<service>`, and the project name is the
// directory Compose ran in or a target's `-p` override — for the root
// docker-compose.yml it is the checkout directory's own name, which differs
// between a developer's clone and a CI workspace, and two files in one directory
// share a name. Reproducing that here would mean reimplementing Compose's
// project-name resolution over 29 compose files and their invocation sites, and
// the names it produces belong to whichever checkout on the host used them last
// rather than to this repository. Those stacks are torn down by the target that
// runs them, where the name is unambiguous because that target just used it.
//
// A GO PROGRAM THAT RUNS `docker build` IS NOT A SOURCE HERE. The producer
// sweep reads the Makefile and shell scripts, which is where every producer in
// this tree lives; a Go command that shelled out to Docker under a literal tag
// would escape it. Nothing does today, and the reason is recorded rather than
// assumed: adding one means either giving it a Makefile target that names the
// tag, or teaching this package a third source.
package dockerinv

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/scripts/internal/gate"
)

// UnresolvedTag is one producer whose image name this package could not reduce
// to a literal.
type UnresolvedTag struct {
	// Origin is the file the producer was read from.
	Origin string
	// Token is the argument as written.
	Token string
}

// UnresolvedTagsError reports producers whose image names are decided at run
// time. An image nobody can name cannot be cleaned up by name, so it fails the
// derivation rather than dropping out of it — otherwise an image that escapes
// cleanup reads downstream as "nothing missing".
type UnresolvedTagsError struct {
	Tags []UnresolvedTag
}

func (e *UnresolvedTagsError) Error() string {
	parts := make([]string, 0, len(e.Tags))
	for _, t := range e.Tags {
		parts = append(parts, t.Origin+": cannot resolve the image built by "+t.Token+" to a literal name")
	}
	return strings.Join(parts, "; ")
}

// ScanError reports a file the derivation could not read. It is fatal for the
// same reason an unresolved tag is: a producer that was not read is a producer
// that is not in the inventory.
type ScanError struct {
	Path string
	Err  error
}

func (e *ScanError) Error() string { return "cannot read " + e.Path + ": " + e.Err.Error() }

func (e *ScanError) Unwrap() error { return e.Err }

// makeVars are the Makefile variables a producer may name in place of a literal.
var makeVars = []string{"DOCKER_IMAGE", "DOCKER_TAG", "DOCKER_REGISTRY"}

var (
	// assignment matches a shell or make variable assignment token, `NAME=value`.
	assignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	// literalValue is the value shape safe to treat as an image-name fragment.
	literalValue = regexp.MustCompile(`^[A-Za-z0-9._/:-]+$`)
	// variableRef matches a token that is nothing but a variable reference:
	// $NAME, $$NAME, ${NAME}.
	variableRef = regexp.MustCompile(`^\$+\{?[A-Za-z_][A-Za-z0-9_]*\}?$`)
	// dockerWord matches the token that starts a docker command, including
	// `/usr/bin/docker` and the Makefile's own `$(DOCKER)`.
	dockerWord = regexp.MustCompile(`(^|[^A-Za-z0-9_-])docker$`)
	makeDocker = regexp.MustCompile(`\$\(DOCKER\)$`)
	// controlWord ends the current command: a pipe, an && or a background &.
	controlWord = regexp.MustCompile(`^(\|\||\||&&|&)$`)
	// commentLine is a line the shell and make both ignore.
	commentLine = regexp.MustCompile(`^[ \t]*#`)
	// fieldSep splits a line the way the producer scan reads it. A leading
	// separator yields an empty first field, which is what a Makefile recipe's
	// leading tab produces and is handled explicitly.
	fieldSep = regexp.MustCompile(`[ \t]+`)

	composeFile     = regexp.MustCompile(`^docker-compose.*\.ya?ml$`)
	composeServices = regexp.MustCompile(`^services:[ \t]*$`)
	composeTopLevel = regexp.MustCompile(`^[A-Za-z]`)
	composeService  = regexp.MustCompile(`^  [A-Za-z0-9_.-]+:[ \t]*$`)
	composeBuild    = regexp.MustCompile(`^    build:`)
	composeImage    = regexp.MustCompile(`^    image:[ \t]+`)
)

// prunedDirs are never a producer of this repository's images: vendored and
// installed trees answer for themselves.
var prunedDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
}

// Derive returns every image identity the repository at root creates, sorted and
// deduplicated. It returns an error when a producer's name cannot be resolved,
// when a file cannot be read, or when the Makefile variables a producer refers
// to cannot be read — all three being holes in the inventory rather than empty
// answers from it.
func Derive(root string) ([]string, error) {
	vars := make(map[string]string, len(makeVars))
	for _, name := range makeVars {
		value, err := gate.MakeVar(root, name)
		if err != nil {
			return nil, err
		}
		vars[name] = value
	}

	fromCommands, err := scanCommands(root, vars)
	if err != nil {
		return nil, err
	}
	fromCompose, err := scanCompose(root)
	if err != nil {
		return nil, err
	}

	return sortUnique(append(fromCommands, fromCompose...)), nil
}

// commandFiles returns every file under root that may hold one of the
// repository's own docker commands: the Makefile and every shell script beneath
// it. Paths are returned relative to root and sorted, so an error message names
// a producer the same way from any working directory.
func commandFiles(root string) ([]string, error) {
	var out []string
	if _, err := os.Stat(filepath.Join(root, "Makefile")); err == nil {
		out = append(out, "Makefile")
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && prunedDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".sh") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, &ScanError{Path: root, Err: err}
	}
	sort.Strings(out)
	return out, nil
}

// scanCommands derives the `-t` of every docker build and the destination of
// every docker tag across the repository's own command files.
func scanCommands(root string, vars map[string]string) ([]string, error) {
	files, err := commandFiles(root)
	if err != nil {
		return nil, err
	}

	var ids []string
	var unresolved []UnresolvedTag
	for _, rel := range files {
		raw, rerr := os.ReadFile(filepath.Join(root, rel))
		if rerr != nil {
			return nil, &ScanError{Path: rel, Err: rerr}
		}
		fileIDs, fileUnresolved := scanCommandFile(rel, string(raw), vars)
		ids = append(ids, fileIDs...)
		unresolved = append(unresolved, fileUnresolved...)
	}
	if len(unresolved) > 0 {
		return nil, &UnresolvedTagsError{Tags: unresolved}
	}
	return ids, nil
}

// scanCommandFile reads one command file. Continuations are joined first, so a
// build's `-t` and the variable assignment it refers to sit in the same record —
// which is also the shell's own view of them.
func scanCommandFile(origin, content string, vars map[string]string) ([]string, []UnresolvedTag) {
	records := joinContinuations(content)

	// A shell script assigns its variables in statements of their own, so the
	// literal assignments are collected file-wide and a record's own assignments
	// then win over that.
	fileLiterals := map[string]string{}
	for _, record := range records {
		collectAssignments(record, fileLiterals)
	}

	var ids []string
	var unresolved []UnresolvedTag
	for _, record := range records {
		literals := make(map[string]string, len(fileLiterals))
		for k, v := range fileLiterals {
			literals[k] = v
		}
		collectAssignments(record, literals)

		emit := func(token string) {
			id, ok := resolve(token, vars, literals)
			if !ok {
				unresolved = append(unresolved, UnresolvedTag{Origin: origin, Token: token})
				return
			}
			if !strings.Contains(id, ":") {
				id += ":latest"
			}
			ids = append(ids, id)
		}
		scanRecord(record, emit)
	}
	return ids, unresolved
}

// mode is where the scan is inside a docker command.
type mode int

const (
	modeNone mode = iota
	modeDocker
	modeBuild
	modeTag
)

// scanRecord walks one logical command, calling emit for each image identity the
// command creates.
func scanRecord(record string, emit func(string)) {
	if commentLine.MatchString(record) {
		return
	}
	tokens := fieldSep.Split(record, -1)

	current := modeNone
	positional := 0
	for i, token := range tokens {
		endsCommand := strings.HasSuffix(token, ";")
		token = strings.TrimSuffix(token, ";")

		if token == "" {
			if endsCommand {
				current = modeNone
			}
			continue
		}
		if controlWord.MatchString(token) {
			current = modeNone
			continue
		}
		if dockerWord.MatchString(token) || makeDocker.MatchString(token) {
			current = modeDocker
			positional = 0
			continue
		}

		switch current {
		case modeDocker:
			switch token {
			case "buildx":
				continue
			case "build":
				current = modeBuild
				continue
			case "tag":
				current = modeTag
				continue
			default:
				current = modeNone
				continue
			}
		case modeBuild:
			if token == "-t" && i+1 < len(tokens) {
				emit(tokens[i+1])
			}
		case modeTag:
			if strings.HasPrefix(token, "-") {
				continue
			}
			positional++
			if positional == 2 {
				emit(token)
				current = modeNone
			}
		case modeNone:
		}

		if endsCommand {
			current = modeNone
		}
	}
}

// joinContinuations folds a backslash-continued line into the record that
// follows it, exactly as the shell and make do: the backslash and the newline
// both disappear and nothing is inserted in their place.
func joinContinuations(content string) []string {
	lines := strings.Split(content, "\n")
	var records []string
	pending := ""
	continuing := false
	for _, line := range lines {
		if continuing {
			pending += line
		} else {
			pending = line
		}
		if strings.HasSuffix(pending, `\`) {
			pending = strings.TrimSuffix(pending, `\`)
			continuing = true
			continue
		}
		continuing = false
		records = append(records, pending)
		pending = ""
	}
	if continuing {
		records = append(records, pending)
	}
	return records
}

// collectAssignments records every `NAME=literal` in a record.
func collectAssignments(record string, into map[string]string) {
	if commentLine.MatchString(record) {
		return
	}
	for _, token := range fieldSep.Split(record, -1) {
		token = strings.TrimSuffix(token, ";")
		if !assignment.MatchString(token) {
			continue
		}
		eq := strings.Index(token, "=")
		value := unquote(token[eq+1:])
		if literalValue.MatchString(value) {
			into[token[:eq]] = value
		}
	}
}

// resolve reduces a producer's image argument to a literal, reporting false when
// it cannot. Make variables expand first, then a variable the file assigned a
// literal; a token that is still carrying a `$` names an image decided at run
// time, which the caller treats as a hole in the inventory.
func resolve(token string, vars, literals map[string]string) (string, bool) {
	id := unquote(token)
	for name, value := range vars {
		id = strings.ReplaceAll(id, "$("+name+")", value)
	}
	if variableRef.MatchString(id) {
		name := strings.NewReplacer("$", "", "{", "", "}", "").Replace(id)
		if value, ok := literals[name]; ok {
			id = value
		}
	}
	if strings.Contains(id, "$") {
		return "", false
	}
	return id, true
}

// unquote strips the shell quoting a producer may have written around an image
// name. Both quote characters are removed wherever they appear, because a name
// is a single word and any quote in it is syntax.
func unquote(s string) string {
	return strings.NewReplacer(`"`, "", `'`, "").Replace(s)
}

// scanCompose derives the explicit `image:` of every Compose service that also
// builds.
func scanCompose(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && d.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if composeFile.MatchString(d.Name()) {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, &ScanError{Path: root, Err: err}
	}
	sort.Strings(files)

	var ids []string
	for _, path := range files {
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil, &ScanError{Path: relativeTo(root, path), Err: rerr}
		}
		for _, id := range scanComposeFile(string(raw)) {
			if !strings.Contains(id, ":") {
				id += ":latest"
			}
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// scanComposeFile returns the explicit image identity of each service in one
// Compose file that carries a `build:`.
func scanComposeFile(content string) []string {
	var ids []string
	inServices := false
	builds := false
	image := ""

	flush := func() {
		if builds && image != "" {
			ids = append(ids, image)
		}
		builds = false
		image = ""
	}

	for _, line := range strings.Split(content, "\n") {
		switch {
		case composeServices.MatchString(line):
			inServices = true
		case composeTopLevel.MatchString(line):
			flush()
			inServices = false
		case !inServices:
		case composeService.MatchString(line):
			flush()
		default:
			if composeBuild.MatchString(line) {
				builds = true
			}
			if composeImage.MatchString(line) {
				fields := strings.Fields(line)
				if len(fields) > 1 {
					image = unquote(fields[1])
				}
			}
		}
	}
	flush()
	return ids
}

// sortUnique is the `sort -u` the two sources are merged through.
func sortUnique(in []string) []string {
	sort.Strings(in)
	out := in[:0]
	for i, v := range in {
		if i == 0 || in[i-1] != v {
			out = append(out, v)
		}
	}
	return out
}

// relativeTo names a path the way an operator sees it, falling back to the
// absolute path when it lies outside root. The fallback is a NAMING choice, not
// a swallowed failure: the caller is already reporting an error about this file
// and the absolute path identifies it just as well.
func relativeTo(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}
