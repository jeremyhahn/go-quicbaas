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

package containerruntime

import (
	"context"
	"fmt"
	"maps"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// The build planner reads parsed instructions against the build arguments
// and the base images' configurations, and resolves everything a runner
// needs before it starts a container: each stage's base, its ARG and ENV
// scope, user, working directory, shell, entrypoint, command and labels,
// and its RUN and COPY steps with their environment and paths. It touches
// no storage: the base image lookup is injected.

// defaultShell is the shell a stage starts with until a SHELL replaces it.
var defaultShell = []string{"/bin/sh", "-c"}

// buildPlanOptions are a build's inputs besides its Dockerfile.
type buildPlanOptions struct {
	Args   map[string]string // --build-arg values
	Labels map[string]string // labels on top of the Dockerfile's LABELs, final stage only
}

// buildPlan is a Dockerfile read against its build arguments and base
// image configurations.
type buildPlan struct {
	Stages     []buildStage // every stage, in Dockerfile order; the last is the image
	UnusedArgs []string     // sorted build arguments no ARG declares
}

// buildStage is one FROM and the steps after it.
type buildStage struct {
	Index     int
	Name      string // the AS name, lower-cased; "" when unnamed
	Base      string // the expanded FROM reference; "" for scratch or an earlier stage
	BaseStage int    // the earlier stage it builds on, or -1
	Steps     []buildStep
	Config    imageConfig // the stage's configuration after its last instruction
}

// buildStep is one RUN or COPY, with everything it needs already resolved.
type buildStep struct {
	Line    int
	Command string // "RUN" or "COPY"

	// RUN
	Argv    []string // the process to run
	Env     []string // "KEY=value": the stage's environment, then its ARGs in scope
	User    string
	WorkDir string // absolute; the runner creates it before the step

	// COPY
	FromStage int      // the earlier stage copied from, or -1 for a heredoc
	Sources   []string // paths in FromStage's filesystem
	Content   string   // the heredoc body, expanded when its delimiter was unquoted
	Dest      string
	Chmod     string // octal, or ""
	Chown     string // expanded, or ""
}

// planArg is an ARG in scope and its value.
type planArg struct {
	name  string
	value string
}

// dockerfilePlanner is planDockerfile's working state.
type dockerfilePlanner struct {
	ctx  context.Context
	opts buildPlanOptions
	base func(context.Context, string) (imageConfig, error)

	bases    map[string]imageConfig // base configurations by reference
	global   map[string]string      // global ARGs that have a value
	declared map[string]bool        // every name an ARG declares
	plan     buildPlan
	shells   [][]string // each stage's current shell

	// The current stage's scope.
	args   []planArg
	cmdSet bool
}

// planDockerfile plans instructions. base returns the configuration of an
// image a FROM names; it is called once per distinct reference.
func planDockerfile(ctx context.Context, instructions []dockerfileInstruction,
	opts buildPlanOptions, base func(context.Context, string) (imageConfig, error)) (*buildPlan, error) {
	p := &dockerfilePlanner{
		ctx: ctx, opts: opts, base: base,
		bases:    map[string]imageConfig{},
		global:   map[string]string{},
		declared: map[string]bool{},
	}
	for i := range instructions {
		if err := p.instruction(&instructions[i]); err != nil {
			return nil, err
		}
	}
	if len(p.plan.Stages) == 0 {
		return nil, &DockerfileError{Message: "no FROM instruction"}
	}
	if len(opts.Labels) > 0 {
		last := p.stage()
		if last.Config.Labels == nil {
			last.Config.Labels = map[string]string{}
		}
		maps.Copy(last.Config.Labels, opts.Labels)
	}
	for name := range opts.Args {
		if !p.declared[name] {
			p.plan.UnusedArgs = append(p.plan.UnusedArgs, name)
		}
	}
	sort.Strings(p.plan.UnusedArgs)
	return &p.plan, nil
}

// refuse is a DockerfileError on line.
func refuse(line int, format string, a ...any) error {
	return &DockerfileError{Line: line, Message: fmt.Sprintf(format, a...)}
}

func (p *dockerfilePlanner) instruction(in *dockerfileInstruction) error {
	if err := checkForm(in); err != nil {
		return err
	}
	if in.Command == "FROM" {
		return p.from(in)
	}
	if len(p.plan.Stages) == 0 {
		if in.Command == "ARG" {
			return p.globalArg(in)
		}
		return refuse(in.Line, "%s before the first FROM", in.Command)
	}
	switch in.Command {
	case "ARG":
		return p.stageArg(in)
	case "ENV":
		return p.env(in)
	case "LABEL":
		return p.label(in)
	case "USER", "WORKDIR":
		return p.userOrWorkDir(in)
	case "SHELL":
		if !in.JSON || len(in.Args) == 0 {
			return refuse(in.Line, "SHELL takes a non-empty JSON list")
		}
		p.shells[len(p.shells)-1] = slices.Clone(in.Args)
		return nil
	case "RUN":
		return p.run(in)
	case "CMD", "ENTRYPOINT":
		p.cmdOrEntrypoint(in)
		return nil
	case "COPY":
		return p.copy(in)
	}
	return refuse(in.Line, "%s is not supported by the in-process build", in.Command)
}

// checkForm refuses flags, JSON lists and heredocs on the instructions
// that do not take them. COPY checks its own flags.
func checkForm(in *dockerfileInstruction) error {
	if len(in.Flags) > 0 && in.Command != "COPY" {
		return refuse(in.Line, "%s --%s is not supported by the in-process build", in.Command, in.Flags[0].Name)
	}
	switch in.Command {
	case "RUN", "COPY":
		return nil
	case "CMD", "ENTRYPOINT", "SHELL":
	default:
		if in.JSON {
			return refuse(in.Line, "%s does not take a JSON list", in.Command)
		}
	}
	if len(in.Heredocs) > 0 {
		return refuse(in.Line, "%s does not take a heredoc", in.Command)
	}
	return nil
}

// stage is the current stage.
func (p *dockerfilePlanner) stage() *buildStage {
	return &p.plan.Stages[len(p.plan.Stages)-1]
}

// shell is the current stage's shell.
func (p *dockerfilePlanner) shell() []string {
	return p.shells[len(p.shells)-1]
}

// globalLookup expands FROM lines: only the global ARGs are visible.
func (p *dockerfilePlanner) globalLookup(name string) (string, bool) {
	v, ok := p.global[name]
	return v, ok
}

// lookup expands inside a stage: its ENV first, then its ARGs in scope.
func (p *dockerfilePlanner) lookup(name string) (string, bool) {
	if v, ok := envValue(p.stage().Config.Env, name); ok {
		return v, true
	}
	for _, a := range p.args {
		if a.name == name {
			return a.value, true
		}
	}
	return "", false
}

// expandError is an expansion failure on in's line.
func expandError(in *dockerfileInstruction, err error) error {
	return refuse(in.Line, "%s: %v", in.Command, err)
}

// argDecl is one NAME or NAME=default an ARG declares, its default
// already expanded.
type argDecl struct {
	name, def  string
	hasDefault bool
}

func argDecls(in *dockerfileInstruction, lookup func(string) (string, bool)) ([]argDecl, error) {
	words, err := expandWords(in.Args[0], lookup)
	if err != nil {
		return nil, expandError(in, err)
	}
	var decls []argDecl
	for _, word := range words {
		name, def, ok := strings.Cut(word, "=")
		if name == "" {
			return nil, refuse(in.Line, "ARG %q has no name", word)
		}
		decls = append(decls, argDecl{name: name, def: def, hasDefault: ok})
	}
	return decls, nil
}

// globalArg is an ARG before the first FROM.
func (p *dockerfilePlanner) globalArg(in *dockerfileInstruction) error {
	decls, err := argDecls(in, p.globalLookup)
	if err != nil {
		return err
	}
	for _, d := range decls {
		p.declared[d.name] = true
		if v, ok := p.opts.Args[d.name]; ok {
			p.global[d.name] = v
		} else if d.hasDefault {
			p.global[d.name] = d.def
		}
	}
	return nil
}

// stageArg is an ARG inside a stage.
func (p *dockerfilePlanner) stageArg(in *dockerfileInstruction) error {
	decls, err := argDecls(in, p.lookup)
	if err != nil {
		return err
	}
	for _, d := range decls {
		p.declared[d.name] = true
		value, ok := p.opts.Args[d.name]
		switch {
		case ok:
		case d.hasDefault:
			value = d.def
		default:
			value = p.global[d.name]
		}
		p.setArg(d.name, value)
	}
	return nil
}

func (p *dockerfilePlanner) setArg(name, value string) {
	for i := range p.args {
		if p.args[i].name == name {
			p.args[i].value = value
			return
		}
	}
	p.args = append(p.args, planArg{name: name, value: value})
}

// from starts a stage.
func (p *dockerfilePlanner) from(in *dockerfileInstruction) error {
	words, err := expandWords(in.Args[0], p.globalLookup)
	if err != nil {
		return expandError(in, err)
	}
	var ref, name string
	switch {
	case len(words) == 0 || words[0] == "":
		return refuse(in.Line, "FROM names no image")
	case len(words) == 1:
		ref = words[0]
	case len(words) == 3 && strings.EqualFold(words[1], "AS"):
		ref, name = words[0], strings.ToLower(words[2])
	default:
		return refuse(in.Line, "FROM takes an image and an optional AS name")
	}
	if name != "" {
		if _, err := strconv.Atoi(name); err == nil {
			return refuse(in.Line, "stage name %s is a number", name)
		}
		if p.stageIndex(name) >= 0 {
			return refuse(in.Line, "stage name %s is used twice", name)
		}
	}

	stage := buildStage{Index: len(p.plan.Stages), Name: name, BaseStage: -1}
	shell := defaultShell
	if idx := p.stageIndex(strings.ToLower(ref)); idx >= 0 {
		stage.BaseStage = idx
		stage.Config = cloneImageConfig(p.plan.Stages[idx].Config)
		shell = p.shells[idx]
	} else if !strings.EqualFold(ref, "scratch") {
		cfg, err := p.baseConfig(in.Line, ref)
		if err != nil {
			return err
		}
		stage.Base = ref
		stage.Config = cloneImageConfig(cfg)
	}
	p.plan.Stages = append(p.plan.Stages, stage)
	p.shells = append(p.shells, shell)
	p.args, p.cmdSet = nil, false
	return nil
}

// stageIndex is the index of the earlier stage named name, or -1.
func (p *dockerfilePlanner) stageIndex(name string) int {
	for i := range p.plan.Stages {
		if p.plan.Stages[i].Name != "" && p.plan.Stages[i].Name == name {
			return i
		}
	}
	return -1
}

// baseConfig is ref's configuration, looked up once per reference.
func (p *dockerfilePlanner) baseConfig(line int, ref string) (imageConfig, error) {
	if cfg, ok := p.bases[ref]; ok {
		return cfg, nil
	}
	cfg, err := p.base(p.ctx, ref)
	if err != nil {
		return imageConfig{}, fmt.Errorf("dockerfile line %d: base image %s: %w", line, ref, err)
	}
	p.bases[ref] = cloneImageConfig(cfg)
	return p.bases[ref], nil
}

func cloneImageConfig(c imageConfig) imageConfig {
	return imageConfig{
		Entrypoint: slices.Clone(c.Entrypoint),
		Cmd:        slices.Clone(c.Cmd),
		Env:        slices.Clone(c.Env),
		WorkingDir: c.WorkingDir,
		User:       c.User,
		Labels:     maps.Clone(c.Labels),
	}
}

// envValue is name's value in env.
func envValue(env []string, name string) (string, bool) {
	for _, e := range env {
		if k, v, _ := strings.Cut(e, "="); k == name {
			return v, true
		}
	}
	return "", false
}

// setEnv replaces name's entry in env in place, or appends one.
func setEnv(env []string, name, value string) []string {
	for i, e := range env {
		if k, _, _ := strings.Cut(e, "="); k == name {
			env[i] = name + "=" + value
			return env
		}
	}
	return append(env, name+"="+value)
}

// pairs reads the K=V words of an ENV or LABEL.
func (p *dockerfilePlanner) pairs(in *dockerfileInstruction) ([][2]string, error) {
	words, err := expandWords(in.Args[0], p.lookup)
	if err != nil {
		return nil, expandError(in, err)
	}
	pairs := make([][2]string, 0, len(words))
	for _, word := range words {
		k, v, ok := strings.Cut(word, "=")
		if !ok {
			return nil, refuse(in.Line, "%s %s has no =", in.Command, word)
		}
		if k == "" {
			return nil, refuse(in.Line, "%s %s has no name", in.Command, word)
		}
		pairs = append(pairs, [2]string{k, v})
	}
	return pairs, nil
}

func (p *dockerfilePlanner) env(in *dockerfileInstruction) error {
	raw := in.Args[0]
	var pairs [][2]string
	if first := strings.Fields(raw)[0]; strings.Contains(first, "=") {
		var err error
		if pairs, err = p.pairs(in); err != nil {
			return err
		}
	} else {
		rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), first))
		if rest == "" {
			return refuse(in.Line, "ENV %s has no value", first)
		}
		value, err := expandWord(rest, p.lookup)
		if err != nil {
			return expandError(in, err)
		}
		pairs = [][2]string{{first, value}}
	}
	cfg := &p.stage().Config
	for _, kv := range pairs {
		cfg.Env = setEnv(cfg.Env, kv[0], kv[1])
	}
	return nil
}

