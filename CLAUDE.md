# CLAUDE.md

Instructions for Claude Code working in this repository. Read it before doing anything else, even with no memory of how this project came to be.

## What this project is

An independent Go client (`v720ctl`) **and** self-hosted server (`v720srv`) for the **"V720" / "A9 Mini" WiFi camera** (vendor: **Naxclow**; cheap AliExpress-class hardware, Beken BK7252-class MCU running RT-Thread). The official apps are unreliable, so this reimplements the camera's actual wire protocol directly in Go, from a full reverse-engineering pass over the official Android app. This is an interoperability project for hardware the developer owns; it is **not** meant to attack, disrupt, or gain unauthorized access to any third-party service. If cloud-relay is ever used, it uses the developer's own naxclow credentials exactly as the official app would — never someone else's.

**Current state and where to start reading: `docs/README.md`** — the docs index + project status. Short version: the AP-mode and relay-mode clients are built and working; `v720srv` is built to spec but **pending real-camera validation** (see `docs/self-hosted-server-deploy.md`).

## Before touching any protocol code — read this first

1. **Read the findings/spec doc in full first: `docs/v720-camera-findings-and-client-spec.md`.** It is the single source of truth — the cloud REST surface (§4), the two connection modes and how the app chooses (§5, §12.1/12.1a), the unified 20-byte wire frame used by *every* channel (§12.2a/12.3a), the byte-accurate handshakes for both modes (§12.2/12.3), the JSON command-code tables (device control §12.5, SD-card/file §12.6, connection/auth §12.7), and the client build spec (§8).
2. **Confirmed vs. inferred:** the doc separates confirmed facts from unconfirmed guesses — see §7 ("Open Items") and §12.8 ("Remaining gaps"). Treat confirmed details as ground truth; treat flagged-unconfirmed ones as provisional (say so in code comments/PRs, don't let a guess silently become "the spec").
3. **When the doc doesn't cover something, or a detail needs re-verifying, consult the decompiled Android app.** It is a `jadx` decompile of the official APK, kept **outside this repo** (it is the vendor's copyrighted app — not redistributed here); produce your own with `jadx -d output_jadx <com.naxclow.v720.apk>` and look under `output_jadx/sources`. The relevant packages are `com.naxclow.rtc` (session/engine) and `com.naxclow.rtc.net` (channel/transport/framing — the wire protocol). It's a `jadx` decompile of `com.naxclow.v720` v3.2.8, heavily ProGuard/R8-obfuscated (`C3048OooOo0O`, `OooOOO0`, …). **Never carry obfuscated names into the Go code** — translate via the naming map below and extend it when you identify something new.
4. **If jadx contradicts or extends the doc, update the doc** (or add a marked discrepancy note) rather than silently diverging — the doc must stay authoritative for the next session.
5. Some codes/field-names in the doc are still obscured because jadx substitutes symbolic names from unrelated third-party enums for inlined constants — resolving them means reading the real integer/string value in a decompiler, not guessing from context.

> For *how* to look something up efficiently (which doc section, which jadx package, the a9-v720 reference), use the **`protocol-lookup`** skill.

## Naming conventions — obfuscated Java → Go

The map is now largely complete (the client and server are built). Extend it whenever you identify a new obfuscated symbol worth naming — name things for what they *do* per the findings doc, never transliterate the Java name.

