# TCP vs UDP Framing — A Primer

This explains what TCP and UDP actually guarantee at the transport layer, why that leads to two completely different "framing" problems, and how each one maps onto what we found in the V720 protocol's four channels.

---

## 1. The core thing to unlearn: TCP has no concept of "messages"

TCP is a **byte stream**, not a sequence of messages. This is the single most important thing to internalize, because it's counterintuitive coming from almost any application-level API. When you write data to a TCP connection three separate times, TCP gives no guarantee about how the receiver's reads line up with your writes. The receiver might:

- get all three writes in a single `read()` call, concatenated together
- get one write split across two or more `read()` calls
- get some combination of the above, depending entirely on network conditions, buffering, and timing

TCP guarantees the bytes arrive **in order** and **reliably** (retransmitting anything lost) — but it treats them as one continuous stream, with zero awareness that your application considers them to be separate "messages" at all. If your application needs message boundaries, *you* have to invent and encode them yourself; TCP will not preserve them for you.

This is why the Java code we read pipelines a `LengthFieldBasedFrameDecoder` onto every TCP channel (`NaxclowApChannel`, `NaxclowServerChannel`, `NaxclowSdcardChannel`). It exists specifically to solve this problem.

---

## 2. Framing: how you recover message boundaries from a byte stream

"Framing" is the general term for any scheme that lets you carve a continuous byte stream back into discrete messages. The common approaches:

- **Length-prefixed framing** (what the V720 protocol uses): before each message, write a fixed-size field stating how many bytes follow. The reader reads that fixed-size field first, then reads exactly that many more bytes to get one complete message, then repeats. This is what the 4-byte little-endian length field at the start of every `Frame` is for.
- **Delimiter-based framing**: mark the end of each message with a special byte sequence (e.g. HTTP headers ending in `\r\n\r\n`, or newline-delimited protocols like Redis's older text protocol). Works well for text protocols; risky for arbitrary binary payloads, since you have to guarantee the delimiter sequence never appears inside the payload itself (or escape it when it does).
- **Fixed-length framing**: every message is exactly N bytes. Simplest possible scheme, but inflexible — no good for variable-sized payloads like a JSON command versus a JPEG video frame, which is exactly the mix the V720 protocol carries.

Length-prefixed framing is the right choice for a protocol carrying wildly different payload sizes over the same connection, which is why it's what got used here.

**What `LengthFieldBasedFrameDecoder` (or your own equivalent) actually does, conceptually:** it sits between the raw TCP stream and your application, buffering incoming bytes until it has a complete frame (the length field plus that many payload bytes), and only then hands a complete, ready-to-parse frame up to your code. It might need to wait across many underlying `read()` calls to accumulate one frame, or it might get several complete frames in a single `read()` call and need to deliver them one at a time — both cases are normal, and the decoder's whole job is hiding that variability from you.

---

## 3. UDP: the opposite problem

UDP datagrams, unlike a TCP stream, **do** preserve message boundaries — each `send()` on the sending side becomes exactly one datagram, delivered (if it's delivered at all) as exactly one discrete unit on the receiving end. A single read from a UDP socket never gives you a partial datagram or multiple concatenated datagrams the way TCP can. So, ironically, UDP doesn't need the length-prefixed framing machinery from section 2 at all — you already get one message per read, for free.

What UDP *doesn't* give you, though, is everything TCP quietly handles for you at the transport layer:

- **No ordering guarantee.** Datagrams can arrive in a different order than they were sent, if they take different network paths or one gets delayed.
- **No delivery guarantee.** Datagrams can simply vanish — dropped by a congested router, a flaky WiFi link, whatever — with no retransmission and no notification to either side that it happened.
- **No duplicate protection.** In rare cases (network-level retransmission at a lower layer, routing anomalies) the same datagram can be delivered more than once.
- **No connection state.** There's no persistent "connection" object at the transport layer the way TCP has — no built-in way to detect "the other side went away" versus "the network is just being slow."

This is the tradeoff: TCP hands you a clean, ordered, reliable byte stream but makes you build your own message-boundary logic. UDP hands you clean message boundaries for free but makes you build your own ordering, reliability, and liveness logic — if you need any of those at all.

---

## 4. Why use UDP if it's missing so much?

For real-time media specifically, most of what TCP provides is actively unwanted. If a video frame is a few hundred milliseconds late, retransmitting it and waiting is *worse* than just dropping it and moving on — by the time a TCP retransmit arrived, that frame would already be stale, and worse, TCP's in-order delivery guarantee means every packet *after* the lost one would also be stuck waiting behind it (this is sometimes called "head-of-line blocking"), stalling the entire stream over a single dropped packet. Real-time video would rather skip a frame and keep going than freeze waiting for one to be recovered.

There's also the NAT-traversal reason covered in the earlier primer: UDP hole-punching is far more reliable than TCP hole-punching, which is a large part of why peer-to-peer media protocols default to UDP whenever a direct/NAT-traversed path is being attempted.

---

## 5. What you have to build yourself on top of UDP

Since none of TCP's guarantees come for free, a real-time UDP protocol has to reinvent the specific pieces it actually needs:

- **Sequence numbers.** A per-packet counter, so the receiver can detect gaps (something was lost) and reordering (something arrived out of sequence). This is exactly the 4-byte sequence/counter field in the V720 `Frame` header.
- **A jitter buffer / reordering buffer.** Since packets can arrive out of order, you can't just process them the instant they arrive — you buffer briefly, use the sequence numbers to put them back in order, and only then hand them to the application. Too short a buffer and you drop packets that would've arrived "in time" with a little patience; too long a buffer and you add latency. This is exactly what the frame-reassembly logic we found in `NaxclowLiveChannel` is doing — buffering by sequence number and flushing runs of consecutive packets.
- **Fragmentation and reassembly, if a logical message is bigger than one datagram comfortably holds.** A typical safe UDP payload size is well under 1500 bytes (network MTU, minus headers, minus margin for anything in between that might add its own encapsulation). A single JPEG video frame is very often bigger than that, so it has to be split across multiple UDP packets and reassembled on the other end. This is almost certainly what the flag byte in the V720 `Frame` header is for — the `-1`/`-4`/`-5`/`-6` sentinel values we found are marking fragment/frame-boundary state (`-4` repeatedly flagged as significant, likely "this is the last fragment of the current frame") so the receiver knows when it's collected all the pieces of one logical frame and can hand the reassembled whole to the application.
- **Keepalives**, covered in more depth in the NAT-traversal primer — since there's no transport-level "connection," you need your own periodic ping/pong to both keep any NAT mapping alive and detect that the other side has actually disappeared, rather than just being briefly slow.

---

## 6. Building this in Go

**TCP side:** Go's `net.Conn` gives you exactly the same raw byte-stream reality as the Java `SocketChannel` we read — `Read()` can return fewer bytes than you asked for, and there's no built-in message framing. The standard-library tool for reading an exact number of bytes reliably is `io.ReadFull(reader, buf)`, which blocks until either it's filled the buffer completely or hit an error — this is the direct equivalent of what `LengthFieldBasedFrameDecoder` does under the hood. A minimal Go frame reader is: `io.ReadFull` 4 bytes for the length field, decode it with `encoding/binary`, then `io.ReadFull` that many more bytes for the payload. Wrap this in a loop and you have your own frame decoder, no external framework needed.

**UDP side:** `net.ListenUDP`/`net.UDPConn.ReadFromUDP` already gives you one complete datagram per call — no framing decoder needed there. What you do need to write yourself: the sequence-number-based jitter buffer and fragment reassembly described above. There's no standard-library equivalent to reach for here since this logic is inherently application-specific (it depends entirely on your own sequence-numbering and fragment-marking scheme) — this is genuinely one of the more interesting pieces of domain logic in the whole client, and a good candidate for careful, well-tested implementation given how easy it is to get subtly wrong (off-by-one in a reorder window, or mishandling the "flush what you have" case when a fragment never completes).

---

## 7. Summary, tied back to the four V720 channels

| Channel | Transport | Framing needed | Why |
|---|---|---|---|
| `NaxclowApChannel`, `NaxclowServerChannel`, `NaxclowSdcardChannel` | TCP | Length-prefixed frame decoder (`io.ReadFull`-based in Go) | TCP is a byte stream with no message boundaries — you must reconstruct them |
| `NaxclowLiveChannel` | UDP | None for boundaries (free from UDP) — but sequence tracking, jitter buffer, and fragment reassembly instead | UDP preserves message boundaries but guarantees nothing else — ordering, reliability, and multi-packet reassembly are all your responsibility |

Same 20-byte `Frame` header format across all four, but the surrounding machinery each one needs is almost the inverse of the other — which is exactly why this is worth understanding well before writing the Go equivalents, rather than assuming "framing" means the same thing on both.