func (p *dockerfilePlanner) label(in *dockerfileInstruction) error {
	pairs, err := p.pairs(in)
	if err != nil {
		return err
	}
	cfg := &p.stage().Config
	if cfg.Labels == nil {
		cfg.Labels = map[string]string{}
	}
	for _, kv := range pairs {
		cfg.Labels[kv[0]] = kv[1]
	}
	return nil
}

func (p *dockerfilePlanner) userOrWorkDir(in *dockerfileInstruction) error {
	value, err := expandWord(in.Args[0], p.lookup)
	if err != nil {
		return expandError(in, err)
	}
	cfg := &p.stage().Config
	if in.Command == "USER" {
		cfg.User = value
		return nil
	}
	if !path.IsAbs(value) {
		value = path.Join(p.workDir(), value)
	}
	cfg.WorkingDir = path.Clean(value)
	return nil
}

// workDir is the current working directory, "/" when unset.
func (p *dockerfilePlanner) workDir() string {
	if wd := p.stage().Config.WorkingDir; wd != "" {
		return wd
	}
	return "/"
}

// resolveDest joins a relative destination onto the working directory,
// keeping a trailing "/" that marks it a directory.
func (p *dockerfilePlanner) resolveDest(dest string) string {
	if path.IsAbs(dest) {
		return dest
	}
	joined := path.Join(p.workDir(), dest)
	if strings.HasSuffix(dest, "/") && joined != "/" {
		joined += "/"
	}
	return joined
}

