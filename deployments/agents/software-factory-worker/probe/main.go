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

// Command probe runs the Software Factory worker image's probes against one
// image and exits non-zero if any fails. It is what `make test-worker-image`
// runs, and it runs the same probes (softwarefactoryworker.Probes) through the
// same runner (workerimage.RunProbes) the daemon's refresh gate uses, so an
// image certified by hand and an image promoted by the factory pass one bar.
//
// The toolchain pins it checks against are the repository's own
// (pkg/version/toolchain.mk), exactly as the daemon's are.
//
//	go run ./deployments/agents/software-factory-worker/probe -image quicbaas-software-factory-worker:<tag> \
//	    -base 0.11.0-alpha -claude-code 2.1.280 -codex 0.156.0 -gemini 0.64.0
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	softwarefactoryworker "github.com/jeremyhahn/go-quicbaas/deployments/agents/software-factory-worker"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc/workerimage"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main with its outputs and exit status explicit.
func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("probe", flag.ContinueOnError)
	flags.SetOutput(stderr)
	image := flags.String("image", "", "image reference or ID to probe (required)")
	base := flags.String("base", "", "the QuicBaaS release the image was built from (required)")
	claude := flags.String("claude-code", "", "the Claude Code version the image must carry (required)")
	codex := flags.String("codex", "", "the Codex version the image must carry (required)")
	gemini := flags.String("gemini", "", "the Gemini CLI version the image must carry (required)")
	uid := flags.Int("uid", os.Getuid(), "uid the probes run as: the uid sandboxed spawns run as")
	gid := flags.Int("gid", os.Getgid(), "gid the probes run as")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	pins, err := softwarefactoryworker.ToolchainPins()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	build := softwarefactoryworker.Build{
		Base:   *base,
		Agents: softwarefactoryworker.Agents{ClaudeCode: *claude, Codex: *codex, Gemini: *gemini},
		Pins:   pins, UID: *uid, GID: *gid,
	}
	if err := build.Validate(); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	log, err := newProbeLogger()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	engine, err := orchestration.NewExecContainerRuntime(&logger.LoggerAdapter{Logger: log})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	results, err := workerimage.RunProbes(ctx, engine, *image, build)
	for _, r := range results {
		mark := "PASS"
		if !r.Passed {
			mark = "FAIL"
		}
		fmt.Fprintf(stdout, "%s  %s\n", mark, r.Name)
		if !r.Passed && r.Detail != "" {
			fmt.Fprintf(stdout, "      %s\n", r.Detail)
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if len(results) == 0 {
		fmt.Fprintln(stderr, "no probe ran")
		return 1
	}
	for _, r := range results {
		if !r.Passed {
			return 1
		}
	}
	return 0
}

// newProbeLogger is the engine's logger: warnings and worse, to stderr, so the
// probe verdicts on stdout stay readable.
func newProbeLogger() (*logger.Logger, error) {
	return logger.New(logger.LogConfig{Level: "warn", Format: "console", Output: "stderr", Component: "worker-image-probe"})
}
