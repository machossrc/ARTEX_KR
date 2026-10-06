#!/bin/sh
# Direct execution preserves arguments, exit status and SIGTERM delivery.
set -u
cd "$(dirname "$0")" || exit 1
exec ./artex "$@"
