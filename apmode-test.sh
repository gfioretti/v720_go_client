#!/bin/sh
# apmode-test.sh — run this while connected to the camera's Nax_* Wi-Fi.
# It needs NO internet. Output is shown LIVE in your terminal and also saved to
# apmode-test.log; ~10s of video frames go to ./capture/, so you can reconnect to
# your home Wi-Fi afterward and we'll review the results together.
#
# Usage (from the project directory):
#   ./apmode-test.sh                     # uses the default device id, no password
#   ./apmode-test.sh <devId>             # your camera's id if different
#   ./apmode-test.sh <devId> <password>  # if the camera has a password
#
# Then switch back to your home Wi-Fi and tell me "done".

cd "$(dirname "$0")" || exit 1

DEV="${1:-080c0e07D735}"
PW="${2:-}"
ADDR="192.168.169.1:6123"
LOG="apmode-test.log"

PWARG=""
[ -n "$PW" ] && PWARG="--password $PW"

# Everything (status + --debug frame log) is streamed live to the terminal AND
# copied to the log via tee. The whole body is grouped and piped so both the
# script's own echoes and every command's stdout/stderr are mirrored in real time.
{
	echo "=== apmode-test $(date) ==="
	echo "device=$DEV addr=$ADDR password=$([ -n "$PW" ] && echo set || echo none)"

	echo
	echo "--- [1] this Mac's Wi-Fi address (expect 192.168.169.x) ---"
	ifconfig en0 | grep 'inet '
	echo "--- route to the camera ---"
	route -n get 192.168.169.1 2>&1 | grep -E 'gateway|interface'

	echo
	echo "--- [2] is 192.168.169.1:6123 reachable? ---"
	if nc -z -G 3 192.168.169.1 6123; then
		echo "6123 OPEN"
	else
		echo "6123 NOT reachable — are you on the Nax_* Wi-Fi and is the camera in AP mode?"
	fi

	echo
	echo "--- [3] rebuild client if missing ---"
	[ -x ./v720ctl ] || go build -o v720ctl ./cmd/v720ctl
	echo "binary: $([ -x ./v720ctl ] && echo present || echo MISSING)"

	echo
	echo "--- [4] connect / handshake (smallest test) ---"
	# shellcheck disable=SC2086
	./v720ctl ap connect --addr "$ADDR" --dev "$DEV" $PWARG --debug --timeout 8s
	echo "connect exit=$?"

	echo
	echo "--- [5] live capture: 10s, frames -> ./capture/ ---"
	rm -rf ./capture && mkdir -p ./capture
	# shellcheck disable=SC2086
	./v720ctl ap live --addr "$ADDR" --dev "$DEV" $PWARG --debug --duration 10s --out ./capture
	echo "live exit=$?"

	echo
	echo "--- [6] captured frames ---"
	ls -l ./capture 2>/dev/null | head -5
	echo "frame count: $(ls ./capture 2>/dev/null | wc -l | tr -d ' ')"

	echo
	echo "=== done — reconnect to your home Wi-Fi and tell me 'done' ==="
} 2>&1 | tee "$LOG"