// shellForm is the current shell running text.
func (p *dockerfilePlanner) shellForm(text string) []string {
	return append(slices.Clone(p.shell()), text)
}

func (p *dockerfilePlanner) run(in *dockerfileInstruction) error {
	if len(in.Flags) > 0 {
		return refuse(in.Line, "RUN --%s is not supported by the in-process build", in.Flags[0].Name)
	}
	var argv []string
	switch {
	case in.JSON:
		argv = slices.Clone(in.Args)
	case len(in.Heredocs) > 0:
		raw := strings.TrimSpace(in.Args[0])
		loc := heredocPattern.FindStringIndex(raw)
		if len(in.Heredocs) != 1 || loc == nil || loc[0] != 0 || loc[1] != len(raw) {
			return refuse(in.Line, "RUN takes a heredoc only as its whole script")
		}
		argv = p.shellForm(in.Heredocs[0].Body)
	default:
		argv = p.shellForm(in.Args[0])
	}
	cfg := p.stage().Config
	env := slices.Clone(cfg.Env)
	for _, a := range p.args {
		if _, set := envValue(cfg.Env, a.name); !set {
			env = append(env, a.name+"="+a.value)
		}
	}
	p.stage().Steps = append(p.stage().Steps, buildStep{
		Line: in.Line, Command: "RUN", Argv: argv, Env: env,
		User: cfg.User, WorkDir: p.workDir(), FromStage: -1,
	})
	return nil
}

