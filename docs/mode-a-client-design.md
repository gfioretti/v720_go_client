# Mode A (cloud-relay / WAN) — client design & decisions

Durable record of the design decisions behind the Mode A build, so they can be
referred to later without re-deriving them from commits or chat. The wire
protocol itself remains authoritative in
[`v720-camera-findings-and-client-spec.md`](./v720-camera-findings-and-client-spec.md)
(esp. §5, §12.1a, §12.2) — this doc is about *how the client is built on top of
it*, not the protocol.

## What Mode A is here — the reframing

The findings doc's §8 "Mode A" describes a client that authenticates against
**naxclow's real cloud** and streams over the WAN. In this project that framing
is deliberately changed:

- The developer has **no naxclow account** and the camera was **never bound to
  naxclow's cloud**. A client hardwired to naxclow's backend is therefore useless
  here.
- Goal instead: a **self-hostable relay client**. The cloud rendezvous
  (`login` → `watching` → `getA9VideoConf`, doc §5) is abstracted behind a port so
  the rendezvous source is swappable — naxclow's real REST backend *or* a
  server the developer runs on their own box / AWS. This is the doc §6
  "fake server" direction (sinkhole `*.naxclow.com`, speak the protocol locally),
  applied at the client's port boundary rather than via DNS tricks alone.

### Verification boundary (important, state this in PRs)

Mode A **cannot be verified end-to-end against the real camera yet**. That needs
either:
1. a **camera-facing self-hosted server** — a separate, larger reverse-
   engineering effort covering the camera's *outbound* registration + MQTT wake,
   which the findings doc does **not** yet cover (`a9-v720` is the reference); or
2. a naxclow account with the camera cloud-bound.

So the pure layers (domain + application: the frame codec, handshake sequencing,
command building) are built TDD-first with near-complete coverage against fakes,
and the network adapters are **build-to-spec with hardware verification
explicitly deferred**. Every Mode A PR calls out what was checked in isolation
vs. left unverified against hardware.

## Architecture decisions

Built on the existing hexagonal/DDD structure (see `CLAUDE.md`): dependency
direction strictly inward (adapters → application → domain), ports are
consumer-defined Go interfaces, domain is pure and I/O-free.

### 1. `CloudAPI` port is stateful and auth-token-free at the interface

