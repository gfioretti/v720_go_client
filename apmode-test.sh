#!/bin/sh
# apmode-test.sh — run this while connected to the camera's Nax_* Wi-Fi.
# It needs NO internet. Output is shown LIVE in your terminal and also saved to
# apmode-test.log. ~10s of video frames go to ./capture/, the decoded audio to
# ./capture/audio.wav, and a muxed ./capture/clip.mp4 (video + audio); step [7]
# briefly pops open a live ffplay window (if ffplay is installed). Reconnect to
# your home Wi-Fi afterward and we'll review together.
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
	echo "--- [3] rebuild client (always, so code changes take effect) ---"
	if go build -o v720ctl ./cmd/v720ctl; then
		echo "binary: rebuilt ($(date -r ./v720ctl '+%H:%M:%S'))"
	else
		echo "binary: BUILD FAILED — using existing $([ -x ./v720ctl ] && echo binary || echo '(none)')"
	fi

	echo
	echo "--- [4] connect / handshake (smallest test) ---"
	# shellcheck disable=SC2086
	./v720ctl ap connect --addr "$ADDR" --dev "$DEV" $PWARG --debug --timeout 8s
	echo "connect exit=$?"

	echo
	echo "--- [5] live capture: 10s, frames -> ./capture/, audio -> ./capture/audio.wav ---"
	rm -rf ./capture && mkdir -p ./capture
	# shellcheck disable=SC2086
	./v720ctl ap live --addr "$ADDR" --dev "$DEV" $PWARG --debug --duration 10s \
		--out ./capture --audio-out ./capture/audio.wav
	echo "live exit=$?"

	echo
	echo "--- [6] captured frames + audio ---"
	echo "frame count: $(ls ./capture/frame-*.jpg 2>/dev/null | wc -l | tr -d ' ')"
	if [ -f ./capture/audio.wav ]; then
		echo "audio.wav size: $(wc -c < ./capture/audio.wav | tr -d ' ') bytes"
		# afinfo (macOS) prints sample rate / duration; fall back to file(1).
		if command -v afinfo >/dev/null 2>&1; then
			afinfo ./capture/audio.wav 2>&1 | grep -Ei 'data format|duration|sample rate' | head -4
		else
			file ./capture/audio.wav
		fi
	else
		echo "audio.wav: NOT created (no G.711 audio frames received?)"
	fi

	echo
	echo "--- [7] live preview: 8s ffplay window ---"
	if command -v ffplay >/dev/null 2>&1; then
		echo "opening an ffplay window for 8s (the default player is -autoexit, so it auto-closes)..."
		# shellcheck disable=SC2086
		./v720ctl ap preview --addr "$ADDR" --dev "$DEV" $PWARG --duration 8s
		echo "preview exit=$?"
	else
		echo "ffplay not installed — skipping. Install ffmpeg to try 'v720ctl ap preview'."
	fi

	echo
	echo "--- [8] record 10s to a single MP4 (video + synced audio) ---"
	if command -v ffmpeg >/dev/null 2>&1; then
		# shellcheck disable=SC2086
		./v720ctl ap record --addr "$ADDR" --dev "$DEV" $PWARG --duration 10s --out ./capture/clip.mp4
		echo "record exit=$?"
		if [ -f ./capture/clip.mp4 ]; then
			echo "clip.mp4 size: $(wc -c < ./capture/clip.mp4 | tr -d ' ') bytes"
			if command -v ffprobe >/dev/null 2>&1; then
				ffprobe -hide_banner -loglevel error \
					-show_entries format=duration,format_name \
					-show_entries stream=codec_type,codec_name,width,height,sample_rate \
					-of default=noprint_wrappers=1 ./capture/clip.mp4
			fi
		else
			echo "clip.mp4: NOT created"
		fi
	else
		echo "ffmpeg not installed — skipping. Install ffmpeg (brew install ffmpeg) to record MP4."
	fi

	echo
	echo "=== done — reconnect to your home Wi-Fi and tell me 'done' ==="
} 2>&1 | tee "$LOG"