func (p *dockerfilePlanner) cmdOrEntrypoint(in *dockerfileInstruction) {
	list := []string{}
	if in.JSON {
		list = append(list, in.Args...)
	} else {
		list = p.shellForm(in.Args[0])
	}
	cfg := &p.stage().Config
	if in.Command == "CMD" {
		cfg.Cmd, p.cmdSet = list, true
		return
	}
	cfg.Entrypoint = list
	if !p.cmdSet {
		cfg.Cmd = nil
	}
}

// copyFlags reads COPY's --chmod and --chown into step and returns --from.
func (p *dockerfilePlanner) copyFlags(in *dockerfileInstruction, step *buildStep) (from string, hasFrom bool, err error) {
	for _, f := range in.Flags {
		switch f.Name {
		case "from":
			from, hasFrom = f.Value, true
		case "chmod":
			mode, perr := strconv.ParseUint(f.Value, 8, 32)
			if (len(f.Value) != 3 && len(f.Value) != 4) || perr != nil || mode > 0o7777 {
				return "", false, refuse(in.Line, "COPY --chmod=%s is not a 3 or 4 digit octal mode", f.Value)
			}
			step.Chmod = f.Value
		case "chown":
			if step.Chown, err = expandWord(f.Value, p.lookup); err != nil {
				return "", false, expandError(in, err)
			}
		default:
			return "", false, refuse(in.Line, "COPY --%s is not supported by the in-process build", f.Name)
		}
	}
	return from, hasFrom, nil
}

