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

Port 80 needs privilege:

```
sudo ./v720srv --host <that-box's-LAN-IP>
```

Startup logs: `REST on :80, coordinator on :6123, advertising host <ip>:6123`.
Flags: `--http-port` (default 80), `--tcp-port` (6123), `--device-code`,
`--target`, `--pwd` (default `deadbeef`).

## 3. DNS redirect (the real work)

Make the camera resolve naxclow to the server box. With **dnsmasq** (or Pi-hole →
Local DNS):

```
# /etc/dnsmasq.d/a9-camera
address=/naxclow.com/<box-ip>
address=/naxclow.com.cn/<box-ip>
```

Then set that box as the network's DNS: **Fritz!Box → Home Network → Network →
Network Settings → IPv4 → "Local DNS server"** = the box's IP (or hand it out via
DHCP). (The Fritz!Box can't do per-host overrides itself, hence dnsmasq/Pi-hole.)

If you don't have dnsmasq/Pi-hole: `v720srv` could grow a small built-in DNS
responder (sinkhole naxclow → itself, forward the rest) so you'd only point the
Fritz!Box's local DNS at the box — ask if you want that added.

## 4. Reboot the camera

So it re-resolves naxclow and boots against the server.

## What to watch for (control-plane validation)

On `v720srv`'s log:
- Boot REST getting hit (the camera calling `getA9ConfCheck` etc.).
- **The money line:** `camera registered: uid="..."; sent start-stream`

That line = the whole approach validated on this firmware. (No video yet — the
UDP media path is a separate build.)

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
