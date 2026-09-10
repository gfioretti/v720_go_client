# Self-hosted server (STA / cloud replacement) — scope

## Why this is now necessary (not a guess)

Confirmed against the real camera (2026-09-10): once joined to home Wi-Fi (STA
mode), the V720 **accepts no inbound connections** — every TCP port is
filtered/no-response (nmap top-100 + 6123 closed). It is an **outbound-only cloud
client**: on boot it dials *out* to naxclow, and only starts a session when the
cloud tells it to. So the STA-direct shortcut is dead, and reaching the camera
over the network requires **being the cloud it dials out to** — a self-hosted
server the camera connects to via DNS redirection. This is the doc §6 "fake
server" approach, and a9-v720 has a working reference (`src/fake_srv.py`,
`v720_http.py`, `v720_sta.py`, `netsrv_tcp.py`/`netsrv_udp.py`) we can mirror.

## How the camera behaves on boot (from a9-v720's fake server)

All camera-initiated, outbound. The server must answer three things on the
server's LAN IP:

1. **DNS** — the camera resolves naxclow hostnames; these must be redirected to
   the server IP. A single wildcard covers it (dnsmasq): `address=/naxclow.com/<server-ip>`
   (hosts seen: `v720.naxclow.com`, `v720.p2p.naxclow.com`, `p2p.v720.naxclow.com`,
   `dl2.naxclow.com`, `*.naxclow.com.cn`, `home.naxclow.com`).
2. **HTTP `:80`** — the camera calls REST endpoints; the server returns canned
   `{"code":200,...}` envelopes. The pivotal one is
   **`GET /app/api/ApiServer/getA9ConfCheck`** → `{"data":{"host":<server-ip>,
   "tcpPort":6123,"uid":<devcode>,"domain":"v720.naxclow.com","pwd":"deadbeef",
   "isBind":"8","currTime":<ts>}}` — this tells the camera **where to open its
   session** (our `host:tcpPort`). Others (`registerDevices`, `confirm`,
   `a9bindingAppDevice`, `getDevInfo`) just need `code:200`.
3. **TCP `:6123`** — the camera connects and registers `{uid, token}`; the server
   drives a short FSM and then **injects the start-stream command**:
   `INITIAL` (register → reply `{code:101,status:200}`) → `SET_NAT` → `GET_STATUS`
   → `RETRANS` → `BASEINFO` (code 4) → `CAPLIVE`: send
   `{code:301,"target":…,"content":{code:3}}` (301 forward wrapper, inner code 3 =
   OPEN_A_OPEN_V) to make the camera stream.
4. **UDP `:6123`** — media + keepalive: MJPEG (tag 1), G.711 (tag 4), PCM (tag 6),
   heartbeat/ping/pong (100/101/102), the same 20-byte frames.
5. **Viewer** — a9-v720 also serves `/dev/{id}/live` (multipart MJPEG),
   `/snapshot`, `/audio` for the browser.

## Reuse map — most of this already exists in our codebase

| Need | Reuse |
|---|---|
| 20-byte frame codec | `internal/domain/frame.go` (`Frame` Encode/Decode) — as-is |
| Coordinator codes (100/101/13/30/31/52/301…), stream codes 0–3 | `internal/domain/command.go` — as-is (add the few not yet named) |
| 301 forward wrapper | `domain.NewRelayDeviceCommand` — as-is |
| MJPEG reassembly, G.711 decode | `internal/domain/media.go` (`MJPEGReassembler`, `DecodeALaw`) — as-is |
| REST envelope + shapes the server must ANSWER | `docs §5a` + `internal/adapters/naxclowapi` (invert client→server) |
| Browser viewer (live MJPEG + panel) | `ap serve` HTTP panel in `internal/adapters/cli` — reuse/adapt |
| Handshake sequencing patterns | mirror of `application/relaysession.go` (phase-1), server-side |

## New components to build

Hexagonal, mirroring the client:
- **application: `DeviceGateway`** (server-side session) — accepts a registered
  camera, runs the boot FSM (register→NAT→status→baseinfo→CAPLIVE), exposes the
  live A/V stream to a viewer. The server-side counterpart of `RelaySession`.
- **adapters/transport: a server (Accept) side** — `framedtcp` is currently
  Dial-only; add a TCP **listener** that frames accepted connections (reuse
  `readFrame`), plus a UDP `:6123` listener (partly reuse `relay.MediaChannel`
  read path).
- **adapters: an HTTP "cloud responder"** — serves the camera's boot REST calls,
  critically `getA9ConfCheck` returning our `host:tcpPort`. Small net/http server.
- **adapters: viewer** — reuse the `ap serve` panel to show the ingested stream.
- **cmd/v720srv** — new binary/composition root wiring HTTP:80 + TCP:6123 +
  UDP:6123 + viewer.
- **Docs**: a DNS-setup guide (dnsmasq wildcard) — mostly user config, not code.

## Phased build (branch per step, spike-driven)

1. `framedtcp` **Listen/Accept** side + a UDP listener (adapter, testable vs loopback).
2. application `DeviceGateway` boot FSM against fakes (register→…→CAPLIVE inject),
   TDD from the codes we already have; read a9-v720 `v720_sta.py` to pin the exact
   NAT/status/baseinfo message bodies.
3. HTTP cloud-responder adapter (`getA9ConfCheck` etc.), tested with httptest.
4. `cmd/v720srv` wiring + viewer (reuse `ap serve`). **Real-camera spike:** point
   DNS at it, watch the camera register and stream.
5. Harden (keepalive, reconnect, multi-camera) as needed.

## Open unknowns to pin during build (from a9-v720 `v720_sta.py`, then real camera)
- Exact bodies of the `SET_NAT` / `GET_STATUS` / `RETRANS` / `BASEINFO` messages
  (a9-v720 has them; a9-v720 is an earlier revision so verify against our camera).
- Media path in STA: whether A/V arrives over UDP:6123, or over the TCP session —
  a9-v720 binds both; confirm which our firmware uses.
- Whether the full FSM is required or a minimal subset suffices to start streaming.
- **DNS on the user's network:** the Fritz!Box can't do per-host DNS overrides, so
  the user runs dnsmasq/Pi-hole on a LAN box (they have a Linux box) set as the
  network's DNS, with `address=/naxclow.com/<server-ip>`. Server runs on that box.

## Effort
Moderate. The frame/codec/codes/viewer are reuse; the genuinely new work is the
server-side FSM (bounded — uses codes we have) + three listeners + the HTTP
responder. Bigger than a slice, smaller than Mode A was. a9-v720 de-risks the
protocol; the client we built de-risks the Go/architecture side.