`internal/application/cloudapi.go`. Methods mirror doc §5:
`Login → ListDevices → Watching → GetA9VideoConf`. The interface exposes only
what the application needs — the account `UID` (for the coordinator register
`"uid"` field, doc §12.1a) and the `LiveConf` (getA9VideoConf's DeviceLiveBean).
Any auth/bearer-token state stays **inside the adapter**, deliberately absent
from the port.

*Why:* a self-hosted server adapter can then **no-op `Login`/`Watching`** and
just return a configured `Account`/`LiveConf`, without the port forcing a
naxclow-shaped auth token onto every implementation.

### 2. Relay transport uses a bundled `RelayTransport` port, not three `Transport`s

Relay mode has three channels (TCP coordinator, UDP media, TCP storage, doc
§12.1/§12.2). Rather than pass three bare single-channel `Transport`s (the
AP-mode port), a **new relay-specific port bundles them**:

```go
type RelayChannel interface { // a channel that moves whole Frames
    Send(ctx context.Context, f domain.Frame) error
    Receive(ctx context.Context) (domain.Frame, error)
}
type RelayTransport interface {
    Coordinator() RelayChannel // TCP control/coordination (phase 1)
    Media() RelayChannel       // UDP live A/V (phase 2)
    Storage() RelayChannel     // TCP SD-card/playback
    Close() error              // shuts down all three
}
```

*Why:* relay's three-channel shape is explicit at the port; lifecycle is one
`Close`; a test fake bundles three scriptable channels in one place. All four
channels share the identical 20-byte `Frame` (doc §12.2a), so each concrete
channel stays trivial; the UDP-only fragment-reassembly concern (doc §12.2 phase
2 step 6) is payload logic in the domain + media adapter, not part of the port.
The AP-mode single-channel `Transport` port is **left untouched**.

### 3. Credential-field mapping is fixed by doc §12.1a

- coordinator register (`code:100`): `uid` = app account UID (**not** device id),
  `token` = device `pwd`, third field = `domain` (JSON key still jadx-obscured,
  doc §12.8 — implemented as `"domain"`, marked unconfirmed in code).
- `devToken` (on codes 10/30/52) = `tarPwd`; `devTarget` = device id.

Modeled as `domain.Account{UID}` + `domain.LiveConf{Host,TCPPort,Pwd,TarPwd,Domain}`.

## Slice roadmap & status

Small, independently mergeable slices, one branch each, domain → application →
adapter, TDD where the doc pins behavior. Workflow: go-reviewer on wire-critical
code, then commit + merge ("auto rhythm").

| # | Slice | Status |
|---|---|---|
| 1 | domain: `ConnectionMode` + relay `Code`s + `NewRelayDeviceCommand` (301 wrapper) | **merged** |
| 2 | application: `CloudAPI` port + `Account`/`Device`/`LiveConf` value types | **merged** |
| 3 | application: `RelayTransport` port + `RelaySession` phase-1 coordinator handshake (doc §12.2 phase 1) | **merged** |
| 4 | application: phase-2 media bring-up + fragment reassembly (doc §12.2 phase 2) | **spec-pinned primitives merged**; async loop + reassembler deferred (see below) |
| 5 | adapters: relay transports (TCP coordinator/storage, UDP media) composed into a `RelayTransport` | planned |
| 6 | adapters: `naxclowapi` REST client (`CloudAPI`) — swappable; some request/response JSON shapes need pinning from jadx | planned |
| 7 | CLI: `relay` command group wired at the composition root | planned |

### Slice 4 is partly blocked on real-device behavior

Phase-2 (doc §12.2 phase 2) splits into a spec-pinned part and an underspecified
part:
- **Spec-pinned (buildable to spec):** the candidate-probe → `ConnectionMode`
  decision (compare the responding address to the candidates, map to
  Direct/NAT/Forward per §5), the quality-telemetry message shapes (54/55), and
  the keepalive (bare ping out, type-102 pong resets a timeout).
- **Underspecified (needs a real device / capture):** the UDP frame
  **reassembly** semantics — the doc says it is jitter-buffered by the header
  sequence field with the flag byte (offset 6) sentinels -1/-4/-5/-6 marking
  fragment boundaries, but the exact meaning of those sentinels is *not* pinned
  ("-4 likely 'last fragment'"). Building a byte-exact reassembler blind would
  turn a guess into "the spec", which the project rules forbid without a spike.
  A plausible design to validate later: reorder payloads by sequence, then feed
  the existing marker-based `domain.MJPEGReassembler` (JPEG SOI/EOI) — but this
  needs confirming against real traffic. Also underspecified: the exact
  async/timing behavior (probe retries, telemetry cadence) and how `forwardId`
  is encoded into the header (best guess: the 8-byte opaque field, §12.8).

Because the developer has no cloud/self-hosted server yet, none of phase 2 can be
observed. Recommendation: build the spec-pinned pieces with heavy UNCONFIRMED
flags, and defer the reassembler + async timing until a capture exists.

### Known unconfirmed items entering the build (flag in code, don't let them harden into "the spec")
- The register third-field JSON key (`"domain"`, doc §12.8).
- Client candidate-address discovery (STUN / socket bind) feeding codes 30/10 —
  the exact mechanism and its relationship to the phase-2 UDP candidate exchange
  (21/51) is not fully confirmed (doc §12.2 phase 2 step 2, §12.8).
- The `status` value on `reqSendDeviceStatus` (code 52) — doc shows the field but
  not the value.
