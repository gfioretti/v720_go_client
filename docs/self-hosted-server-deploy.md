# Deploying & validating v720srv (self-hosted server)

The camera is an outbound-only cloud client in STA mode (confirmed: all inbound
ports filtered). `v720srv` replaces naxclow's cloud — the camera connects out to
it via DNS redirection. This is the runbook to deploy it and validate the control
plane (does the real camera connect + register?). See
`docs/self-hosted-server-scope.md` for the architecture.

## 1. Build

On an always-on LAN box (e.g. the Linux box), with Go installed:

```
go build -o v720srv ./cmd/v720srv
```

Or cross-compile from the Mac (if the box is amd64 Linux):

```
GOOS=linux GOARCH=amd64 go build -o v720srv ./cmd/v720srv
# then copy v720srv to the box
```

## 2. Run

Port 80 (and 53 with `--dns`) needs privilege. Recommended first run — the
built-in DNS sinkhole plus verbose logging:

```
sudo ./v720srv --host <that-box's-LAN-IP> --dns --verbose
```

Startup logs: `REST on :80, coordinator+media on :6123, viewer on :8080/live
(+/snapshot)` and, with `--dns`, `DNS sinkhole on :53 …`.
Flags: `--http-port` (80), `--https-port` (443, self-signed TLS; 0 disables),
`--tcp-port` (6123, TCP coordinator + UDP media), `--view-port` (8080), `--dns`,
`--dns-upstream` (1.1.1.1:53), `--verbose`, `--device-code`, `--target`, `--pwd`
(`deadbeef`).

## 3. DNS redirect (the real work)

Make the camera resolve naxclow to the server box. **Easiest — the built-in
sinkhole:** run with `--dns` (above), then set that box as the network's DNS:
**Fritz!Box → Home Network → Network → Network Settings → IPv4 → "Local DNS
server"** = the box's IP (or hand it out via DHCP). `v720srv` answers naxclow
queries with `--host` and forwards everything else to `--dns-upstream`, so the
rest of the network keeps resolving normally. (Requires an IPv4 `--host`.)

**Alternative (dnsmasq/Pi-hole)**, if you'd rather not run DNS in `v720srv` — omit
`--dns` and instead:

```
# /etc/dnsmasq.d/a9-camera
address=/naxclow.com/<box-ip>
address=/naxclow.com.cn/<box-ip>
```

then point the Fritz!Box's Local DNS server at the Pi-hole/dnsmasq box.

## 4. Reboot the camera

So it re-resolves naxclow and boots against the server.

## What to watch for (control-plane validation)

On `v720srv`'s log (run with `--verbose` for per-request/per-frame detail —
each REST path, coordinator frame code, DNS sinkhole hit, and media stats):
- Boot REST getting hit (the camera calling `getA9ConfCheck` etc.).
- **The money line:** `camera registered: uid="..."; sent start-stream`

That line = the control plane validated on this firmware. Then check the video:
**`http://<box-ip>:8080/live`** (MJPEG stream) in a browser, or
**`/snapshot`** for a single still. The media path is built (UDP ingest →
reassembly → MJPEG); `--verbose` logs `media: N video frames …` when frames
arrive. If it logs `registered` but no video appears, the media/probe path or the
open-video trigger
diverges on this firmware — that's the next thing to adapt.

## If it does NOT register — note the furthest stage reached

- **No REST hits at all** → DNS redirect isn't reaching the camera (most common;
  confirm the camera actually uses the box as DNS).
- **REST hits but no coordinator connection** → the camera may want HTTPS (we
  serve plain HTTP:80, as a9-v720 does — if the firmware forces TLS we'd need a
  TLS listener), or `getA9ConfCheck`'s shape differs — capture the exact paths it
  requested.
- **Coordinator connects but no "registered"** → the register handshake differs
  on this firmware — capture any `camera bring-up failed` log.

## Known caveats
- a9-v720 (the protocol reference) reflects an EARLIER firmware revision, so the
  boot REST shapes and coordinator FSM are provisional (UNCONFIRMED) until this
  test passes.
- Port 80 + a network-wide DNS change affect the whole LAN — the dnsmasq wildcard
  only rewrites `naxclow.com`, everything else forwards upstream.
