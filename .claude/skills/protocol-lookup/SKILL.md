---
name: protocol-lookup
description: Resolve a V720/Naxclow wire-protocol detail (a frame field, a command code, a handshake step, a JSON shape) without guessing. Use when implementing or reviewing protocol code and you need to find the authoritative answer. Points to the right findings-doc section, the jadx source, and the a9-v720 reference.
allowed-tools:
  - Read
  - Grep
  - Glob
  - Bash
---

# Protocol lookup — find the answer, don't guess

Order of authority: **findings doc → jadx source → a9-v720**. If jadx/a9-v720
contradicts or extends the doc, update the doc (per `CLAUDE.md`).

## 1. Findings doc — the source of truth

`docs/v720-camera-findings-and-client-spec.md`. Section map:

- **Frame (20-byte header, all channels):** §12.2a / §12.3a. Offsets: len(int32
  LE)@0, type-tag(int16)@4, flag(int8)@6, unknown@7, opaque(8B)@8, seq(uint32)@16.
- **Frame type tags:** §12.4 (`FrameType*` in `internal/domain/frame.go`).
- **Command codes (JSON `"code"`), scoped per channel:**
  - device control (`201–230`, `299`): §12.5
  - SD-card / file transfer (`400–418`, `110`): §12.6
  - connection / auth (`500,501,502,4`): §12.7
  - coordinator / media channel codes: §12.2
- **Handshakes:** AP-mode byte-accurate §12.3; relay-mode (TCP coordinator + UDP
  media) §12.2.
- **Cloud REST surface:** §4. **Build spec (Mode A/B):** §8.
- **Still unconfirmed / open:** §7 and §12.8 — treat as provisional.
- **What was previously wrong:** `docs/protocol-corrections-log.md`.

Quick grep for a section: `grep -n "^#\{1,4\} .*12\.5" docs/v720-camera-findings-and-client-spec.md`

## 2. jadx-decompiled Android app (v3.2.8) — when the doc is silent/unsure

Path: `/Users/gabrielfioretti/codebases/personal-projects/V720_extracted/output_jadx/sources`
Packages: `com.naxclow.rtc` (session/engine) and **`com.naxclow.rtc.net`** (the
actual channel/transport/framing — the wire protocol lives here). Heavily
obfuscated — cross-reference names via the naming map in `CLAUDE.md`. Some
constants are inlined and jadx shows unrelated enum names for them; read the real
integer/string in a decompiler, don't infer from context.

## 3. a9-v720 (github.com/intx82/a9-v720) — cross-check / server side

An independent client + fake server for an **earlier firmware** — good for
confirming codes and for the server-side reference, but may diverge from this
camera's firmware. Most useful files (fetch via WebFetch):
- `src/cmd_udp.py` — resolved most previously-obscured command codes.
- `src/fake_srv.py`, `v720_sta.py`, `v720_http.py` — the server-side boot flow
  `v720srv` was built from.
- `PROTOCOL_MSG_FLAG_*` — the signed-byte media flags (HEAD/BODY/END/FINISH).

After resolving anything, update the findings doc (and the naming map if you named
a new symbol) so it's settled for next time.
