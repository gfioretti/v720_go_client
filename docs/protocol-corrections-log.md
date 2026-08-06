# AP-mode protocol corrections log

A running record of wrong assumptions made while building the AP-mode client and
the corrections applied once real hardware (or a closer read of the decompiled
app) contradicted them. The authoritative protocol description lives in
`v720-camera-findings-and-client-spec.md`; this file is the *why it changed*
companion, so a future reader understands which "facts" were provisional and how
they were resolved.

**Root cause pattern.** The findings doc was built from **static analysis only**
(jadx decompilation, no packet capture) — it says so at the top. Static reading
is reliable for byte layouts and message codes, but repeatedly got the **runtime
sequencing** wrong: what the device actually requires, in what order, and what it
does when a step is malformed. Every correction below came from either running
against the real camera or re-reading the app once the camera disagreed. When in
doubt about *sequencing*, trust the device over the decompilation.

---

## 1. AP-mode address was "TBD empirically"

- **Assumption:** the AP-mode `host:port` had to be discovered at runtime; the doc
  left it as "port TBD empirically."
- **Correction:** it is hardcoded **`192.168.169.1:6123`** throughout the app
  (`ApRotateDeviceActivity.openConnection`, `UdpDeviceActivity.setHost/setTcpPort`,
  `startDiscovery`). This removed the main blocker to real-hardware testing.
- **How found:** re-reading the decompiled activities.
- **Source:** doc §12.3 step 1. Commit `6afd1cd` (2026-08-06).

## 2. Password authorization (code 500) was treated as a blocking handshake step

- **Assumption:** the handshake was `115 → echo → 501 → time-sync → 500 authorize`,
  and `Connect` blocked awaiting the `{"code":500,"status":200}` ack before
  considering the session up.
- **Symptom:** against a real (freshly reset, no-password) camera, connect got all
  the way through the time-sync and then **hung until timeout** waiting for a 500
  ack the camera never sends.
- **Correction:** authorization is a **separate, best-effort exchange, not part of
  the handshake.** The app's `OooOOO0` dispatcher only auto-chains
  `115 → 501 → time-sync`; `reqAuthorize` is invoked independently and its response
  is only *logged*, never gated on. `Connect` now establishes the session after the
  time-sync, sends the 500 only when a password is configured, and never awaits it.
  Removed the now-unused `ErrHandshakeAuth`.
- **How found:** real camera; the blocked `Connect` timed out.
- **Source:** doc §12.3 step 7. Commit `3383d8b` (2026-08-06).

## 3. "Live view auto-starts; the client sends no start command" — WRONG (and was itself a correction)

This one was corrected twice. The history matters because it shows how a confident
static-analysis "correction" can still be wrong.

- **v1 assumption (earliest):** the client sends an explicit stream-start command
  as part of live view.
- **v2 "correction" (2026-08-05, commit `d152d74`):** after reading
  `OooOOO0`/`NaxclowApChannel`, concluded AP-mode live view is **receive-only** —
  the type-115 trigger *is* the request to stream, the device auto-streams after
  the handshake, and the stream-state codes `0/1/2/3` are **inbound-only**
  notifications. Tests were written asserting `StartLiveView` sends **zero** frames.
- **Symptom:** against the real camera, connect/handshake succeeded (`exit=0`) but
  live capture produced **0 video / 0 audio frames** — the device sent nothing at
  all after the time-sync, not even a keepalive.
- **v3 correction (2026-08-06, commit `b66ab57`): live view is client-started.**
  After the handshake the device sends a **502-wrapped ready notification**
  (`{"code":502,"content":{"code":4,...}}`, surfaced as `onDeviceMessage(code==4)`),
  and the app replies with `setLiveStreamMode(devId, 3)` →
  `{"code":502,"content":{"code":3,"devTarget":...}}`. Only then does the device
  stream. `StartLiveView` now sends this stream-start command before reading frames.
  The stream-state codes are therefore **bidirectional**, not inbound-only: the
  client sends `3` (start) / `0` (stop), and the device also sends `0/1/2/3` back
  as notifications.
- **Trail in the app:** `ApRotateDeviceActivity`/`UdpDeviceActivity` `onDeviceMessage`
  → `NaxclowSdkUtil.setLiveStreamMode(devId, 3)` → `NaxclowRtcEngine.setDeviceLiveStreamMode`
  → `OooOOO0.OooOO0o(int,String)` → `OooO.OooO00o` → `NaxclowApChannel.sendApCommand`.
- **Source:** doc §12.3 step 8. Commits `d152d74` (wrong), `b66ab57` (fix).
- **Open detail (needs real-hardware confirmation):** the app gates sending
  stream-start on *receiving* the device's 502/code-4 ready notification. The Go
  client currently sends it **proactively** right after `Connect`, relying on TCP
  ordering to place it after the time-sync. If a real capture shows the device
  ignores an early stream-start, the fix is to have `StartLiveView` wait for the
  inbound 502/code-4 before sending. Flagged here so it isn't silently assumed
  solved.

## 4. Time-sync was sent unwrapped as a top-level `{"code":4,...}`

- **Assumption:** the step-6 time-sync was a top-level message
  `{"code":4,"devTarget":...,"unixTimer":...}` (as the doc's §12.3 step 6 stated).
- **Correction:** it is **502-wrapped**. The app builds the flat map
  `{"devTarget","code":4,"unixTimer"}` and sends it through the *same*
  `OooO.OooO00o → sendApCommand` path every device command uses, so the wire form
  is `{"code":502,"content":{"code":4,"devTarget":...,"unixTimer":...}}`. By
  contrast the `501` register and `500` authorize go through the direct
  `reqConnect`/`reqAuthorize` channel methods and are **not** wrapped.
- **Symptom / why it matters:** sent unwrapped, the real camera stays **completely
  silent** afterward — no ready notification, no stream. This is almost certainly
  why the device never emitted the 502/code-4 that would trigger streaming, so it
  is entangled with correction #3; both had to land together for live view to work.
- **How found:** tracing the exact send path of the time-sync in `OooOOO0`'s
  dispatcher (it calls `OooO.OooO00o`, the 502-wrapper), prompted by the "device is
  silent after handshake" symptom.
- **Source:** doc §12.3 step 6. Commit `b66ab57` (2026-08-06).

---

## Lessons for whoever picks this up next

- **Static analysis nails structure, not sequencing.** Byte layouts, field names,
  and message codes read straight off the decompiled channels have held up. The
  order of messages, which side sends what, and error/idle behavior have been wrong
  more often than right — confirm those against the device.
- **Watch for send-path wrappers.** Several messages that *look* top-level in the
  data they carry are actually pushed through `sendApCommand` and come out
  502-wrapped. Whether a given message is wrapped depends on *which method sends it*
  (`reqX` direct vs. `OooO.OooO00o`), not on its code.
- **A "correction" is not automatically right.** Correction #3 was itself a
  confident rewrite that the hardware later disproved. Prefer landing a change and
  validating it against the camera over trusting a second static read.
- **Keep the findings doc authoritative.** Each correction above was mirrored back
  into `v720-camera-findings-and-client-spec.md` in the same change, so the spec and
  this log never drift.
