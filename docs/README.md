# docs/ — index & project status

Read this first to find the right document; open only what you need (keeps
context lean). `CLAUDE.md` at the repo root has the working rules and naming map.

## Project status

| Component | State |
|---|---|
| **`v720ctl` AP-mode client** (camera's own hotspot, `192.168.169.1:6123`) | **Built & working** — connect, live view, device control (`ap set`), SD-card, and Wi-Fi provisioning (`ap set-wifi`, code 204) verified on the real camera. |
| **`v720ctl` relay-mode client** (Mode A, self-hostable — no naxclow account) | **Built** end-to-end (domain → CLI `relay connect`); phase-2 live A/V wired via the media path. Not hardware-verified. |
| **`v720srv` self-hosted server** (camera dials out to it via DNS redirect) | **Built to spec** — DNS sinkhole + boot REST + TCP coordinator + UDP media + MJPEG viewer. **Pending real-camera validation** — the current open thread. |
| STA-direct (plain TCP to camera on home Wi-Fi) | **Disproven** — in STA mode the camera is outbound-only; all inbound ports filtered. Hence the server exists. |

**Next action:** validate `v720srv` against the real camera — see
`self-hosted-server-deploy.md` (or the `deploy-v720srv` skill). The server code is
built from a9-v720's *earlier* firmware, so boot-REST shapes / coordinator FSM /
media ordering are provisional until this test passes.

## The documents

| Document | What it is / when to open it |
|---|---|
| **`v720-camera-findings-and-client-spec.md`** | **The source of truth.** Wire frame (§12.2a/12.3a), both handshakes (§12.2/12.3), command-code tables (§12.4–12.7), REST surface (§4), build spec (§8). Confirmed-vs-inferred flagged in §7 & §12.8. Read before any protocol work. |
| **`mode-a-client-design.md`** | Design + key decisions for the relay-mode (Mode A) client: the self-hostable `CloudAPI` abstraction (no naxclow account), the `RelayTransport` port, phase-1/phase-2 split. |
| **`self-hosted-server-scope.md`** | Architecture of `v720srv`: the camera boot flow (DNS → REST → coordinator FSM → media), what reuses client code vs. what's new (Accept-side listeners, `DeviceGateway`, `cloudhttp`). |
| **`self-hosted-server-deploy.md`** | The runbook: build, run (`sudo ./v720srv --host <ip> --dns --verbose`), DNS-redirect, reboot the camera, what to watch for, and what to note if it doesn't register. |
| **`protocol-corrections-log.md`** | Log of where the doc was wrong and got corrected (e.g. resolved obscured codes). Check before re-deriving something that looks off. |
| **`mjpeg-video-format-primer.md`** | Background: how MJPEG framing/reassembly works (video reassembles by JPEG markers, not one image per wire frame). |
| **`tcp-vs-udp-framing-primer.md`** | Background: the length-field framing model shared by all channels. |
| **`nat-traversal-primer.md`** | Background: NAT-traversal candidate model behind relay mode's `candidateProber`. |

## Where the live state lives

Durable cross-session state (decisions, what's verified vs. pending) is in the
Claude Code memory index for this project, under
`~/.claude/projects/<project-slug>/memory/MEMORY.md` (the slug is derived from
this repo's local path, so it is specific to your machine).
