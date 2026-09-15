---
name: deploy-v720srv
description: Deploy and validate the self-hosted v720srv server against the real V720/A9-Mini camera. Use when the user wants to run the server, point the camera at it, and check whether the camera registers and streams. Covers build, run flags, DNS redirect, and the what-to-watch-for validation checklist.
allowed-tools:
  - Read
  - Bash
---

# Deploy & validate v720srv

The camera is outbound-only in STA mode (all inbound ports filtered), so it must
dial OUT to a server that impersonates naxclow's cloud via DNS redirection. This
skill is the operational runbook. Full detail + failure-triage:
`docs/self-hosted-server-deploy.md`. Architecture: `docs/self-hosted-server-scope.md`.

## Steps

1. **Build** (on an always-on LAN box, or cross-compile from the Mac):
   ```
   go build -o v720srv ./cmd/v720srv
   # or: GOOS=linux GOARCH=amd64 go build -o v720srv ./cmd/v720srv
   ```
   Prebuilt static binaries may already be in `dist/` (amd64 + arm64, gitignored).

2. **Run** (`:80` and `:53` need privilege). Recommended first run — built-in DNS
   sinkhole + verbose logging so nothing else is needed:
   ```
   sudo ./v720srv --host <that-box's-LAN-IPv4> --dns --verbose
   ```
   Key flags: `--host` (required IPv4), `--dns` (built-in :53 sinkhole),
   `--dns-upstream` (default `1.1.1.1:53`), `--http-port` 80, `--https-port` 443
   (self-signed; 0 disables), `--tcp-port` 6123 (TCP coordinator **and** UDP
   media), `--view-port` 8080, `--verbose`, `--device-code`, `--target`, `--pwd`
   (`deadbeef`).

3. **DNS redirect** — make the camera resolve naxclow to the box. Easiest: with
   `--dns` running, set the box as the network's DNS (Fritz!Box → Home Network →
   Network → Network Settings → IPv4 → "Local DNS server" = box IP). Alternative:
   dnsmasq/Pi-hole `address=/naxclow.com/<box-ip>` (+ `.com.cn`).

4. **Reboot the camera** so it re-resolves naxclow and boots against the server.

## What to watch for (validation)

- Boot REST getting hit (camera calling `getA9ConfCheck` etc.).
- **The money line:** `camera registered: uid="..."; sent start-stream` — control
  plane validated on this firmware.
- Then open **`http://<box-ip>:8080/live`** (MJPEG) or `/snapshot`. `--verbose`
  logs `media: N video frames …` when frames arrive.

## If it does NOT work — note the furthest stage reached

- **No REST hits** → DNS redirect isn't reaching the camera (most common).
- **REST hits, no coordinator connection** → firmware may force HTTPS, or
  `getA9ConfCheck`'s shape differs — capture the exact paths requested.
- **Coordinator connects, no `registered`** → register handshake differs on this
  firmware — capture any `camera bring-up failed` log.
- **`registered` but no video** → media/probe path or open-video trigger diverges
  (a9-v720 is an earlier firmware) — this is the next thing to adapt.

Record whatever real-camera behavior you learn back into the findings doc and the
memory index so the next session isn't guessing.