func (p *dockerfilePlanner) copy(in *dockerfileInstruction) error {
	step := buildStep{Line: in.Line, Command: "COPY", FromStage: -1}
	from, hasFrom, err := p.copyFlags(in, &step)
	if err != nil {
		return err
	}
	switch {
	case hasFrom && len(in.Heredocs) > 0:
		return refuse(in.Line, "COPY --from cannot also copy a heredoc")
	case hasFrom:
		err = p.copyFromStage(in, from, &step)
	case len(in.Heredocs) > 0:
		err = p.copyHeredoc(in, &step)
	default:
		return refuse(in.Line, "COPY from the build context is not supported: the build context is empty")
	}
	if err != nil {
		return err
	}
	p.stage().Steps = append(p.stage().Steps, step)
	return nil
}

func (p *dockerfilePlanner) copyFromStage(in *dockerfileInstruction, from string, step *buildStep) error {
	current := len(p.plan.Stages) - 1
	name, err := expandWord(from, p.lookup)
	if err != nil {
		return expandError(in, err)
	}
	idx := p.stageIndex(strings.ToLower(name))
	if idx < 0 {
		if n, err := strconv.Atoi(name); err == nil && n >= 0 {
			idx = n
		}
	}
	if idx < 0 || idx >= current {
		return refuse(in.Line, "COPY --from=%s names no earlier stage: the build copies only from earlier stages", from)
	}

	var words []string
	if in.JSON {
		for _, a := range in.Args {
			word, err := expandWord(a, p.lookup)
			if err != nil {
				return expandError(in, err)
			}
			words = append(words, word)
		}
	} else if words, err = expandWords(in.Args[0], p.lookup); err != nil {
		return expandError(in, err)
	}
	if len(words) < 2 {
		return refuse(in.Line, "COPY needs at least one source and a destination")
	}
	step.FromStage = idx
	step.Sources = words[:len(words)-1]
	step.Dest = p.resolveDest(words[len(words)-1])
	return nil
}

func (p *dockerfilePlanner) copyHeredoc(in *dockerfileInstruction, step *buildStep) error {
	if len(in.Heredocs) != 1 || in.JSON {
		return refuse(in.Line, "COPY takes exactly one heredoc")
	}
	raw := in.Args[0]
	loc := heredocPattern.FindStringIndex(raw)
	if loc == nil {
		return refuse(in.Line, "COPY takes exactly one heredoc")
	}
	dest, err := expandWord(strings.TrimSpace(raw[:loc[0]]+raw[loc[1]:]), p.lookup)
	if err != nil {
		return expandError(in, err)
	}
	if dest == "" {
		return refuse(in.Line, "COPY heredoc has no destination")
	}
	heredoc := in.Heredocs[0]
	if strings.HasSuffix(dest, "/") {
		dest += heredoc.Name
	}
	step.Dest = p.resolveDest(dest)
	step.Content = heredoc.Body
	if heredoc.Expand {
		if step.Content, err = expandHeredoc(heredoc.Body, p.lookup); err != nil {
			return expandError(in, err)
		}
	}
	return nil
}
