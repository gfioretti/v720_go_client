# V720 / Naxclow Mini WiFi Camera — Reverse Engineering Findings & Independent Client Specification

Status: findings are from **static analysis only** (jadx/apktool decompilation of the official Android app). No live traffic has been captured yet. Several items below are flagged as open questions that should be resolved with a live packet capture before finishing the client implementation.

---

## 1. Device Overview

- Product: "V720" mini WiFi camera, also sold as "A9 Mini Camera" (~€5, AliExpress-class hardware)
- Backend/app vendor: Naxclow
- MCU: BL7252 / BK7252 (Beken)
- Official mobile app: broken on iOS at time of writing. Android APK **v3.2.8**, package `com.naxclow.v720`, downloaded from APKPure and statically analyzed.
- Prior art: the open-source project [`intx82/a9-v720`](https://github.com/intx82/a9-v720) has already reverse-engineered an earlier protocol revision of this same camera family, and ships a working Python client for local (AP-mode / STA-mode) access, including a "fake server" technique described in section 6.

---

## 2. Network Reconnaissance Findings (this specific device)

- Router: FRITZ!Box, 192.168.178.1
- Camera located at 192.168.178.99, MAC `22:C0:E0:12:BB:5A`
  - This MAC is **locally administered** (not a registered IEEE OUI) — consistent with cheap/unlicensed IoT WiFi modules.
  - The device was identified by process of elimination via `nmap -sn` scans before/after power-cycling the camera, diffing the host list (`comm -13 before.txt after.txt`).
- Printed device serial: `080c0e07D735`
  - Does **not** match the MAC seen on the network.
  - Format (mixed hex, includes letters) plus confirmation that the app bundles Google ML Kit's barcode scanner (`libbarhopper_v3.so`) strongly suggests this is the **P2P UID** used for QR-code-based pairing, not a MAC address.
- Full TCP port scan (80, 443, 554, 8080, 8000, 8081, 6667, 34567) on the camera → **all closed**. No local RTSP/HTTP/ONVIF server is exposed. This is expected for a cloud-relay/P2P architecture (see section 5) rather than a traditional IP camera with a local web/RTSP server.

---

## 3. APK Structure & Native Libraries

Package: `com.naxclow.v720`, version 3.2.8, distributed as a split APK (`.xapk`): base APK (`com.naxclow.v720.apk`) + per-locale/per-arch config APKs. Native libs live in `config.arm64_v8a.apk`.

Camera-relevant native libraries (`lib/arm64-v8a/`):

| Library | Role |
|---|---|
| `libnax-nano-rtsp.so` | **Misleadingly named** — not RTSP. Implements a custom lightweight WebRTC-style engine: SDP offer/answer, ICE/STUN/TURN negotiation, RTCP PLI (picture-loss-indication), and multiplexed logical channels for video/audio/file/command. See `NanoRtspNativeBridge.java`. |
| `libnaxplayer.so` | Media player/decode wrapper (`NaxPlayer.java`) |
| `libnaxsdl.so` | Rendering (SDL-based) |
| `libnaxffmpeg.so`, `libavcodec.so`, `libavformat.so`, `libavfilter.so`, `libavdevice.so`, `libavutil.so`, `libswscale.so`, `libswresample.so`, `libmp4v2.so`, `libffmpegkit.so` | Standard FFmpeg/FFmpegKit stack for decode/mux |
| `libnms.so` | Unconfirmed — possibly network/messaging service, not yet analyzed |

Not camera-protocol-relevant (generic third-party SDKs, safe to ignore): `libbarhopper_v3.so` (Google ML Kit barcode scanner, used for QR pairing), `libapplovin-native-crash-reporter.so` (AppLovin ads SDK), `libapminsighta.so`/`libapminsightb.so` (APM/telemetry), `libimage_processing_util_jni.so`/`libsurface_util_jni.so` (Android CameraX — used for the *phone's own* camera during setup, not the IP camera), `libtt_ugen_layout.so`, `libfile_lock_pgl.so`/`libpglarmor.so`/`libbuffer_pgl.so`, `libc++_shared.so`.

Audio codec: **G.711 A-law** (`G711Codec` class in the app; independently confirmed by the `a9-v720` project's own findings on this camera family).

Video codec: not directly confirmed from static analysis — FFmpeg's presence suggests H.264 is likely, but this should be verified against captured traffic.

---

## 4. Cloud Infrastructure

Extracted from `com.naxclow.base.IHttpService`:

| Purpose | Address |
|---|---|
| Main REST API | `https://v720.naxclow.com/` |
| Auth / ICE signaling host | `https://icev720.naxclow.com/` |
| WebRTC ICE signaling (WebSocket) | `ws://icev720.naxclow.com/v720/rtc/ice/` |
| STUN | `stun:icev720.naxclow.com:3578` |
| TURN | `turn:icev720.naxclow.com:3578` — username `b2228c1fb40d65a8`, password `4a7318ecb2228c1fb40d65a843e0ca9a`. These are **static and shared across every install** of the app, not per-account secrets. |
| Ads/telemetry | `https://tongji.naxclow.com/`, `http://applog.naxclow.com/` |
| Payments | `https://v720pay.naxclow.com/` |
| File download links | `https://v720.naxclow.com/app/api/DFSFdfsFile/dynamicAppDownloadLink` |

Domains relevant to DNS-based interception/self-hosting (own findings + `a9-v720` project): `*.naxclow.com`, `*.naxclow.com.cn`, `v720.naxclow.com`, `v720.p2p.naxclow.com`, `p2p.v720.naxclow.com`, `dl2.naxclow.com`, `icev720.naxclow.com`, `home.naxclow.com`, `logo.naxclow.com.cn`.

---

## 5. Cloud-Mediated Session Flow (as implemented by the official app)

1. `POST app/api/ApiAppUser/login` (or `register`) — obtain bearer token.
2. `GET app/api/ApiSysDevices/getDevicesListByType` — list devices bound to the account.
3. `POST app/api/ApiServer/watching` — validates the device password (`pwd` + `devicesCode`) before allowing a session.
4. `GET app/api/ApiServer/getA9VideoConf` — returns a `DeviceLiveBean` containing `host`, `tcpPort`, `pwd`, `tarPwd`, `domain`.
5. All device communication — control-plane and media-plane, local and cloud — funnels through a single class, `com.naxclow.rtc.NaxclowRtcEngine` (a singleton). `com.naxclow.common.sdk.NaxclowSdkUtil` is just a thin one-line-per-method facade in front of it (likely the bridge exposed to a JS/hybrid UI layer), with no protocol logic of its own.

**Revised transport model** (supersedes the earlier "raw-TCP vs. WebRTC" hypothesis — the real picture, read directly from `NaxclowRtcEngine`'s constants, is a classic P2P-camera connection ladder rather than a binary split):

- `P2P_MODE_DIRECT=1` → `P2P_MODE_NAT=2` → `P2P_MODE_FORWARD=3` — the engine tries a direct LAN/WAN connection first, falls back to STUN-assisted NAT traversal, then falls back to relaying through naxclow's forward/TURN-style server if that fails. `P2P_MODE_AP=4` is the separate mode used when the phone is connected to the camera's own AP-mode hotspot.
- `P2P_TYPE_TCP=1` / `P2P_TYPE_UDP=0` — the actual wire transport in all these modes is plain TCP or UDP, **not** a full WebRTC/ICE/DTLS-SRTP stack. The `reqSdpWebRtc`/`sendSdpWebRtc` methods (backed by `NanoRtspNativeBridge`, section 3) almost certainly reuse SDP's text format purely as a session-description container for candidate/session negotiation, not the full WebRTC standard. This is a materially easier target for a Go client than a real `pion/webrtc` integration would have been.
- `MEDIA_PLAYER_TYPE_RAW=10` / `RTMP=11` / `RTSP=12` — the engine also supports RTMP/RTSP as alternate playback modes, not just the raw P2P protocol. This likely explains the iOS/Volcano-Engine divergence noted in section 10: Android may default to the raw P2P engine while iOS defaults to (or exclusively uses) the RTMP path.
- `VIDEO_TYPE_H264=101` confirms H.264 (previously only suspected via FFmpeg's presence). `AUDIO_FRAME_TYPE_G711=1002` confirmed as one of four supported audio codecs, alongside PCM(1000)/AAC(1001)/MP3(1003).
- Error/status codes (`CODE_P2P_REGISTER_FAIL=3`, `CODE_FORWARD_REGISTER_FAIL=5`, `CODE_DEVICE_PROBE_FAIL=6`, `CODE_DEVICE_AP_DISCONNECTED=9`, `CODE_PLAYBACK_*` 10–13) corroborate the same register → probe → forward-fallback ladder, plus a distinct code path for SD-card playback sessions.

**Local discovery confirmed:** `NaxclowRtcEngine.connectDirect(String, int, int, DirectConnectCallback)` is a genuinely separate method from `connect(host, port, devId, ...)` (the cloud-relay path using `getA9VideoConf`'s `host`/`tcpPort`). Its success callback returns a `String` — almost certainly the discovered device's LAN IP. This is the local/AP-mode discovery entry point needed for Mode B of the client spec (section 8), and is invoked via `NaxclowSdkUtil.startDiscovery()`/`stopDiscovery()`.

**Current blocker (resolved — see section 12):** every method on `NaxclowRtcEngine` just does `executor.submit(new RunnableXXXX(...))`, deferring to dozens of small, heavily ProGuard-obfuscated `Runnable` classes (package `OooO00o.*`, e.g. `RunnableC0583OooOO0o` for `connect()`). `AbstractC3040OooO0oO` (`com.naxclow.rtc.net`) is the per-device session/socket object referenced throughout, and it has exactly **two concrete implementations** — `C3048OooOo0O` (P2P/cloud-relay mode) and `OooOOO0` (local AP-mode). Both have now been read; see section 12 for the extracted wire protocol.

---

## 6. Local / AP-Mode Access (bypasses cloud entirely)

- Camera-hosted WiFi AP SSID prefix: `Nax_<digits>` — confirmed both in the decompiled `Config.getConnectWifiSsids()` (filters scan results for this prefix) and independently in the `a9-v720` project's README.
- Default AP-mode camera IP (per `a9-v720`, not independently re-confirmed against this firmware/app version): `192.168.169.1`.
- No local web/RTSP/ONVIF interface exists in AP mode either — configuration and streaming happen via the camera's custom binary protocol.
- The `a9-v720` project already reverse-engineered an earlier protocol revision, providing:
  - File listing/download from the camera's SD card (`python3 src/a9_naxclow.py -f`, `-d`)
  - Live streaming (`-l`)
  - WiFi provisioning without the app (`--set-wifi SSID PASSWORD`)
  - A **"fake server" technique**: redirect all `*.naxclow.com` DNS queries (via a local dnsmasq instance) to a self-hosted server that speaks the expected cloud HTTP + MQTT protocol locally. This sinkholes the camera's outbound cloud calls entirely — the camera believes it's talking to naxclow's real servers, but everything stays on the LAN. This approach is attractive because it reuses existing protocol work rather than requiring a full from-scratch reverse-engineering effort.

**Open question:** it is not confirmed whether app v3.2.8 / the current camera firmware still uses the protocol the `a9-v720` script implements, or whether local/AP-mode sessions have also migrated to the WebRTC path (5b). This must be tested empirically.

---

## 7. Open Items — Recommended Before Finalizing a Client

1. **Resolve the remaining obscured command codes** — several JSON `"code"` values (and one JSON field name) are displayed by jadx as symbolic references into unrelated third-party constants, because the real value got inlined at compile time and jadx guessed the wrong source. Confirmed pattern, seen three times now: `com.vungle.ads.internal.protos.Sdk.SDKError.Reason.*` (ad SDK, section 12.5), `androidx.constraintlayout.core.motion.utils.TypedValues.CycleType.*` (section 12.6), and `com.ironsource.C2640d4.j.D` (ad SDK, section 12.2 phase 1 step 2). These are **not real field references** — in Android Studio, click into each one to read its actual value and update section 12. Remaining unresolved: `instLed`, `motorState`, `setRf433Dev`, `sfq`, two parameter-less codes (12.5), two SD-card delete-picture codes (12.6), and the `reqRegister` third field name (12.2).
2. **Read `OooO00o.OooO00o`** (`connectDirect`, local-LAN discovery) — the remaining unread piece needed to know whether STA-mode devices (camera already on the home WiFi, not in its own AP hotspot) are reached via the same `NaxclowApChannel` protocol against a discovered IP, or something else. (`RunnableC0617Ooooo00`/`registerDevice` has now been read — section 12.1a.)
3. **Read `C3050OooOoO`'s static factory methods** — would confirm exactly how `forwardId` and sequence numbers map onto the 20-byte header's still-unconfirmed fields (section 12.8).
4. **Live traffic capture** — now most valuable for confirming the remaining unconfirmed header-field values, the AP-mode port number, the UDP-vs-TCP candidate relationship (section 12.2 phase 2 step 2), and the actual video codec/packet-tag used in practice.
5. **Confirm the "serial number" `080c0e07D735`** is the pairing UID — test by entering it manually in a generic P2P camera app's "add device" flow, or trace its consumption through `DevicePresenter` and device-binding activities not yet reviewed in this pass.
6. **Confirm which video/audio codec combination is actually used at runtime** — `VIDEO_TYPE_H264`/`VIDEO_TYPE_JPEG` and the audio types are all defined as options; live capture will show which the camera actually sends. Packet type tag `1` = MJPEG is now confirmed as a distinct path from the H.264 constant, so it's worth checking whether live view actually uses MJPEG frames rather than H.264 in practice.

---

## 8. Independent Client Specification

Two target modes, since they have different dependency profiles and risk levels.

### Mode A — Cloud-relay client (mirrors the official app's WAN path) — now fully specified, see section 12

**Goal:** A standalone client that authenticates against naxclow's real cloud and streams the camera from anywhere, replacing only the broken UI — not the backend.

Required capabilities (superseding the earlier WebRTC-based plan — no `pion/webrtc` needed at all, this is plain TCP/UDP with JSON control messages):
- HTTP client for the `v720.naxclow.com` REST API: `login`/`register`, `getDevicesListByType`, `watching`, `getA9VideoConf` (yields `host`/`tcpPort`/`pwd`/`tarPwd`/`domain`).
- A TCP client implementing the 20-byte frame format (section 12.2a) and the `NaxclowServerChannel` handshake (section 12.2, phase 1): register (`100`), request forward IDs (`30`/`36`), report candidate addresses (`10`), confirm status (`52`), and send/receive all device commands wrapped in `301`.
- A UDP client implementing the same frame format for `NaxclowLiveChannel` (section 12.2, phase 2): candidate probing (`21`/`51`), quality telemetry (`54`/`55`), keepalive ping/pong (packet type tag `102` = pong on this path, per section 12.4), and the actual audio/video payload packets (type tags `1`=MJPEG, `4`=G.711, `6`=PCM), including the frame-reassembly logic needed since frames can span multiple UDP packets (section 12.2 phase 2 step 6).
- Video decode: MJPEG is confirmed as the packet-tag-`1` payload (trivial to decode — each is essentially a JPEG). Whether H.264 is used instead/also under some conditions is unconfirmed (section 7.6) — build for MJPEG first, add H.264 if live capture shows it's needed. G.711 A-law audio decode is simple and well-documented.
- Command codes for the full device control surface (PTZ, IR LED, video quality, etc.) are in section 12.5.

Risks/considerations: depends on naxclow's cloud remaining available for the initial registration/candidate-discovery phase (the coordinator role); uses the real account/auth system; the TURN/STUN credentials from section 4 turned out to be for the (largely unused) WebRTC/SDP path, not needed for this actual protocol.

### Mode B — Local-only client (fully bypasses cloud) — now fully specified for AP-mode, see section 12.3/12.3a

**Goal:** Talk to the camera directly over the LAN with zero cloud dependency, immune to the vendor breaking or discontinuing their backend.

With the full AP-mode protocol now documented (section 12.3), this is a straightforward implementation rather than requiring the `a9-v720` "fake server" workaround:
- TCP client implementing the 20-byte frame format (section 12.3a) and connecting to the camera's own AP-mode hotspot (`Nax_*` SSID, IP `192.168.169.1` per `a9-v720`, exact port TBD empirically).
- Implement the handshake exactly as in section 12.3: empty `115` ping → device echo → `501` register with the hardcoded `"NaxclowToken"` → time-sync (`4`) → separate `500`/`sk` password check.
- Command codes: sections 12.5 (device control) and 12.6 (SD-card/file transfer) apply directly, using `target`=devId.
- Live view: same MJPEG/G.711/PCM packet tags as Mode A, but all over the single TCP connection — no UDP, no frame-reassembly complexity from fragmentation across datagrams (TCP already guarantees ordered delivery), simplifying this path further.
- WiFi provisioning equivalent to `--set-wifi` (setting the camera to join the home network) still needs `RunnableC0617Ooooo00`/related classes if not covered by what's already documented — check before assuming it's out of scope.
- For **STA-mode** (camera already joined the home WiFi, not in its own hotspot) rather than AP-mode: local discovery is still needed (`connectDirect`, section 7.2, not yet read) to find the camera's LAN IP, but the same `NaxclowApChannel`-style protocol likely applies once connected — this needs confirmation.

**Recommended build order:** AP-mode (this section) is simpler than Mode A and doesn't depend on naxclow's cloud at all, even for initial pairing — build this first. The `a9-v720` project's Python implementation and "fake server" technique remain useful as a cross-check/fallback, but are no longer strictly necessary now that the native protocol is documented directly.

---

## 9. Reference Material

- `a9-v720` GitHub project: https://github.com/intx82/a9-v720 — existing open-source Python implementation, plus disassembled original-app sources in its `orig-app/` folder.
- App analyzed: V720 v3.2.8, package `com.naxclow.v720`, obtained as a `.xapk` from APKPure.
- Analysis method: `jadx` (Java decompilation) + `apktool` (resource/manifest/native-lib extraction). No dynamic analysis or live traffic capture has been performed yet — see section 7 for what that would add.

---

## 10. iOS App — Divergent Findings (Deprioritized)

The iOS `.ipa` was also briefly inspected. Its main executable is FairPlay-encrypted (`LC_ENCRYPTION_INFO_64`, `cryptid 1`, ~52MB encrypted region), so no code-level decompilation was possible without a jailbroken device — findings below are limited to bundled framework names (`Frameworks/` folder) and linked-library metadata (`otool -L`), both of which sit outside the encrypted region.

Notable divergence from the Android build: the iOS app does **not** appear to bundle anything resembling naxclow's custom `nax-nano-rtsp`/`naxplayer` WebRTC engine. Instead it links ByteDance's **Volcano Engine** live-streaming SDK — `TTSDKCore`, `TTSDKLiveBase`, `TTSDKLivePlayerLite`, `TTSDKLivePullVolcLog`, `TTSDKPlayerCoreLiveLite`, `TTSDKStrategyLite`, `TTSDKTTFFmpegLiveLite`, `TTSDKVolcLog`, `VolcBaseLog` — plus `Tquic.framework` (likely ByteDance's own QUIC transport). This suggests the iOS client may pull video via a standard CDN live-streaming protocol rather than the peer-to-peer WebRTC/raw-TCP paths used on Android. Unconfirmed, since the binary couldn't be disassembled — could also be unused/dead-code from a shared SDK template.

Corroborating findings (consistent with the Android side): `SocketRocket.framework` (WebSocket client, consistent with the same `ws://icev720.naxclow.com/v720/rtc/ice/` signaling channel), `AFNetworking.framework` (REST client, presumably the same `v720.naxclow.com` API surface), `AliyunOSSiOS`/`AliyunLogProducer` (Alibaba Cloud storage/logging, likely for cloud-recording playback), `aisr.framework` (matches `libaisr.so` on Android). Remaining frameworks are ad-mediation SDKs (AppLovin, AnyThink, InMobi, GDT, KSAd), not camera-protocol relevant.

**Decision:** iOS investigation is deprioritized for now — the Android app already gives a complete, decompilable protocol picture, and the client build is proceeding Android-first. This section is kept for reference in case the CDN-pull hypothesis becomes worth revisiting later (e.g. if the Android WebRTC/raw-TCP paths prove harder to implement than expected).

---

## 11. Recommended Tooling for Continued Investigation

- Open the `output_jadx/sources` folder directly as a project in Android Studio (File → Open). It won't build (no Gradle setup, external libs unresolved), but it indexes the `.java` files enough to enable **Find Usages** and **Navigate → Call Hierarchy** across the whole decompiled tree — far faster than grepping for call sites by hand.
- For abstract classes like `AbstractC3040OooO0oO` (section 5/7), use **Go to → Subclasses** to jump straight to the concrete implementation(s) doing the real socket I/O.
- The `OooO00o.*` package and `com.naxclow.rtc.net.*` classes are ProGuard/R8-obfuscated (meaningless class/method names) — expect to open several small `Runnable` classes to trace one logical operation end to end. Cross-referencing via the IDE is much faster than doing this from raw file dumps.

---

## 12. Concrete Wire Protocol (extracted from `C3048OooOo0O` and `OooOOO0`)

Both classes extend `AbstractC3040OooO0oO` and are the two concrete "session" implementations selected based on connection mode. Neither is full WebRTC/DTLS-SRTP, despite the `nax-nano-rtsp` naming and SDP-shaped method names seen in section 3.

**Transport (fully confirmed):** every channel class — `NaxclowApChannel`, `NaxclowServerChannel`, `NaxclowSdcardChannel` — is Netty **TCP** (`ChannelInitializer<SocketChannel>`, `NioSocketChannel`), except `NaxclowLiveChannel`, which is genuinely **UDP** (`ChannelInitializer<NioDatagramChannel>`). So the P2P/relay mode (`C3048OooOo0O`) really is a TCP-control + UDP-media split, confirmed: `NaxclowServerChannel` (TCP, registration/coordination) and `NaxclowSdcardChannel` (TCP, file/playback) mirror AP-mode's design almost exactly, while `NaxclowLiveChannel` (UDP) carries the actual live audio/video. AP-mode, by contrast, has no separate UDP path — everything, including live A/V, goes over its one TCP connection. The "p2p-udp" strings baked into several classes' log messages (including the TCP ones) are leftover/legacy naming, not accurate.

**One unified frame format across all four channels.** `NaxclowServerChannel` and `NaxclowSdcardChannel` use byte-for-byte the same `decode`/`encode` pipeline as `NaxclowApChannel` (section 12.3a) — same 20-byte header, same field order, same `LengthFieldBasedFrameDecoder` parameters. `NaxclowLiveChannel` (UDP) uses the identical 20-byte header layout too, just written directly into/read directly out of each `DatagramPacket`'s payload rather than a length-prefixed TCP stream (redundant since UDP already preserves datagram boundaries, but kept for consistency with the other three channels).

### 12.1 Two connection models

| | `C3048OooOo0O` (P2P / cloud-relay) | `OooOOO0` (AP-mode / local-direct) |
|---|---|---|
| Used for | `NaxclowRtcEngine.connect(host, port, devId, ...)` — the cloud-coordinated path, using `host`/`tcpPort` from `getA9VideoConf` (section 5) | Direct connection when the phone is on the camera's own `Nax_*` WiFi hotspot |
| Channel model | Three separate logical channels: `NaxclowServerChannel` (control/registration), `NaxclowLiveChannel` (A/V streaming), `NaxclowSdcardChannel` (file/playback) | Single `NaxclowApChannel` handling everything |
| Connection setup | Multi-step NAT-traversal candidate probing (see 12.2) | Direct connect + `"NaxclowToken"` handshake (see 12.3) |
| SDP-related methods (`reqSdpWebRtc`/`sendSdpWebRtc`) | **No-op** — empty method bodies. Not used on this path. | **Active** — sends `{"code":230,"devTarget":...,"sdp":...}`. The WebRTC-style SDP exchange found in section 3 (`NanoRtspNativeBridge`) appears to only matter for AP-mode/local sessions, not the primary cloud-relay live-view path. |

### 12.1a Mode selection and credential fields (from `RunnableC0617Ooooo00`, i.e. `NaxclowRtcEngine.registerDevice`)

This is the code that actually decides whether a `C3048OooOo0O` (P2P/relay) or `OooOOO0` (AP-mode) instance gets created for a given `devId` session — and it resolves a naming ambiguity from earlier sections.

- **The AP-mode-vs-relay choice is made by the caller, not auto-detected.** `registerDevice` takes an explicit `isApMode` boolean; `UdpDeviceActivity2` (the flow traced back in section 5) always passes `false` (i.e. always P2P/relay), and AP-mode is presumably chosen by a different UI flow that isn't triggered when the phone is on the camera's own hotspot instead of the home WiFi. There's no protocol-level negotiation of which mode to use.
- **A gating check that can silently no-op registration:** the `deviceInfo`/`ConfigDevice.getDeviceConfig(devId)` map (passed into `registerDevice`) is checked for a `"p2p"` integer key. If it's present and not exactly `0` (or missing entirely, which defaults to `-1`), **no session object gets created at all** — `registerDevice` becomes a silent no-op. Where `ConfigDevice` populates `"p2p"` from hasn't been traced, but this app likely supports multiple camera families/protocol variants gated by this flag; for the V720 it's presumably always `0`, but a Go client replicating this flow exactly would need to know this gate exists.
- **Resolves the credential field naming**, which section 12.2 flagged as unconfirmed. The three-argument `AbstractC3040OooO0oO` constructor (`OooO0o`, `OooO0oO`, `OooO0oo`) is populated as `(uid, pwd, domain)` for **both** modes, where `uid` is the **logged-in app user's account ID** (not the device ID), and `pwd`/`domain` come straight from `getA9VideoConf`'s `DeviceLiveBean`. Two more fields get set immediately after construction: `OooO` = `devId`, `OooOO0` = `tarPwd` (also from `getA9VideoConf`).
- **This directly resolves the P2P/relay register message from section 12.2 phase 1 step 2**: `NaxclowServerChannel.reqRegister`'s `{"code":100, "uid":..., "token":..., "<domain-field>":...}` sends `uid`=the app user's account ID, `token`=**the device's `pwd`** (not an app auth token — a naming coincidence), and the third field is `domain`. Similarly, every place a `"devToken"` field appears (codes `10`, `30`, `52` in section 12.2) is populated from `tarPwd`, distinct from the `"token"` field which is `pwd`. AP-mode's equivalent password field, `"sk"` in `{"code":500, "sk":...}` (section 12.3 step 7), is presumably also `pwd`, though the exact call site wasn't directly observed in this pass.

### 12.2 P2P/cloud-relay handshake (`C3048OooOo0O`) — now fully confirmed across both its channels

This mode runs two handshakes in sequence: first register on the TCP server/coordinator channel, then bring up the UDP live channel once the server has supplied addressing info.

**Phase 1 — TCP `NaxclowServerChannel` (coordination):**

1. Connect to `host:tcpPort` (from `getA9VideoConf`) — this is a coordination/rendezvous server, not necessarily the camera's own address.
2. On connect, client immediately sends a bare/empty frame (`new C3050OooOoO()`), then sends the register request: `{"code":100, "uid":"<app user ID>", "token":"<pwd>", "<field>":"<domain>"}` (`reqRegister`). Field values confirmed via section 12.1a: `uid` is the logged-in app account's user ID (not the device ID), `token` is actually the device's `pwd` value from `getA9VideoConf` (not an app auth token — naming coincidence). The third field's exact JSON key name is still a jadx-obscured constant (`com.ironsource.C2640d4.j.D`) needing an Android Studio lookup, but its value is confirmed to be `domain`.
3. Server responds `{"code":13, "status":200, "devIp":..., "devPort":..., "devNatIp":..., "devNatPort":...}` — two device candidate addresses. `devNatIp`/`devNatPort` is stored as the address checked against `P2P_MODE_DIRECT`, `devIp`/`devPort` as the address checked against `P2P_MODE_NAT` (yes, that mapping looks backwards from the field names — read directly from the code, not rationalized further). A background timer/handler then begins probing these.
4. Client requests a relay/forward ID in case direct connection fails: `{"code":30, "cliIp":..., "cliPort":..., "devIp":..., "devPort":..., "devTarget":..., "devToken":...}` (`reqForwardId`) → server responds `{"code":31, "status":200, "forwardId":"..."}`, which becomes the identifier attached to the live channel's traffic (see phase 2).
5. Separately, `{"code":36, "devTarget":...}` (`reqPlaybackId`) requests a forward ID specifically for SD-card/playback use → server responds `{"code":37, "status":200, "forwardId":"..."}`, used to register `NaxclowSdcardChannel`.
6. `{"code":10, "cliIp":..., "cliPort":..., "cliNatIp":..., "cliNatPort":..., "devTarget":..., "devToken":...}` (`reqConnect`, the TCP-channel version — distinct from the UDP-channel method of the same name) reports the client's own candidate addresses back to the server, presumably so the server can inform the device of them for its own NAT-traversal attempt.
7. `{"code":52, "devTarget":..., "devToken":..., "status":...}` (`reqSendDeviceStatus`) completes registration.
8. Server confirms P2P registration status: `{"code":101, "status":200}` → client opens the UDP live channel (phase 2). `{"code":101, "status":500}` → registration failed (`onError(3)`, matches `CODE_P2P_REGISTER_FAIL`).
9. Ongoing device presence: `{"code":53, "status":0}` = device went offline (`onError(4)`, matches `CODE_DEVICE_OFFLINE`); `{"code":53, "status":1}` = device online — if this is a fresh connection, server also relays a housekeeping `{"code":298}` and the same time-sync `{"code":4, "unixTimer":...}` message seen in AP-mode (confirms code `4` = universal time-sync regardless of transport mode).
10. All device control commands (the section 12.5 table: PTZ, IR LED, video quality, etc.) get sent through this channel wrapped in `{"code":301, "target":"<devId>", "content":{...}}` (`sendForwardCommand`) — the TCP-channel equivalent of AP-mode's `{"code":502, "content":{...}}` wrapper. Inbound `{"code":301, "target":..., "content":{"code":N, ...}}` carries the response, with the same inner-code convention as AP-mode's `502`: `0`=stream stop, `1`="open audio, close video", `2`="close audio, open video", `3`=stream start, and `4` or `200–300` = generic device message passthrough.

**Phase 2 — UDP `NaxclowLiveChannel` (live audio/video):**

1. Once phase 1 succeeds, the client binds a UDP socket and sends to the device/relay address supplied in phase 1.
2. A UDP-level exchange (processed by a background queue-draining task, not the TCP handler) uses its own `code` sub-namespace on this channel: `{"code":21, "ip":..., "port":...}` inbound gives a candidate to probe, the client probes it, and `{"code":51, "status":200}` inbound confirms success — the client then compares the responding address against the three candidates (`OooOo`/`OooOo0o`/`OooOo00`) to set the final `P2P_MODE` (`1`=Direct, `2`=NAT, `3`=Forward — constants from section 5). The exact relationship between this UDP-level candidate exchange and phase 1's `devIp`/`devNatIp` candidates is not fully confirmed — plausibly this is a secondary, finer-grained probe once the coarse candidates from phase 1 are known.
3. Ongoing quality telemetry: client sends `{"code":54, "timerStamp":"<millis>"}` periodically (on writer-idle); `{"code":55, "timerStamp":...}` inbound drives round-trip-time/frame-loss measurement, feeding an adaptive quality algorithm that periodically sends `{"code":<sfq-code>, "sfq":<value>}` back — but that specific feedback message goes out via the **TCP** server channel's `sendForwardCommand`/code-301 wrapper, not over UDP. (The "sfq" outgoing code itself is still one of the obscured ones — section 7.1.)
4. Keepalive: client periodically sends a bare ping frame (`sendPing()`, on writer-idle); packet type tag `102` = "pong" inbound resets a ping-timeout counter (`resetPingCount`).
5. Live audio/video data (`sendRecordData`) is sent framed via the same 20-byte header, with the `forwardId` from phase 1 threaded through — the exact field it occupies in the header isn't proven, but the 8-byte opaque field (section 12.2a) is a strong candidate given no other field is unaccounted for.
6. **Frame reassembly / fragmentation confirmed:** the receive path implements jitter-buffer-style reordering keyed on the header's sequence field, with the flag byte (offset 6) acting as a fragment/frame-boundary marker — specific values `-1`, `-4`, `-5`, `-6` are checked as special sentinels (`-4` in particular repeatedly flagged elsewhere as significant, likely "last fragment of frame"). This means individual video/audio frames can be split across multiple UDP packets and need reassembly by sequence number before decoding — relevant for the Go client's live-view implementation.

### 12.2a Byte-level packet framing — now confirmed identical across all four channels

The 20-byte header described in section 12.3a for `NaxclowApChannel` is used, byte-for-byte, by `NaxclowServerChannel`, `NaxclowSdcardChannel`, and `NaxclowLiveChannel` too (confirmed by reading their `decode`/`encode` Netty handlers, which are identical to `NaxclowApChannel`'s). This is genuinely one wire format for the whole application — see section 12.3a for the field table; it applies everywhere.

### 12.3 AP-mode handshake (`OooOOO0` + `NaxclowApChannel`) — fully confirmed, byte-accurate

1. TCP connect `NaxclowApChannel` directly to `host:port` (per `a9-v720`, camera's own AP-mode IP is `192.168.169.1`; exact port still to be confirmed empirically).
2. On connect (`channelActive`), client immediately sends an **empty** frame with packet type tag `115` (`reqLiveMotion()` — no JSON body, just the bare 20-byte header).
3. Device echoes back a type-`115` packet.
4. Client's dispatcher sees the inbound `115` and responds with `reqConnect(devId, "NaxclowToken")`, sending `{"code":501, "target":"<devId>", "token":"NaxclowToken", "unixTimer": <unix epoch seconds + local timezone offset in seconds>}`. `"NaxclowToken"` is a **hardcoded literal string** in the app, not derived from anything device-specific.
5. Device responds `{"code":501, "status":200, "target":"<devId>"}` on success.
6. Client replies with a time-sync message: `{"code":4, "devTarget":"<devId>", "unixTimer": <same timestamp format>}`.
7. Password/authorization is a separate exchange: client sends `{"code":500, "sk":"<password>"}` (`reqAuthorize` — `"sk"` is the actual JSON field name for the shared key/password); device responds `{"code":500, "status":200}` on success.
8. **Live view auto-starts; the client does not send a start command** (corrected 2026-08-05 by reading `OooOOO0`/`NaxclowApChannel` directly — supersedes the earlier reading of this step). `NaxclowApChannel.channelActive` calls `reqLiveMotion()` (the empty type-`115` frame from step 2) on connect, and after the handshake completes the device simply streams MJPEG/audio frames — there is **no** client-sent stream-start message. The `sendApCommand` method (which wraps its argument as `{"code":502, "content": {...}}`) is called from **exactly one place**: the device-control helper `OooO.OooO00o(map, channel)` (section 12.5). The stream-state inner `code` values `0`=stream stop / `1`="open audio, close video" / `2`="close audio, open video" / `3`=stream start are therefore **inbound only** — notifications the device sends to the client — not something the client emits to control streaming. To stop live view, the client closes the TCP connection. (This means every AP-mode device-control command in 12.5 is itself `502`-wrapped — see the corrected 12.5.)
9. Keepalive: packet type tag `101` = "pong" (`rspPong()`, sent via a distinct static factory `C3050OooOoO.OooO0O0()`), distinct from the `102` used on the P2P/relay path. The channel also has a Netty `IdleStateHandler` — on writer-idle it auto-sends a bare ping frame (`C3050OooOoO.OooO00o()`, no-args factory); on reader-idle it just logs a timeout warning rather than disconnecting.
10. SD-card metadata is delivered as **XML**, not JSON: packet type tag `8` = a chunk of an XML file, accumulated into a buffer until the received length matches a previously-announced `fileSize` (from a `{"code":403, "status":200, "date":..., "fileSize":...}` response), then parsed via the SAX-based XML handler noted in section 3's `AbstractC3040OooO0oO`.
11. Packet type tag `10` = G-sensor/orientation data: three little-endian `int16` values (x/y/z), used to compute a rotation angle via `atan2` — presumably drives auto-rotation/flip UI logic, not core to streaming.

### 12.3a Byte-level packet framing (`NaxclowApChannel`, TCP) — confirmed from Netty `decode`/`encode` handlers

Every frame on the AP-mode TCP connection has this exact 20-byte header, followed by the payload:

| Offset | Size | Field | Notes |
|---|---|---|---|
| 0 | 4 | payload length | int32, little-endian |
| 4 | 2 | packet type tag | int16, little-endian — the value from section 12.4's table |
| 6 | 1 | flag byte | purpose unconfirmed; a value of `-4` was seen elsewhere flagged as a "frame complete" indicator |
| 7 | 1 | unknown byte | purpose unconfirmed |
| 8 | 8 | opaque field | purpose unconfirmed — possibly a session token/ID fragment |
| 16 | 4 | sequence/counter | int32, little-endian |
| 20 | N | payload | N = the value read from the length field at offset 0 |

This exactly matches the pipeline's `LengthFieldBasedFrameDecoder(LITTLE_ENDIAN, maxFrameLength=8092, lengthFieldOffset=0, lengthFieldLength=4, lengthAdjustment=16, initialBytesToStrip=0, failFast=true)` configuration (4-byte length field + 16 more header bytes before the payload starts = 20 total), and independently matches the `+ 20` byte-counter adjustment seen in the channel's read handler. This is sufficient to hand-construct and parse packets without needing `C3050OooOoO`'s source — the `decode`/`encode` methods read/write these fields in this exact order.

### 12.4 Packet type tag (`short`, distinct from the JSON `"code"` field below)

| Tag | Meaning | Confirmed in |
|---|---|---|
| `0` | JSON command/response payload | Both |
| `1` | MJPEG video frame → `onMjpegPacket(devId, bytes)` | Both |
| `4` | G.711 audio frame | Both |
| `6` | PCM audio frame | Both |
| `7` | Misc/file-related queue | Both |
| `8` | SD-card XML file chunk | AP-mode |
| `10` | G-sensor/orientation data (3× little-endian int16) | AP-mode |
| `100` | No-op | AP-mode |
| `101` | Pong (AP-mode keepalive) | AP-mode |
| `102` | Pong (P2P/relay keepalive) | P2P/relay |
| `115` | Triggers AP-mode handshake (`reqConnect` w/ `"NaxclowToken"`) | AP-mode |

Video codec: packet tag `1` maps specifically to **MJPEG**, not H.264 — despite `VIDEO_TYPE_H264=101` existing as a defined constant (section 5), the code path actually read in both classes only handles MJPEG frames via this callback. Whether H.264 frames use a different, not-yet-seen tag value needs live capture to confirm (section 7.7).

### 12.5 JSON `"code"` field — outbound commands (app → device)

All sent as `{"code": N, ...params}` wrapped in a type-`0` packet, via `sendForwardCommand` (P2P/relay) or the `OooO.OooO00o(map, channel)` helper (AP-mode) — same codes/params on both paths.

| Code | Parameters | Purpose |
|---|---|---|
| 201 | `speedGrade` (int) | Video quality/HD grade |
| 202 | `IrLed` (int) | IR LED on/off |
| 203 | `sdMoveMode` (int) | SD card motion-detect recording mode |
| 204 | `s`, `p`, optional `url` (strings) | Unclear exact purpose — likely playback/stream-target related given the param names |
| 205 | `moveAlert` (int) | Motion-detection alert toggle |
| 206 | `moveGrade` (int) | PTZ move grade/sensitivity |
| 207 | none | Unclear — toggle/request, no extra params |
| 211 | none | Unclear — toggle/request, no extra params |
| 220 | `ledEI` (int) | White/status LED state |
| 221 | `setApPwd` (string) | Set the camera's AP-mode WiFi password |
| 222 | `camId` (int) | Switch camera (dual-lens devices) |
| 223 | none (devId only) | Status/keepalive-type request |
| 224 | none (devId only) | Status/keepalive-type request (AP-mode) |
| 230 | `devTarget`, `sdp` | SDP send — **AP-mode only**, no-op on P2P/relay |
| 299 | none | Unclear — likely disconnect/unregister signal |
| *(obscured)* | `instLed` (int) | Instant/flash LED — real code number needs Android Studio lookup (section 7.1) |
| *(obscured)* | `motorState` (int) | PTZ motor state — real code number needs lookup |
| *(obscured)* | `setRf433Dev` (int) | RF433 remote pairing/control — real code number needs lookup |
| *(obscured)* | `sfq` (int) | Adaptive frame-quality feedback (section 12.2 step 6) — real code number needs lookup |
| *(obscured, two variants)* | none | Two more toggle-style commands with no params — real code numbers need lookup |

### 12.6 JSON `"code"` field — SD-card / file transfer — confirmed exact integers, identical codes on both AP-mode and P2P/relay-mode

Same numeric codes are used by `NaxclowApChannel` (AP-mode, addresses with `target`=devId directly) and `NaxclowSdcardChannel` (P2P/relay-mode, addresses with `forwardId` — obtained via the phase-1 `36`/`37` exchange in section 12.2 — plus a separate `devId` field). `NaxclowSdcardChannel` additionally sends `{"code":110, "forwardId":...}` (`reqRegister`) on connect, before any of the codes below, which has no AP-mode equivalent.

| Code | Direction | Fields (AP-mode / P2P-relay-mode) | Purpose |
|---|---|---|---|
| 400 | out | `target` / `forwardId` | Request SD-card date list |
| 402 | out | `target`, `date` / `forwardId`, `date` | Request file name list for a date |
| 403 | in | `status`, `date`, `fileSize` | Response: pending XML file's size, ahead of type-`8` chunk delivery |
| 404 | out | `target`, `date`, `hours`, `minute` / `forwardId`, `devId`, `date`, `hours`, `minute` | Request AVI file info |
| 406 | out | same as 404, plus optional `udpPlayback`, `udpPlayBackSeq` (P2P-relay only) | Start AVI file stream (playback) — the P2P/relay variant can optionally request UDP delivery of the recorded stream |
| 408 | out | `target` / `forwardId`, `devId` | Stop AVI file stream |
| 412 | out | same fields as 404 | Delete media file |
| 414 | out | `target` | Request SD-card photo album info (AP-mode only, not seen on `NaxclowSdcardChannel`) |
| 416 | out | `target`, `albumPageId` | Request album pictures for a page (AP-mode only) |
| 418 | out | `target` | Stop album show (AP-mode only) |
| *(obscured)* | out | `target`, `albumPageId` | Delete a page of album pictures — real code needs Android Studio lookup (jadx substituted `TypedValues.CycleType.TYPE_CUSTOM_WAVE_SHAPE`, another unrelated-enum artifact like section 7.1) |
| *(obscured)* | out | `target`, `albumJpegId` | Delete a single album picture — real code needs lookup (`TypedValues.CycleType.TYPE_EASING` artifact). Given the surrounding range is evenly spaced 400–418, both obscured codes are very likely nearby unused even numbers, but this is a guess, not confirmed — don't hardcode without checking. |
| 110 | out (P2P-relay only) | `forwardId` | Register the SD-card TCP connection against a forward ID |

### 12.7 JSON `"code"` field — connection/auth (AP-mode, confirmed exact integers)

| Code | Direction | Fields | Purpose |
|---|---|---|---|
| 500 | out | `sk` | Authorize/password check — `"sk"` = shared key/password. Response: `{"code":500,"status":200}` |
| 501 | out | `target`, `token`, `unixTimer` | Connect/register (`reqConnect`) — `token` is always the literal string `"NaxclowToken"` |
| 501 | in | `status`, `target` | Registration confirmation |
| 502 | out | `content` (nested JSON) | Live-stream state wrapper (`sendApCommand`) — see 12.3 step 8 |
| 4 | out | `devTarget`, `unixTimer` | Time-sync reply, sent after a successful `501` registration |

### 12.8 Remaining gaps

All four `AbstractC3040OooO0oO`-related channel classes (`NaxclowApChannel`, `NaxclowServerChannel`, `NaxclowLiveChannel`, `NaxclowSdcardChannel`) have now been read. What's left:

- **Obscured command codes** — the two SD-card delete codes (12.6), plus `instLed`, `motorState`, `setRf433Dev`, `sfq`, and two parameter-less codes (12.5), plus the `reqRegister` third field name on `NaxclowServerChannel` (jadx shows `com.ironsource.C2640d4.j.D`, inferred but unconfirmed to be `"domain"`) — all need their real values confirmed via Android Studio "Go to Declaration" on the jadx-mislabeled constant references.
- **`C3050OooOoO`'s static factory methods** (`OooO00o()` no-args ping, `OooO00o(String)` /`OooO00o(String,String)` for JSON+forwardId, `OooO00o(byte[],String,int,short)` for binary A/V+forwardId+seq+type, `OooO0O0()` pong) haven't been read directly. Their *effect* is now inferable from usage (they populate the 20-byte header fields), but reading the class directly would confirm exactly how/where `forwardId` gets encoded into the header — the current best guess is the 8-byte opaque field (offset 8–15), unconfirmed.
- **Three still-unconfirmed header fields**: the flag byte at offset 6 is now understood to be a fragment/frame-boundary marker with sentinel values `-1`/`-4`/`-5`/`-6` (section 12.2 phase 2 step 6), but its full meaning isn't pinned down; the byte at offset 7 and the 8-byte field at offset 8–15 are still unconfirmed (best guess: forwardId/session-token related, per above).
- **The UDP-level candidate exchange** (`{"code":21}`/`{"code":51}` in section 12.2 phase 2) vs. the TCP phase-1 candidates (`devIp`/`devNatIp`) — plausibly a two-stage refinement, but the exact relationship isn't proven. A live capture would settle this.
- **`RunnableC0617Ooooo00`** has now been read — see section 12.1a for mode-selection logic and the resolved credential-field mapping. **`OooO00o.OooO00o`** (`connectDirect`, local-LAN discovery for STA-mode devices not in AP mode) is still unread — it determines whether STA-mode (camera already joined the home WiFi) uses the same `NaxclowApChannel`-style protocol against a discovered IP, or something else entirely.
- **Where `ConfigDevice.getDeviceConfig(devId)` populates its `"p2p"` gating value from** (section 12.1a) — not traced. Assumed to be `0` for the V720/this app version, but unconfirmed.
- Live traffic capture is now mainly useful for resolving the handful of items above plus confirming the actual video codec/packet-tag used in practice (section 5, section 12.4's note on MJPEG vs H.264).

**Bottom line: both Mode A (cloud-relay) and Mode B (AP-mode/local) in section 8 are now specified in enough detail to implement.** AP-mode is simpler (single TCP connection, no NAT-traversal, no forward-ID indirection) and remains the recommended starting point, but the cloud-relay path's TCP-control/UDP-media architecture, full handshake, and command vocabulary are now documented too — see section 8 for an updated build recommendation.
