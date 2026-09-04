# CLAUDE.md

Instructions for Claude Code working in this repository. This file is self-contained — read it before doing anything else, even if you have no memory of how this project came to be.

## What this project is

An independent Go client for the **"V720" / "A9 Mini" WiFi camera** (backend vendor: **Naxclow**, cheap AliExpress-class hardware, MCU family BL7252/BK7252). The official mobile apps (iOS and Android) are unreliable — the iOS app in particular is broken. This project reimplements the camera's actual wire protocol directly in Go, based on a full reverse-engineering pass over the official Android app, so the camera can be used without depending on either app. This is an interoperability project for hardware the developer owns; it is not intended to attack, disrupt, or gain unauthorized access to any third-party service. If cloud-relay mode is ever implemented, it uses the developer's own naxclow account credentials exactly as the official app would — never someone else's.

## Before touching any protocol code — read this first

1. **Read the findings/spec document in full before writing or reviewing any protocol code.** It should be at the project root (or `docs/`) as `v720-camera-findings-and-client-spec.md` — if it's been renamed or moved, find it before proceeding; do not proceed without it. It is the single source of truth for:
   - The cloud REST API surface (section 4)
   - The two connection modes — AP-mode/local-direct and P2P/cloud-relay — and how the app chooses between them (sections 5, 12.1, 12.1a)
   - The unified 20-byte wire frame format used by *every* channel (section 12.2a/12.3a)
   - The full handshake sequences for both modes, byte-accurate for AP-mode (section 12.3) and fully mapped for relay-mode across its TCP control channel and UDP media channel (section 12.2)
   - JSON command-code tables for device control (12.5), SD-card/file transfer (12.6), and connection/auth (12.7)
   - The client build specification itself (section 8) — Mode A (cloud-relay) and Mode B (AP-mode/local), with a recommendation to build Mode B first since it's simpler and has no cloud dependency
2. **The doc distinguishes confirmed facts from inferred/unconfirmed ones** — see its section 7 ("Open Items") and section 12.8 ("Remaining gaps"). Treat confirmed details as ground truth. Treat anything flagged unconfirmed as provisional: implement it if you must, but say so in a code comment and in any PR description, and don't let an unverified guess silently become "the spec" for later work.
3. **If the doc doesn't cover something you need, or a detail needs re-verification, consult the decompiled Android app source directly**, at:
   ```
   /Users/gabrielfioretti/codebases/personal-projects/V720_extracted/output_jadx/sources
   ```
   The relevant packages are `com.naxclow.rtc` (session/engine logic) and `com.naxclow.rtc.net` (the actual channel/transport/framing code — this is where the wire protocol lives). This source is a `jadx` decompilation of the official Android app (package `com.naxclow.v720`, version 3.2.8) and is heavily ProGuard/R8-obfuscated — meaningless class and method names like `C3048OooOo0O`, `OooOOO0`, `AbstractC3040OooO0oO`. **Never carry these obfuscated names into this Go codebase** — translate everything using the naming map below, and extend that map when you identify something new.
4. **If something in the jadx source contradicts or extends the doc, update the doc** (or add a clearly marked discrepancy note) rather than silently diverging from it — the doc needs to stay authoritative for whoever picks this up next, including a future you with no memory of this session.
5. Some command codes and a few field names in the doc are still obscured because jadx substitutes symbolic names from unrelated third-party enums (an ad SDK, an animation library) for constants the compiler inlined — the doc flags every instance of this. Resolving them means opening the actual `.class`/decompiled file in an IDE (Android Studio or similar, if available) and reading the real integer/string value directly, not guessing from context.

## Naming conventions — obfuscated Java → Go

