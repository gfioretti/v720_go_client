# v720ctl / v720srv

An independent **Go client and self-hosted server** for the **"V720" / "A9 Mini" WiFi camera** (vendor: **Naxclow** — cheap AliExpress-class hardware on a Beken BK7252-class MCU running RT-Thread).

The official phone apps are unreliable, so this reimplements the camera's actual wire protocol directly in Go, derived from a reverse-engineering pass over the official Android app. Two binaries:

- **`v720ctl`** — command-line client. Connect to the camera over its own Wi-Fi hotspot (AP mode), stream live video/audio, record to MP4, drive device settings, browse the SD card, provision home Wi-Fi, and discover cameras on your LAN.
- **`v720srv`** — a self-hosted server the camera can dial out to (via a DNS redirect), so you can keep it running on your own network instead of the vendor cloud.

## ⚖️ Scope & intent — please read

This is an **interoperability project for hardware the developer owns.** The goal is to make a camera you bought work reliably without depending on a flaky vendor app or cloud. It is **not** a tool to attack, disrupt, or gain unauthorized access to any third-party service or device.

- Use it only against cameras **you own or are authorized to operate.**
- The reverse-engineering notes describe the vendor's own protocol and the constants embedded in its shipping app (e.g. shared, non-personal TURN credentials that are identical across every install). No third party's account data or private credentials are included.
- The repository does **not** redistribute the vendor's app or any decompiled source — only the independently-written Go implementation and protocol documentation.
- If cloud relay is used, it uses *your own* Naxclow credentials exactly as the official app would — never anyone else's.

## Status

| Component | State |
|---|---|
| **`v720ctl` AP-mode client** (camera hotspot, e.g. `192.168.169.1:6123`) | **Working** — connect, live view, device control, SD-card listing and Wi-Fi provisioning verified against a real camera. |
| **`v720ctl` relay-mode client** (self-hostable, no vendor account) | **Built** end-to-end; phase-1 coordinator handshake works. Live A/V over the UDP media channel is still being finished and is not hardware-verified. |
| **`v720srv` self-hosted server** | **Built to spec**, **pending real-camera validation.** DNS sinkhole + boot REST + TCP coordinator + UDP media + MJPEG viewer are implemented but not yet confirmed against hardware. |

Treat anything not marked "verified against a real camera" as provisional.

## Build

Requires **Go 1.26+**. [`ffmpeg`](https://ffmpeg.org/) / `ffplay` are needed only for the `record`/`preview` convenience commands.

```sh
go build ./...        # builds cmd/v720ctl and cmd/v720srv
go test ./...         # domain/application are covered exhaustively
```

Or install the client directly:

```sh
go install github.com/gfioretti/v720_go_client/cmd/v720ctl@latest
```

## Quickstart (AP mode)

Join the camera's Wi-Fi hotspot, then:

```sh
# Verify the handshake
v720ctl ap connect --addr 192.168.169.1:6123 --dev <devId>

# Live view in a window (needs ffplay)
v720ctl ap preview --addr 192.168.169.1:6123 --dev <devId>

# Browser control panel with live video + settings buttons
v720ctl ap serve --addr 192.168.169.1:6123 --dev <devId> --http :8080

# Record to MP4 with synced audio
v720ctl ap record --addr 192.168.169.1:6123 --dev <devId> --out clip.mp4 --duration 30s

# Tell the camera to join your home Wi-Fi, then find it on the LAN
v720ctl ap set-wifi --ssid <name> --pass <pwd> --dev <devId>
v720ctl scan
```

Run `v720ctl` with no arguments for the full command list.

## Documentation

- **[`docs/README.md`](docs/README.md)** — documentation index and detailed project status.
- **[`docs/v720-camera-findings-and-client-spec.md`](docs/v720-camera-findings-and-client-spec.md)** — the reverse-engineered protocol reference (wire frame, handshakes, command-code tables, REST surface), with confirmed-vs-inferred facts clearly flagged.
- **[`docs/self-hosted-server-deploy.md`](docs/self-hosted-server-deploy.md)** — runbook for deploying and validating `v720srv`.

## Architecture

Go-idiomatic DDD + Hexagonal (Ports & Adapters): a pure `internal/domain`, use cases and ports in `internal/application`, edge adapters in `internal/adapters/*`, and the two binaries as composition roots in `cmd/`. See [`CLAUDE.md`](CLAUDE.md) for the conventions.

## Contributing

Issues and PRs welcome — see **[`CONTRIBUTING.md`](CONTRIBUTING.md)**. In short: keep PRs small, keep `go build`/`vet`/`test` green, and the highest-risk area is wire-format code (byte order, offsets, framing) — those changes should come with byte-exact tests against the spec tables. The protocol documentation is the source of truth; if hardware behavior contradicts it, update the docs rather than letting a guess drift into the spec.

## License

[Apache License 2.0](LICENSE). © 2026 Gabriel Fioretti. See [`NOTICE`](NOTICE) for attribution and scope.
