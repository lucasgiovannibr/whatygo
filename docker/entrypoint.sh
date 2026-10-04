#!/bin/sh
# Hand the data volumes to the unprivileged user, then run the server as that user.
#
# The server does not need root: it listens on a high port and writes only to /app/dbdata and
# /app/logs. A volume created by an image that ran as root still belongs to root, though, and
# the unprivileged user could not write to it, so ownership is fixed here, at every start.
set -e

if [ "$(id -u)" = "0" ]; then
    for dir in /app/dbdata /app/logs "${LOG_DIRECTORY:-}"; do
        [ -n "$dir" ] && [ -d "$dir" ] && chown -R whatygo:whatygo "$dir" 2>/dev/null || true
    done
    exec su-exec whatygo:whatygo "$@"
fi

# Already unprivileged (the container was started with --user): nothing to fix.
exec "$@"
