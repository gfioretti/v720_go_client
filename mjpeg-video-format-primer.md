# MJPEG as a Video Format — A Primer

This explains what MJPEG actually is, why it's fundamentally different from "real" video codecs like H.264, and what that means practically for decoding the V720's live view.

---

## 1. What MJPEG actually is

**Motion JPEG (MJPEG)** is, literally, exactly what it sounds like: a sequence of complete, independent JPEG images, played back one after another fast enough to look like motion. Each frame is a standalone JPEG file — the same format your phone's camera saves photos as — with no relationship to the frame before or after it beyond "comes next in the sequence."

This is a genuinely different category of thing from what most people mean by "video compression." There's no codec state, no inter-frame dependency, no concept of a "stream" at the bitstream level at all — it's just: decode this JPEG, display it, decode the next JPEG, display it, repeat.

---

## 2. How JPEG compression works (just enough to matter here)

JPEG compresses a single still image by exploiting redundancy *within* that one image: it splits the image into small blocks, transforms each block into frequency components (a discrete cosine transform), then discards the frequency information human vision is least sensitive to (this is the "lossy" part — and also where the blocky artifacts you see in over-compressed JPEGs come from). Critically, **none of this looks at any other image.** Every JPEG is compressed and decompressed in complete isolation.

This is the property that defines everything about MJPEG's tradeoffs versus real video codecs.

---

## 3. MJPEG vs. "real" video codecs (H.264 and friends)

Modern video codecs — H.264, H.265, VP9, AV1 — get their dramatically better compression ratios primarily by exploiting **redundancy between frames**, not just within one frame. Consecutive video frames usually look almost identical (a security camera pointed at a mostly-static room is an extreme case of this), so instead of storing every frame as a full image, these codecs store:

- **Keyframes (I-frames)**: full, independently-decodable frames — conceptually similar to a JPEG, though usually still more sophisticated.
- **Predicted frames (P-frames, B-frames)**: encoded as *the difference* from a previous (or surrounding) frame, plus motion vectors describing how blocks of pixels moved. These frames are dramatically smaller than a full image because most of the actual data is "reuse what was already there, just shifted."

This is why H.264 can deliver watchable video at a fraction of MJPEG's bitrate for the same resolution — it's not compressing images better, it's exploiting a completely different kind of redundancy that MJPEG structurally can't touch, because MJPEG never looks at more than one frame at a time.

The direct consequence: **MJPEG needs meaningfully more bandwidth than H.264 for the same visual quality**, because every single frame carries the full cost of a JPEG image, with zero benefit from how similar it is to the frame before it.

---

## 4. Why this matters in practice

The tradeoff isn't purely negative — MJPEG's simplicity buys real advantages that explain why it's still used:

- **Trivial to decode.** Any standard JPEG decoder handles every frame. No specialized video decode pipeline, no hardware video decode chip needed, no codec licensing to worry about (JPEG itself is royalty-free; H.264 historically carried patent licensing considerations that mattered a lot to cheap hardware vendors).
- **Every frame is independently seekable and independently valid.** There's no "wait for the next keyframe" concept, because *every* frame is a keyframe. This matters a lot for a lossy, unreliable transport (like the UDP live channel we found in the V720 protocol) — if you lose one JPEG frame, you just get one dropped/skipped image. If you lose a P-frame in an H.264 stream, every subsequent frame that depends on it is corrupted until the next keyframe arrives, which is a much worse failure mode over an unreliable link.
- **Zero encode/decode latency from frame reordering.** H.264's B-frames in particular require the encoder to look ahead and the decoder to buffer and reorder frames, which adds latency. MJPEG has none of this — decode and display each frame the instant it arrives.
- **Trivial to encode on cheap hardware.** A basic camera sensor plus a JPEG encoder chip is inexpensive, well-understood, decades-old silicon. Real video encoding (motion estimation, rate-distortion optimization) needs meaningfully more compute than these devices are built with.

This combination — robust to packet loss, low latency, cheap to encode, simple to decode — is exactly the profile that makes sense for a $5 camera talking over an unreliable P2P/UDP link, which is presumably why the V720 uses it despite the `VIDEO_TYPE_H264` constant existing unused in the code we read (worth re-checking against a live capture in case it's used under some condition we haven't seen — see the findings doc's open items).

---

## 5. How MJPEG streams are typically delivered

The most common MJPEG delivery format you've probably already encountered without knowing it: many old-school "IP camera" web viewers use an HTTP response with content type `multipart/x-mixed-replace`, where the server just keeps writing `--boundary\r\nContent-Type: image/jpeg\r\n\r\n<jpeg bytes>\r\n` chunks forever, and the browser redraws an `<img>` tag with each new chunk. That's the entire "protocol" — no video container, no timestamps beyond arrival order, nothing.

The V720's approach is conceptually the same idea, just over its own custom framing instead of HTTP: each JPEG frame is delivered as the payload of one wire frame (see the findings doc's `Frame` format), tagged with packet type `1`, and handed to a callback (`onMjpegPacket`) as raw bytes — no video container, no explicit timestamp field observed, no frame-rate metadata. "Frame rate" here is purely a function of how often the camera happens to send a new frame; there's nothing to negotiate at the codec level.

---

## 6. Implementing this in Go

The practical upshot for your client: you don't need a video/multimedia framework to display live view. Go's standard library `image/jpeg` package decodes a single JPEG frame directly from bytes into an `image.Image` — that's the entire "video decoder" you need. Each frame you receive (per the `Frame`/`FrameTypeMJPEG` payload) gets handed straight to `jpeg.Decode`, and the resulting image gets drawn wherever you're rendering (a GUI canvas, re-encoded into a file, piped to a browser as an MJPEG HTTP stream yourself, etc.).

A few things worth building deliberately rather than assuming:

- **Pacing/display timing** is your application's responsibility, not the codec's — decide how you want to handle frames arriving at an uneven rate (display immediately vs. a small buffer for smoothness).
- **Audio/video sync** isn't handled by the codec either, since audio (G.711/PCM) arrives as entirely separate frames. If you need lip-sync for anything, you'll need to build your own timestamp-based alignment — there's no inherent mechanism tying an MJPEG frame to a specific audio sample the way a proper container format (MP4, MKV) would.
- **If you want to save footage to a file** rather than just display it live, the simplest option is to just concatenate the raw JPEG frames (an "MJPEG AVI" container is literally this, with a thin wrapper), or pipe frames into `ffmpeg` if you want real compression/a standard video file out the other end. Don't reach for a general video-processing library just to *decode* what's coming off the wire — that part is genuinely just JPEG decoding, frame by frame.

---

## 7. Summary, tied back to the V720 protocol

Packet type tag `1` in the `Frame` format is a complete, standalone JPEG image, full stop. There's no video codec state to manage, no keyframe/dependency tracking, no hardware decoder needed — just decode each one as it arrives with a standard JPEG library and display it. The tradeoff you're accepting versus a "real" codec is bandwidth (each frame costs what a full JPEG costs, with no cross-frame savings), in exchange for simplicity and robustness that make a lot of sense for this device's link characteristics. If live capture ever reveals the camera actually does send H.264 under some condition (the unused `VIDEO_TYPE_H264` constant flagged in the findings doc), that would need a real video decoder — but nothing you've built for MJPEG goes to waste, since it'd be handled as a separate, additional packet type.
