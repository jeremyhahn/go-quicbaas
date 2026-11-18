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

package fleet

import (
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// containerRuntimeFactory builds container runtimes for one host. It exists so
// the selector table can be exercised without docker or podman installed: the
// production factory shells out to the real engines, a test supplies its own.
type containerRuntimeFactory struct {
	// withCommand builds a runtime bound to a named engine command, failing
	// when that engine is not usable on this host.
	withCommand func(command string, logger interfaces.Logger) (orchestration.ContainerRuntime, error)

	// detect probes the engines this host has, in preference order, and
	// fails when it finds none.
	detect func(logger interfaces.Logger) (orchestration.ContainerRuntime, error)

	// inProcess builds the in-process runtime, or is nil when this binary
	// has none (see SetInProcessContainerRuntime).
	inProcess func(logger interfaces.Logger) (orchestration.ContainerRuntime, error)
}

// InProcessContainerRuntime builds the in-process container runtime with all
// of its state under root, a directory no other runtime uses.
type InProcessContainerRuntime func(root string, logger interfaces.Logger) (orchestration.ContainerRuntime, error)

// inProcessContainerRuntime is the registered constructor, nil until the
// agent's main registers one. pkg/fleet never imports pkg/containerruntime
// itself: quicbaasd links this package and must stay CGO_ENABLED=0.
var inProcessContainerRuntime atomic.Pointer[InProcessContainerRuntime]

// SetInProcessContainerRuntime registers the in-process runtime the
// "libcontainer" selector resolves to; "auto" never takes it. Only the fleet node agent,
// built for linux with cgo, registers one; nil unregisters it.
func SetInProcessContainerRuntime(build InProcessContainerRuntime) {
	if build == nil {
		inProcessContainerRuntime.Store(nil)
		return
	}
	inProcessContainerRuntime.Store(&build)
}

// ErrInProcessRuntimeUnavailable reports the "libcontainer" selector on a
// binary without the in-process runtime: not the agent, or not linux with cgo.
var ErrInProcessRuntimeUnavailable = errors.New("agent: the in-process container runtime is not built into this binary")

// ErrInProcessRuntimeNoRoot reports an in-process runtime asked for with no
// directory to keep its state in.
var ErrInProcessRuntimeNoRoot = errors.New("agent: the in-process container runtime needs a state directory")

// withInProcess returns factory with the registered in-process runtime bound
// to root, or factory unchanged when none is registered.
func (factory containerRuntimeFactory) withInProcess(root string) containerRuntimeFactory {
	build := inProcessContainerRuntime.Load()
	if build == nil {
		return factory
	}
	factory.inProcess = func(logger interfaces.Logger) (orchestration.ContainerRuntime, error) {
		if root == "" {
			return nil, ErrInProcessRuntimeNoRoot
		}
		return (*build)(root, logger)
	}
	return factory
}

// execContainerRuntimeFactory is the production factory: it resolves to
// pkg/orchestration's exec-based docker/podman driver.
var execContainerRuntimeFactory = containerRuntimeFactory{
	withCommand: func(command string, logger interfaces.Logger) (orchestration.ContainerRuntime, error) {
		rt, err := orchestration.NewExecContainerRuntimeWithCommand(command, logger)
		if err != nil {
			return nil, err
		}
		return rt, nil
	},
	detect: func(logger interfaces.Logger) (orchestration.ContainerRuntime, error) {
		rt, err := orchestration.NewExecContainerRuntime(logger)
		if err != nil {
			return nil, err
		}
		return rt, nil
	},
}

// ResolveContainerRuntime turns the operator's --container-runtime selector
// into the runtime the daemon hands to NewRuntime for sandboxed sessions.
//
//   - "docker" / "podman" bind that engine specifically. When the named engine
//     is not usable on this host the daemon degrades to no runtime, logging why:
//     under sandbox policy "preferred" or "opt-in" that is a serviceable daemon,
//     and under "required" NewRuntime refuses to start on the operator's behalf.
//   - "libcontainer" is the in-process runtime. A binary without one, or one
//     that cannot start here, is an error: the operator asked for it by name.
//   - "auto" (and the empty selector) take the first engine detection finds,
//     and resolve to no runtime when the host has none. They never take the
//     in-process runtime, which is opt-in by name until it has networking
//     and seccomp/AppArmor confinement.
//   - "none" resolves to no runtime without probing anything.
//
// ResolveContainerRuntime resolves without a state directory, so its
// in-process runtime is never chosen; the agent resolves through
// ResolveContainerRuntimeIn.
//
// The outcome is logged at INFO because it decides whether this daemon can run
// a container at all, and an operator must be able to read that from the boot
// log rather than from the first refused spawn.
//
// An unknown selector is a typed error: it is a misconfiguration, and guessing
// on the operator's behalf is how a daemon ends up sandboxing nothing.
func ResolveContainerRuntime(selector ContainerRuntimeSelector, logger interfaces.Logger) (orchestration.ContainerRuntime, error) {
	return resolveContainerRuntime(selector, logger, execContainerRuntimeFactory)
}

// ResolveContainerRuntimeIn is ResolveContainerRuntime with root as the
// in-process runtime's state directory. root must belong to this agent alone:
// two runtimes never share one.
func ResolveContainerRuntimeIn(selector ContainerRuntimeSelector, root string, logger interfaces.Logger) (orchestration.ContainerRuntime, error) {
	return resolveContainerRuntime(selector, logger, execContainerRuntimeFactory.withInProcess(root))
}

// containerRuntimeResolver builds the runtime for one selector value.
type containerRuntimeResolver func(logger interfaces.Logger, factory containerRuntimeFactory) (orchestration.ContainerRuntime, error)

// containerRuntimeResolvers dispatches a selector to its resolver in constant
// time. Every value Config.Validate accepts has an entry here; a selector that
// is missing is by construction one Validate rejected.
var containerRuntimeResolvers = map[ContainerRuntimeSelector]containerRuntimeResolver{
	ContainerRuntimeNone:      resolveNoContainerRuntime,
	ContainerRuntimeInProcess: resolveInProcessContainerRuntime,
	ContainerRuntimeAuto:      resolveDetectedContainerRuntime,
	ContainerRuntimeDocker:    resolveNamedContainerRuntime(orchestration.RuntimeDocker),
	ContainerRuntimePodman:    resolveNamedContainerRuntime(orchestration.RuntimePodman),
}

// resolveContainerRuntime is ResolveContainerRuntime with the factory injected.
func resolveContainerRuntime(selector ContainerRuntimeSelector, logger interfaces.Logger, factory containerRuntimeFactory) (orchestration.ContainerRuntime, error) {
	if logger == nil {
		return nil, ErrRuntimeNoLogger
	}
	if selector == "" {
		selector = ContainerRuntimeAuto
	}
	resolve, ok := containerRuntimeResolvers[selector]
	if !ok {
		return nil, &SentinelDetailError{
			Sentinel: ErrConfigUnknownRuntime,
			Detail:   fmt.Sprintf("%q", selector),
		}
	}
	return resolve(logger, factory)
}

// resolveNoContainerRuntime honors an explicit "none": the daemon spawns
// nothing in a container, and says so.
func resolveNoContainerRuntime(logger interfaces.Logger, _ containerRuntimeFactory) (orchestration.ContainerRuntime, error) {
	logger.Infof("container runtime: none (disabled by configuration)")
	return nil, nil
}

// resolveDetectedContainerRuntime implements "auto": first engine found wins,
// none found is a runtime-less daemon rather than a failure.
//
// It never takes the in-process runtime: that one has no container networking
// and no seccomp or AppArmor confinement yet, so an operator opts into it by
// naming "libcontainer".
func resolveDetectedContainerRuntime(logger interfaces.Logger, factory containerRuntimeFactory) (orchestration.ContainerRuntime, error) {
	rt, err := factory.detect(logger)
	if err != nil {
		logger.Infof("container runtime: none (auto-detection found no engine: %v)", err)
		return nil, nil
	}
	logger.Infof("container runtime: %s (available)", containerRuntimeName(rt))
	return rt, nil
}

// resolveInProcessContainerRuntime is the "libcontainer" selector: the
// in-process runtime, or an error naming why there is none.
func resolveInProcessContainerRuntime(logger interfaces.Logger, factory containerRuntimeFactory) (orchestration.ContainerRuntime, error) {
	if factory.inProcess == nil {
		return nil, ErrInProcessRuntimeUnavailable
	}
	rt, err := factory.inProcess(logger)
	if err != nil {
		return nil, fmt.Errorf("container runtime libcontainer: %w", err)
	}
	logger.Infof("container runtime: libcontainer (in-process)")
	return rt, nil
}

// resolveNamedContainerRuntime builds the resolver for an explicitly named
// engine. Its unavailability is reported at INFO with the reason, because the
// operator named it and needs to know it did not take.
func resolveNamedContainerRuntime(command string) containerRuntimeResolver {
	return func(logger interfaces.Logger, factory containerRuntimeFactory) (orchestration.ContainerRuntime, error) {
		rt, err := factory.withCommand(command, logger)
		if err != nil {
			logger.Infof("container runtime: none (%s selected but unavailable: %v)", command, err)
			return nil, nil
		}
		logger.Infof("container runtime: %s (available)", containerRuntimeName(rt))
		return rt, nil
	}
}

// commandNamer is implemented by container runtimes that are backed by a named
// engine command, which is what an operator reading the boot log wants to see.
type commandNamer interface {
	Command() string
}

// containerRuntimeName reports the engine command behind a runtime for logging,
// falling back to a generic label for drivers that do not name a command.
func containerRuntimeName(rt orchestration.ContainerRuntime) string {
	if named, ok := rt.(commandNamer); ok {
		return named.Command()
	}
	return "configured"
}