| Obfuscated (Java, decompiled) | What it actually is | Go name |
|---|---|---|
| `NaxclowRtcEngine` | Central singleton managing all device sessions | `SessionManager` (application layer — creates/looks up a `Session` per device) |
| `NaxclowSdkUtil` | Thin one-line-per-method facade in front of the engine (likely a JS/hybrid-UI bridge), no protocol logic of its own | Not needed as a separate type — this is just the public API surface (`SessionManager` + `Session`) |
| `AbstractC3040OooO0oO` | Abstract per-device session (tracks P2P mode, stats, exposes the send-frame primitive) | `Session` (interface — the primary/driving port; see Architecture section) |
| `C3048OooOo0O` | P2P/cloud-relay session implementation | `RelaySession` (application-layer service implementing `Session`) |
| `OooOOO0` | AP-mode (camera's own WiFi hotspot) session implementation | `APSession` (application-layer service implementing `Session`) |
| `NaxclowServerChannel` | TCP control/coordination channel, relay mode only | `CoordinatorChannel` (adapter implementing the `Transport` driven port) |
| `NaxclowLiveChannel` | UDP live audio/video channel, relay mode only | `MediaChannel` (adapter implementing `Transport`) |
| `NaxclowSdcardChannel` | TCP SD-card/playback channel, relay mode only | `StorageChannel` (adapter implementing `Transport`) |
| `NaxclowApChannel` | The single TCP channel carrying everything in AP-mode | `APChannel` (adapter implementing `Transport`) |
| `C3050OooOoO` | The 20-byte-header wire packet/frame object | `Frame` (pure domain value object — `Encode`/`Decode`, no I/O) |
| `HandlerC3046OooOOoo` | Android-Handler-based state machine driving NAT-traversal candidate probe retries | `candidateProber` (unexported — internal detail of `RelaySession`) |
| `Oooo000` (field name `OooOooo` on `RelaySession`) | Frame reassembly / adaptive-quality-feedback tracker for the live channel | `jitterBuffer` |
| `INaxclowLiveListener` | Live-session event callback interface | `LiveEventListener` |
| `INaxclowVideoData` | Video frame delivery callback | `VideoFrameHandler` |
| `INaxclowAudioData` | Audio frame delivery callback | `AudioFrameHandler` |
| `INaxclowPlaybackListener` | SD-card playback event callback | `PlaybackEventListener` |
| `INaxGSensorListener` | Orientation-sensor data callback | `OrientationListener` |
| `P2P_MODE_DIRECT` / `_NAT` / `_FORWARD` / `_AP` | The connection mode ultimately achieved | Go `ConnectionMode` type; `ModeDirect`, `ModeNAT`, `ModeForward`, `ModeAP` |
| `P2P_TYPE_TCP` / `_UDP` | Underlying transport for a given mode | Not usually needed as its own type — implied by which `Session`/channel is in play |
| Packet type tag values (`0,1,4,6,7,8,10,100,101,102,115`) | What kind of payload a `Frame` carries | `FrameType*` constants — see doc §12.4 |
| JSON `"code"` field values, scoped per channel | Application-level message type within a JSON control frame | `Code*` constants, one Go const block per channel/purpose grouping — see doc §12.2/12.5/12.6/12.7 |

## Architecture: DDD + Hexagonal (Ports & Adapters), Go-idiomatic

The layout in the repo already realizes this: `internal/domain` (pure), `internal/application` (use cases + ports), `internal/adapters/*` (adapters), `cmd/v720ctl` + `cmd/v720srv` (composition roots).

- **Domain has zero I/O/framework/adapter dependencies.** Pure, synchronous, trivially unit-testable: `Frame` (20-byte wire value object, §12.2a/12.3a), `ConnectionMode`, `Command` value objects (§12.5), SD-card/file types, `MediaReassembler`. No `net`, no goroutines — just data and the rules for interpreting it. (Sanctioned exception: pure, I/O-free stdlib helpers like `net.JoinHostPort` for building a dial string are fine — the rule bars sockets/frameworks, not pure stdlib functions.)
- **Application holds the use cases** (`ConnectToCamera`, `StartLiveView`, `SendCommand`, `ListSDCardFiles`, the server-side `DeviceGateway`, …) and the `Session`/`SessionManager` orchestration. It depends on the domain and on **ports** it defines for itself — never on concrete adapters.
- **Ports are plain Go interfaces defined where consumed** (in the application package that needs them), not a generic `ports` package — the idiomatic-Go way: structural typing means an adapter satisfies a port just by having the methods. Keep each port small (1–4 methods), named for what the application needs.
  - Primary/driving: `Session` (what a caller can do with a connected camera). Implemented by `RelaySession`, `APSession`.
  - Driven/secondary: `Transport` (send/receive `Frame`s, mode-agnostic), `RelayTransport` (relay's TCP-coordinator + UDP-media split), `CloudAPI` (naxclow REST, Mode A only).
- **Adapters implement ports at the edges.** Driven: the AP TCP channel, the relay channels (coordinator/media/storage), the naxclow REST client, `cloudhttp` (server boot REST). Driving: the CLI.
- **Dependency direction is strictly inward:** adapters → application → domain, never the reverse. Domain importing an adapter, or application importing a concrete adapter instead of its own port, is a violation to fix.
- **Stay idiomatic:** accept interfaces / return concrete structs from constructors; keep interfaces minimal and consumer-defined; no DI framework (plain constructor injection); table-driven tests; don't create a package per "layer word" if it causes stutter or empty packages.

## Workflow

Delegate to the global Go subagents per layer — **`AGENTS.md` maps each `go-*` agent to a layer and gives the per-unit-of-work loop.** In short:

- **Smallest independently-mergeable slice, one branch per step.** One type, one port, one adapter, one use case, or one handshake step — no drive-by refactors, no bundling unrelated fixes. Branch → implement → `go-reviewer` → merge → branch again.
- **Test-first wherever the doc pins the exact behavior** (most of the domain: byte tables, JSON shapes, handshake sequences). Write the failing test, then implement. Where behavior depends on the real camera and isn't in the doc (flagged unconfirmed in §7/§12.8), a short spike is fine — but lock it in with a regression test in the same branch and update the findings doc with what you learned.
- **Test coverage is not optional in `domain`/`application`** — they're pure by design so they can be tested exhaustively without mocks. Adapters have edges only verifiable against real hardware; cover what you can with fakes and say in the PR what's only been checked against the real device.
- **Wire-format code is the highest-risk area** (byte order, offsets, off-by-one framing) — prioritize byte-exact golden tests from the doc's tables, and run `go-reviewer` on every piece of wire-protocol code before merging.
- **Keep the naming map above and `AGENTS.md` in sync** as understanding deepens (e.g. a `go-refactor` rename pass updates the map in the same change).

## Build & test

```
go build ./...        # both binaries: cmd/v720ctl, cmd/v720srv
go test ./...          # near-complete coverage expected in domain/application
go test -race ./...    # for the concurrent server paths (media hub, listeners)
go vet ./...
```

All three are expected green before merging. To deploy and validate `v720srv` against the real camera (the current open thread), use the **`deploy-v720srv`** skill / `docs/self-hosted-server-deploy.md`.
