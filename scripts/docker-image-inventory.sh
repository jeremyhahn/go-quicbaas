#!/usr/bin/env bash
# docker-image-inventory.sh — print every Docker image identity this repository
# creates under a name it writes literally, one per line, sorted.
#
# This is the authoritative answer to "what should `make clean-docker` remove?",
# derived from the commands that create the images rather than from the
# Makefile's DOCKER_LOCAL_IMAGES list. scripts/verify-clean-docker.sh compares
# the two, so a new image-producing target fails `make verify` until cleanup
# knows about it. Deriving independently is the point: reading the list under
# test cannot detect an omission from it.
#
# Two sources, both literal:
#
#   1. This repo's own Docker commands in the Makefile and scripts/ — the `-t`
#      of every `docker build` / `buildx build`, the destination of every
#      `docker tag`, and the arguments of every `docker rmi`. Backslash
#      continuations are joined first, so a build spread over several lines is
#      one command here, and a variable assigned a literal in that same command
#      is resolved.
#
#   2. The explicit `image:` tag of every Compose service that also has a
#      `build:` — the identity Compose gives the image it builds.
#
# Compose services *without* an explicit `image:` are deliberately not derived.
# Compose names those `<project>-<service>`, and the project name is the
# directory Compose ran in or a target's `-p` override — for the root
# docker-compose.yml it is the checkout directory's own name, which differs
# between a developer's clone and a CI workspace. Reproducing that here would
# mean reimplementing Compose's project-name resolution, and the names it
# produces belong to whichever checkout on the host used them last rather than
# to the repository. Those stacks are torn down by the target that runs them,
# where the name is unambiguous because that target just used it — and where
# the target names the images literally (as `integration-test` does), source 1
# above picks them up.

set -euo pipefail

ROOT=${ROOT:-$(cd "$(dirname "$0")/.." && pwd)}
cd "$ROOT"

