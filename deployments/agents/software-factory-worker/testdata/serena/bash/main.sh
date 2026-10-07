#!/usr/bin/env bash
set -eu

# shellcheck source=greeter.sh
. "$(dirname "$0")/greeter.sh"

greet fixture