This is a starter map, not exhaustive. **Extend it here whenever you identify a new obfuscated symbol worth naming**, so it stays the canonical reference rather than tribal knowledge scattered across commits.

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
| Packet type tag values (`0,1,4,6,7,8,10,100,101,102,115`) | What kind of payload a `Frame` carries | `FrameType*` constants (`FrameTypeJSON`, `FrameTypeMJPEG`, `FrameTypeAudioG711`, `FrameTypeAudioPCM`, `FrameTypeSDCardXMLChunk`, `FrameTypeGSensor`, `FrameTypeNoop`, `FrameTypePongAP`, `FrameTypePongRelay`, `FrameTypeHandshakeTrigger`) — see doc section 12.4 |
| JSON `"code"` field values, scoped per channel (`100,13,10,30,31,36,37,52,53,101,301` on the coordinator channel; `21,51,54,55,102` on the media channel; `201–230,299` device control; `400–418,110` SD-card; `500,501,502,4` connection/auth) | Application-level message type within a JSON control frame | `Code*` constants, one Go const block per channel/purpose grouping — see doc sections 12.2, 12.5, 12.6, 12.7 for the authoritative tables |

When naming something not yet in this table, name it for what it *does* per the findings doc — never transliterate the obfuscated Java name — and add a row here so the next person (or the next you) doesn't have to re-derive it.

## Architecture: Domain-Driven Design + Hexagonal (Ports & Adapters), Go-idiomatic

This project follows DDD and hexagonal architecture principles, but implemented the Go way rather than transplanted from Java/C# conventions. Concretely:

- **Domain layer has zero dependencies on I/O, frameworks, or adapters.** It's pure, synchronous, and trivially unit-testable: things like `Frame` (the 20-byte-header wire value object, findings-doc section 12.2a/12.3a), `ConnectionMode`, `Command` (device-control value objects: PTZ move, IR LED, video quality, etc. — doc section 12.5), and the SD-card/file domain types. No `net`, no Netty-equivalent, no goroutines here — just data and the rules for interpreting it (e.g. "what does packet type tag `1` mean", "what fields does a `code:201` command require"). (Sanctioned exception: pure, I/O-free helpers from `net` that are really just string/byte utilities — e.g. `net.JoinHostPort` for building a dial string in `domain.LiveConf.Addr` — are fine. The rule bars I/O, sockets, and frameworks, not the standard library's pure functions.)
- **Application layer holds the use cases** (`ConnectToCamera`, `StartLiveView`, `SendCommand`, `ListSDCardFiles`, ...) and the `Session`/`SessionManager` orchestration logic (the doc's `NaxclowRtcEngine`/`AbstractC3040OooO0oO`/`C3048OooOo0O`/`OooOOO0` — see naming table). It depends on the domain layer and on **ports** (interfaces) it defines for itself — never on concrete adapters.
- **Ports are plain Go interfaces, defined where they're consumed** (in the application package that needs them), not collected into a generic `ports` package. This is the idiomatic-Go way to do hexagonal architecture: Go's structural typing means an adapter satisfies a port just by implementing its methods — it doesn't need to import the application package or declare `implements` anything, so there's no benefit to centralizing interface definitions the way you might in Java. Keep each port small (1-4 methods) and named for what the application needs, not what the adapter provides.
  - Primary/driving port: `Session` — what a caller (the CLI, or any future primary adapter) can do with a connected camera: connect, disconnect, send a command, consume a live A/V stream, browse/download SD-card files. Implemented by the application-layer `RelaySession` and `APSession`.
  - Driven/secondary ports: `Transport` — what a `Session` implementation needs from the network layer to send/receive `Frame`s, without caring whether that's AP-mode's single TCP connection or relay-mode's TCP-coordinator-plus-UDP-media split. Also `CloudAPI` — what `RelaySession`'s setup phase needs from naxclow's REST backend (`getA9VideoConf` etc., doc section 4), if/when Mode A is built.
- **Adapters implement ports and live at the edges.** Secondary/driven adapters do real I/O: the AP-mode TCP channel, the three relay-mode channels (TCP coordinator, UDP media, TCP storage), and the naxclow REST client. Primary/driving adapters call into the application layer from the outside world: the CLI is the only one needed initially.
- **Dependency direction is strictly inward**: adapters → application → domain. Never the reverse. If you find domain code importing anything from `adapters/`, or the application layer importing a concrete adapter type instead of depending on a port it defines, that's a violation to fix, not a convenience to keep.
- **Stay idiomatic Go on top of this**: accept interfaces and return concrete structs from constructors; keep interfaces minimal and consumer-defined (per above) rather than pre-emptively large; avoid a DI framework — plain constructor injection (`NewRelaySession(transport Transport, ...) *RelaySession`) is enough; prefer table-driven tests; don't create a package per "layer word" if it results in stutter or empty packages — `internal/domain`, `internal/application`, `internal/adapters/...` below is a reasonable starting split, but collapse layers into fewer packages if the separation isn't earning its keep as the code grows.

## Proposed project layout

Adjust freely as the project actually develops — this is a starting proposal, not a mandate. Layer boundaries matter far more than the exact directory names.

```
.
├── CLAUDE.md
├── AGENTS.md
├── docs/
│   └── v720-camera-findings-and-client-spec.md
├── cmd/
│   └── v720ctl/                        # composition root: wires adapters to the application layer, then hands off to the CLI
│       └── main.go
├── internal/
│   ├── domain/                          # pure domain — no I/O, no adapter imports
│   │   ├── frame.go                      # Frame value object + Encode/Decode
│   │   ├── frametype.go                  # FrameType* constants (doc section 12.4)
│   │   ├── connectionmode.go             # ConnectionMode value object (Direct/NAT/Forward/AP)
│   │   ├── command.go                    # Command value objects + Code* constants (doc sections 12.5-12.7)
│   │   └── sdcard.go                     # SD-card file/date-list domain types
│   ├── application/                      # use cases + ports (interfaces), depends only on domain
│   │   ├── session.go                    # Session port (driving) + SessionManager
│   │   ├── relaysession.go               # RelaySession (implements Session, uses Transport + CloudAPI ports)
│   │   ├── apsession.go                  # APSession (implements Session, uses Transport port)
│   │   ├── transport.go                  # Transport port (driven) — defined here, satisfied implicitly by adapters
│   │   ├── cloudapi.go                   # CloudAPI port (driven) — only needed once Mode A is built
│   │   ├── livestream.go                 # StartLiveView/StopLiveView use cases
│   │   └── control.go                    # SendCommand, ListSDCardFiles, etc. use cases
│   └── adapters/
│       ├── transport/
│       │   ├── aptcp/                    # secondary adapter: AP-mode's single TCP channel (implements Transport)
│       │   │   └── channel.go
│       │   └── relay/                    # secondary adapters: relay-mode's three channels (each implements Transport)
│       │       ├── coordinator.go        # CoordinatorChannel (TCP)
│       │       ├── media.go              # MediaChannel (UDP)
│       │       └── storage.go            # StorageChannel (TCP)
│       ├── naxclowapi/                   # secondary adapter: naxclow cloud REST client (implements CloudAPI), Mode A only
│       │   └── client.go
│       └── cli/                          # primary adapter: CLI commands, calls into application.Session/.SessionManager
│           └── commands.go
└── go.mod
```

Recommended build order per the doc's section 8: **Mode B (AP-mode) first** — it's a single TCP connection with a fully byte-accurate spec and zero cloud dependency, versus Mode A's split TCP-coordinator/UDP-media architecture and dependency on naxclow's cloud staying available for the pairing/candidate-discovery phase. Concretely, that means: `domain/frame.go` → `application/transport.go` (the port) → `adapters/transport/aptcp` (the adapter) → `application/apsession.go` (wires them together, implements the handshake from doc section 12.3) → `adapters/cli` (a minimal command to exercise it end-to-end) — get one full vertical slice working before building out Mode A or the remaining SD-card/control commands.

## Development workflow

- Reach for the globally installed Go subagents (documented in `AGENTS.md`) for the corresponding kind of work rather than doing everything inline. Map agents to architecture layers: `go-feature` for domain and application-layer work (`Frame`, `Command`, `Session`/`SessionManager`, `RelaySession`/`APSession`, the use cases); `go-api-builder` specifically for the `naxclowapi` adapter (Mode A only); `go-cli-builder` for the `adapters/cli` primary adapter; `go-scaffold` once at the start to lay down the layered structure above; `go-test-generator` for tests at every layer; `go-reviewer` before merging anything wire-protocol-critical; `go-docs-generator` once an API surface (a port interface, the CLI) has stabilized.
- Wire-format code (`Frame` encode/decode, handshake state machines, command builders) is the highest-risk area for subtle, hard-to-notice bugs (byte order, field offsets, off-by-one framing). This is all domain-layer code and should be trivially unit-testable with no mocks — prioritize byte-exact golden tests built from the doc's tables before considering it done.
- When adding a new port, define it in the application-layer file that consumes it, not a shared `ports` package — see the Architecture section above.
- Keep the naming map above in sync as you go. If a `go-refactor` pass renames something, update this file in the same change.

## Workflow: small steps, one branch per step, test-first

- **Break every piece of work into the smallest independently mergeable slice you can** — one type, one port, one adapter, one use case, or one handshake step at a time. A small working slice beats a large batch of half-finished layers; don't try to land "the whole handshake" or "the whole Transport port + all adapters" in one branch.
- **One branch per step.** Branch before starting a step, keep it scoped to only that step (no drive-by refactors, no bundling an unrelated fix in), get it reviewed (`go-reviewer`), merge, then branch again for the next step. Don't stack unrelated changes onto a single long-lived branch.
- **Test coverage is not optional, especially in `domain` and `application`.** Those layers are pure and I/O-free by design (see Architecture section above) specifically so they can be tested exhaustively without mocks or a real camera — aim for near-complete coverage there. Adapters (real sockets, real HTTP) will necessarily have some edges that are only really verifiable against real hardware; cover what you can with fakes against the ports they implement, and say explicitly in the PR what's only been checked against the real device vs. only in isolation.
- **Use TDD wherever the expected behavior is already fully specified** — which, thanks to the findings doc, is true for most of the domain layer: `Frame` encode/decode has an exact byte table (doc sections 12.2a/12.3a), every command has an exact JSON shape (doc sections 12.5-12.7), every handshake step has an exact message sequence (doc sections 12.2/12.3). For this kind of work: write the test asserting the documented behavior first, watch it fail, then implement until it passes. The doc gives you the spec up front, so there's no reason to write the implementation blind and test it after.
- **Where TDD doesn't fit as cleanly**: anything whose exact behavior depends on the real camera's actual runtime response and isn't pinned down in the doc (flagged "unconfirmed" in doc sections 7/12.8 — an obscured command code, an unconfirmed header field, `connectDirect`/STA-mode discovery). There, a short spike against the real device (or the jadx source) to pin the behavior down first is reasonable — but once it's known, lock it in with a regression test in the same branch before moving on, and update the findings doc with what was learned.
- **Suggested first several branches, in order** (this is the vertical-slice progression referenced in "Proposed project layout" above, spelled out step by step):
  1. `domain`: `Frame` type + `Encode`/`Decode`, TDD'd against the byte table in doc 12.2a/12.3a.
  2. `domain`: `FrameType`/`Code` constants (doc 12.4-12.7) plus `Command` value objects for a first handful of device-control commands.
  3. `application`: the `Transport` port definition, plus an in-memory fake `Transport` used only in tests (no real socket yet).
  4. `application`: `APSession`'s handshake (doc 12.3), TDD'd end-to-end against the fake `Transport` from step 3.
  5. `adapters/transport/aptcp`: the real TCP adapter implementing `Transport` — tested against a local mock TCP listener where practical, then verified manually against the real camera.
  6. `adapters/cli` + `cmd/v720ctl`: one minimal command (e.g. `v720ctl ap connect`) wiring the above together end-to-end.
  7. From there, expand outward following the same domain → application → adapter progression: live-view streaming, SD-card commands, then Mode A (relay mode) once Mode B is solid.

## Build & test

Fill in once the module is scaffolded — standard `go build ./...`, `go test ./...`, `go vet ./...` are expected to apply; add any project-specific lint/build tooling here once it exists.