# Source 1: this repository's own docker commands.
scan_docker_commands() {
    local file
    for file in Makefile scripts/*.sh; do
        [ -f "$file" ] || continue
        case "$file" in
            scripts/docker-image-inventory.sh|scripts/verify-clean-docker.sh) continue ;;
        esac
        # Join backslash continuations so each logical command is one record:
        # a build's `-t` and the variable assignment it refers to then sit in
        # the same record, which is also the shell's own view of them.
        sed -e ':a' -e '/\\$/{N;s/\\\n//;ta' -e '}' "$file" |
            awk \
                -v docker_image="$DOCKER_IMAGE" \
                -v docker_tag="$DOCKER_TAG" \
                -v docker_registry="$DOCKER_REGISTRY" \
                -v origin="$file" \
                '
                function unquote(s) {
                    gsub(/["'"'"']/, "", s)
                    return s
                }
                # Expand make variables, then a shell variable this command
                # assigned a literal. Returns the token unchanged if neither
                # applies; the caller decides whether that is a gap.
                function resolve(tok,   name) {
                    tok = unquote(tok)
                    gsub(/\$\(DOCKER_IMAGE\)/, docker_image, tok)
                    gsub(/\$\(DOCKER_TAG\)/, docker_tag, tok)
                    gsub(/\$\(DOCKER_REGISTRY\)/, docker_registry, tok)
                    if (tok ~ /^\$+\{?[A-Za-z_][A-Za-z0-9_]*\}?$/) {
                        name = tok
                        gsub(/[$\{\}]/, "", name)
                        if (name in lit) tok = lit[name]
                    }
                    return tok
                }
                function emit(tok, producer,   id) {
                    id = resolve(tok)
                    if (id ~ /\$/) {
                        # A producer we cannot name is a hole in the inventory;
                        # a remover we cannot name is not, because whatever it
                        # removes some producer already named.
                        if (producer) printf "unresolved\t%s\t%s\n", origin, tok > "/dev/stderr"
                        return
                    }
                    if (id !~ /:/) id = id ":latest"
                    print id
                }
                /^[ \t]*#/ { next }
                # A shell script assigns its variables in statements of their
                # own, so collect them file-wide and process once the file is
                # read. Assignments in the command itself still win over that.
                {
                    lines[++count] = $0
                    n = split($0, tok, /[ \t]+/)
                    for (i = 1; i <= n; i++) {
                        t = tok[i]
                        sub(/;$/, "", t)
                        if (t ~ /^[A-Za-z_][A-Za-z0-9_]*=/) {
                            eq = index(t, "=")
                            v = unquote(substr(t, eq + 1))
                            if (v ~ /^[A-Za-z0-9._\/:-]+$/) glob[substr(t, 1, eq - 1)] = v
                        }
                    }
                }
                END {
                  for (r = 1; r <= count; r++) {
                    delete lit
                    for (k in glob) lit[k] = glob[k]
                    n = split(lines[r], tok, /[ \t]+/)
                    for (i = 1; i <= n; i++) {
                        t = tok[i]
                        sub(/;$/, "", t)
                        if (t ~ /^[A-Za-z_][A-Za-z0-9_]*=/) {
                            eq = index(t, "=")
                            v = unquote(substr(t, eq + 1))
                            if (v ~ /^[A-Za-z0-9._\/:-]+$/) lit[substr(t, 1, eq - 1)] = v
                        }
                    }
                    mode = ""; positional = 0
                    for (i = 1; i <= n; i++) {
                        t = tok[i]
                        ends_command = (t ~ /;$/)
                        sub(/;$/, "", t)
                        if (t == "" ) { if (ends_command) mode = ""; continue }
                        if (t ~ /^(\||\|\||&&|&)$/) { mode = ""; continue }
                        if (t ~ /(^|[^A-Za-z0-9_-])docker$/ || t ~ /\$\(DOCKER\)$/) {
                            mode = "docker"; positional = 0; continue
                        }
                        if (mode == "docker") {
                            if (t == "buildx") continue
                            if (t == "build") { mode = "build"; continue }
                            if (t == "tag")   { mode = "tag";   continue }
                            if (t == "rmi")   { mode = "rmi";   continue }
                            mode = ""; continue
                        }
                        if (mode == "build") {
                            if (t == "-t" && i < n) emit(tok[i + 1], 1)
                        } else if (mode == "tag") {
                            if (t ~ /^-/) continue
                            if (++positional == 2) { emit(t, 1); mode = "" }
                        } else if (mode == "rmi") {
                            if (t ~ /^-/) continue
                            if (unquote(t) !~ /^[A-Za-z0-9$._\/:{}-]+$/) { mode = ""; continue }
                            emit(t, 0)
                        }
                        if (ends_command) mode = ""
                    }
                  }
                }
                '
    done
}

# Source 2: the explicit `image:` of every Compose service that builds.
scan_compose_image_tags() {
    local file
    while IFS= read -r file; do
        awk '
            function flush() {
                if (builds && image != "") print image
                builds = 0; image = ""
            }
            /^services:[ \t]*$/ { in_services = 1; next }
            /^[A-Za-z]/        { flush(); in_services = 0 }
            !in_services       { next }
            /^  [A-Za-z0-9_.-]+:[ \t]*$/ { flush(); next }
            /^    build:/      { builds = 1 }
            /^    image:[ \t]+/ {
                image = $2
                gsub(/["'"'"']/, "", image)
            }
            END { flush() }
        ' "$file"
    done < <(find . -path ./.git -prune -o -name 'docker-compose*.y*ml' -print | sort) |
        awk '{ if ($0 !~ /:/) $0 = $0 ":latest"; print }'
}

DOCKER_IMAGE=$(make --no-print-directory print-DOCKER_IMAGE)
DOCKER_TAG=$(make --no-print-directory print-DOCKER_TAG)
DOCKER_REGISTRY=$(make --no-print-directory print-DOCKER_REGISTRY)

{ scan_docker_commands; scan_compose_image_tags; } | sort -u
