# Copyright (c) 2026 Jeremy Hahn
# Copyright (c) 2026 Automate The Things, LLC
#
# This file is part of go-quicbaas.
#
# go-quicbaas is dual-licensed:
#
# 1. GNU Affero General Public License v3.0 (AGPL-3.0)
#    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
#
# 2. Commercial License
#    Contact licensing@automatethethings.com for commercial licensing options.

"""Prove a worker container's pids limit holds and fails cleanly (#1040).

`make test-worker-image` pipes this into the worker image, run with the
production capability flags and a small --pids-limit. It forks idle children
until the kernel refuses one, then kills and REAPS every child it made and
forks once more.

It is Python rather than a shell loop because a shell that fails to fork exits
on the spot and orphans its children to the container's PID 1, which is a
shell that does not reap them: the zombies keep holding the pids the recovery
check needs. Here the process that forks is the one that reaps.

Usage: python3 - LIMIT < pids-limit-probe.py
Exit: 0 the bound held and the container recovered, 1 otherwise.
"""

import errno
import os
import signal
import sys


def main() -> int:
    limit = int(sys.argv[1])
    attempts = limit * 2
    children = []
    refusal = None

    for _ in range(attempts):
        try:
            pid = os.fork()
        except OSError as err:
            refusal = err
            break
        if pid == 0:
            signal.pause()
            os._exit(0)
        children.append(pid)

    for pid in children:
        os.kill(pid, signal.SIGKILL)
    for pid in children:
        os.waitpid(pid, 0)

    if refusal is None:
        print(f"{attempts} processes started under --pids-limit {limit}: the bound does not hold")
        return 1
    if refusal.errno != errno.EAGAIN:
        print(f"the fork failed, but not on the pids limit: {refusal}")
        return 1
    print(f"fork {len(children) + 1} refused under --pids-limit {limit}: {refusal.strerror}")

    # Recovery: with every child reaped, the container runs a command again.
    pid = os.fork()
    if pid == 0:
        os._exit(0)
    _, status = os.waitpid(pid, 0)
    if os.waitstatus_to_exitcode(status) != 0:
        print("the container did not recover once its processes were gone")
        return 1
    print("the container recovered")
    return 0


if __name__ == "__main__":
    sys.exit(main())
