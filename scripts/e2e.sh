#!/bin/sh
# Start the daemon, run the Neovim side against it, and clean up either way.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
sock=$(mktemp -u "${TMPDIR:-/tmp}/quietdm-e2e-XXXXXX.sock")
log=$(mktemp "${TMPDIR:-/tmp}/quietdm-e2e-XXXXXX.log")

cleanup() {
	[ -n "${daemon_pid:-}" ] && kill "$daemon_pid" 2>/dev/null || true
	rm -f "$sock" "$log"
}
trap cleanup EXIT

"$root/quietdmd" -config "$root/tests/e2e.toml" -socket "$sock" >"$log" 2>&1 &
daemon_pid=$!

# Wait for the socket rather than sleeping a guessed amount of time.
i=0
while [ ! -S "$sock" ]; do
	i=$((i + 1))
	if [ "$i" -gt 100 ]; then
		echo "daemon never created $sock" >&2
		cat "$log" >&2
		exit 1
	fi
	sleep 0.1
done

QUIETDM_SOCK="$sock" "${NVIM:-nvim}" --headless \
	-u "$root/tests/minimal_init.lua" \
	-c "luafile $root/tests/e2e.lua" || {
	echo "--- daemon log ---" >&2
	cat "$log" >&2
	exit 1
}
