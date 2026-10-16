# Contributing

Thanks for your interest in improving `v720ctl` / `v720srv`. Issues and pull
requests are welcome.

## Scope & intent

This is an **interoperability project for hardware the developer owns** — a
reliable client/server for a camera you bought. It is **not** a tool to attack,
disrupt, or gain unauthorized access to any third-party service or device.
Contributions must stay within that scope:

- Only exercise the code against cameras **you own or are authorized to operate.**
- Don't add anything that targets third-party accounts, devices, or services, or
  that relies on anyone else's credentials.
- Don't commit the vendor's app, firmware, or decompiled source — only
  independently-written code and protocol documentation belong here. When citing
  the decompiled app, reference it; don't paste it in.

## Getting started

Requires **Go 1.26+** (`ffmpeg`/`ffplay` only for the `record`/`preview`
commands). Before opening a PR, all three must be green:

```sh
go build ./...
go vet ./...
go test ./...          # add -race for the concurrent server paths
gofmt -l .             # must print nothing
```

## Architecture

The project follows a Go-idiomatic DDD + Hexagonal (Ports & Adapters) layout:

- `internal/domain` — pure, I/O-free value objects and rules (the wire `Frame`,
  command builders, reassembly). No `net`, no goroutines.
- `internal/application` — use cases and the ports (plain interfaces) they
  depend on. Depends on the domain, never on a concrete adapter.
- `internal/adapters/*` — the edges (TCP/UDP transports, REST clients, the CLI).
- `cmd/v720ctl`, `cmd/v720srv` — composition roots.

Dependencies point strictly inward (adapters → application → domain). Keep
`domain` and `application` dependency-free and exhaustively unit-tested; they're
pure by design, so they don't need mocks.

## How to submit changes

- **Keep PRs small and focused** — the smallest independently-mergeable slice.
  One type, one port, one adapter, or one fix per PR; no drive-by refactors.
- **Test-first where the behavior is pinned by the protocol spec** (byte tables,
  JSON shapes, handshake sequences in
  `docs/v720-camera-findings-and-client-spec.md`). Write the failing test, then
  implement.
- **Wire-format code is the highest-risk area** (byte order, offsets, framing).
  Changes there must come with **byte-exact golden tests** against the spec
  tables, and should be reviewed carefully.
- **The protocol documentation is the source of truth.** If real hardware
  behavior contradicts a doc, update the doc (or add a marked discrepancy note)
  in the same PR — don't let a guess silently become "the spec." Log corrections
  in `docs/protocol-corrections-log.md`.
- Note in the PR description what was verified against a real camera versus what
  is still provisional.
- Follow the existing code style; match the surrounding naming and comment
  density. Never carry obfuscated Java names into Go — translate them via the
  naming map in `CLAUDE.md`.

## Licensing of contributions

By submitting a contribution, you agree it is licensed under the project's
[Apache License 2.0](LICENSE), per section 5 of that license. Only submit code
you have the right to contribute.
