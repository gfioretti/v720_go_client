# NAT Traversal & P2P Networking — A Primer

This explains the general concepts behind how two devices behind home routers manage to talk to each other directly, why that's harder than it sounds, and how the tricks involved (STUN, hole punching, TURN, ICE) map onto what we found in the V720 camera's protocol.

---

## 1. The problem NAT creates

Your home router runs **NAT (Network Address Translation)**. Every device on your home network — your laptop, your phone, the camera — has a private IP address (like `192.168.1.42`) that only means something inside your house. The internet at large only sees your router's one public IP address. When your laptop makes a request to a website, the router rewrites the outgoing packet's source address to its own public IP (and picks a temporary external port), remembers that mapping, and rewrites the reply back to your laptop's private address when it comes in.

This works great for the common case — a device inside your network initiating a connection *out* to a public server. It breaks down for the opposite case: **a device outside your network trying to initiate a connection *in* to something behind your NAT.** There's no mapping for that yet, so the router has no idea which internal device the incoming packet is for, and drops it. This is why you can't normally "just connect" to your camera from outside your home network, or from another phone on a different network — both the phone and the camera sit behind NATs that reject unsolicited inbound traffic by default.

This is the entire reason P2P camera apps (and video calling apps, and torrent clients, and every other "connect two home networks directly" application) need all the machinery below. The goal is always the same: get two NAT'd devices to end up with a working, non-relayed, direct connection to each other, without either device being able to just "listen" on the public internet.

---

## 2. Types of NAT

Not all NAT behaves the same way, and the difference determines whether traversal is even possible without a relay. The classic taxonomy (from the original STUN RFC) has four types, but the important distinction in practice is simpler than the full list:

- **Cone NAT** (full cone, restricted cone, port-restricted cone — three flavors, decreasing in permissiveness): once your device sends a packet out from a given internal `IP:port`, the router assigns it a consistent external `IP:port` mapping, and that mapping stays the same no matter which external host you're now talking to. This is the traversable case — if a peer can learn what that external mapping is, they can send packets to it and your NAT will let them through (with varying restrictions on *who* is allowed to send to it, depending on the cone sub-type).
- **Symmetric NAT**: the external port your router assigns is different for *every distinct destination* you talk to. If you send a packet to server A, you get external port 40001; send a packet to server B, and you get external port 40002 for the *same* internal socket. This breaks the traversal trick below, because there's no single external address a peer can learn and target — the address a STUN server sees you from will be different from the address your actual peer would need to send to. Symmetric NAT is common on carrier-grade/mobile networks and some enterprise firewalls.

Most home routers use some form of cone NAT, which is why P2P generally works from home networks. Symmetric NAT is the case that forces a fallback to relaying (section 6).

---

## 3. STUN — discovering your own address

Before two devices can try to connect to each other, each one needs to find out **what its own NAT'd address looks like from the outside** — because it doesn't know this on its own. Your device only knows its private address; it has no visibility into what public `IP:port` the router is presenting it as.

**STUN (Session Traversal Utilities for NAT)** solves exactly this, and only this. A STUN server is a small, dumb, public server whose entire job is: receive a UDP packet, and reply telling the sender "here's the IP and port I saw this packet come from." Since that packet passed through the NAT on the way out, the address the STUN server sees *is* the NAT's external mapping. Your device now knows its own reflexive (NAT-mapped) address, and can share it with a peer through some other channel (see next section).

This is a lightweight, stateless service — a STUN server doesn't relay any actual traffic, doesn't need to know anything about your application, and a single public STUN server can serve enormous numbers of clients cheaply. This is why Google's public STUN servers, and similar free ones, are widely reused across unrelated apps.

---

## 4. Hole punching — the actual traversal trick

Knowing your own external address isn't enough — you also need to get your peer's external address, and then get both NATs to accept traffic from each other simultaneously. This requires a third party both devices already trust and can reach: a **rendezvous/signaling server**.

The sequence, in general terms:

1. Both devices connect out to the same rendezvous server (an outbound connection, so it works through NAT with no special tricks).
2. Each device either uses STUN (against that server, or a separate STUN server) to learn its own external address, or the rendezvous server observes it directly the same way a STUN server would.
3. The rendezvous server tells each device the other's external `IP:port` (this exchange of connection info is often called "signaling," and this candidate-address data is often called an "offer"/"answer" or "candidate" in various protocols — this is literally what "SDP" and "ICE candidates" refer to in WebRTC).
4. **Both devices now send UDP packets directly to each other's external address, at roughly the same time.** This is the "punch." Each outbound packet causes that device's own NAT to open up a mapping (the same cone-NAT behavior from section 2) that will accept a return packet from that specific external address. If both sides do this within a short window, each side's outbound packet arrives *just as* the other side's NAT has opened the matching hole, and packets start flowing directly, peer-to-peer, with no relay.

The rendezvous server is only involved in this initial address exchange — once the hole is punched, all further traffic goes directly between the two devices, which is the whole point (lower latency, no bandwidth cost to the relay operator, works even if the rendezvous server later goes offline).

This is exactly the pattern we found in the V720 relay-mode handshake: a coordination server hands out candidate addresses, the client probes them, and a successful probe response confirms the hole is punched.

---

## 5. Why it sometimes fails: symmetric NAT

If either device is behind symmetric NAT (section 2), the address the rendezvous/STUN server observed is *not* the address that will work for a peer-to-peer packet — because symmetric NAT assigns a different external port per destination. The address that was valid for "talking to the rendezvous server" won't be valid for "talking to the actual peer." No amount of retrying the punch will fix this; it's a structural property of that NAT, not a timing problem. This is the case that forces a fallback to relaying.

---

## 6. TURN — relaying when punching fails

**TURN (Traversal Using Relays around NAT)** is the fallback: instead of trying to get two NAT'd devices talking directly, both devices connect *outbound* to a public relay server (which works fine through any NAT, since it's an outbound connection), and all traffic gets forwarded through that server. This always works, because it doesn't depend on either NAT's behavior at all — but it costs the relay operator real bandwidth for every byte of every session, and adds latency versus a direct path. This is why it's always the last resort, tried only after direct and hole-punched connections have been ruled out.

This maps directly onto what the V720 protocol calls "Forward" mode — its own TURN-equivalent, relaying everything through naxclow's server when direct/NAT modes don't pan out.

---

## 7. ICE — trying everything in order

**ICE (Interactive Connectivity Establishment)** is the overarching algorithm, used by WebRTC and similar systems, that ties all of the above together: gather every possible way to reach a device (its raw local address, its STUN-discovered reflexive address, a TURN relay address as a guaranteed-fallback), share the full list with the peer via the signaling channel, then systematically try each pairing in priority order (direct/local first, STUN-punched next, TURN relay last) until one works. This is why WebRTC configuration always asks for both STUN and TURN server URLs — STUN for the cheap/fast case, TURN as the guaranteed-to-work-but-expensive fallback.

The V720 app bundles real STUN/TURN server addresses and credentials (found in `IHttpService.java`) and has SDP-shaped method names (`reqSdpWebRtc`) — but the actual protocol we traced turned out to implement this same direct→NAT→forward ladder with its own lightweight custom messages rather than full ICE/WebRTC. The STUN/TURN infrastructure appears to be there for a separate, less-used code path. Same underlying concept, different (and much simpler) wire format.

---

## 8. Keepalives — why the hole needs to stay open

A NAT's port mapping isn't permanent — routers time out idle mappings after anywhere from a few seconds to a few minutes, to avoid keeping stale state forever. If no traffic flows in either direction for too long, the hole closes, and you'd need to punch it again. This is why every P2P protocol that stays connected for a while — video calls, and the V720 camera's live view — sends small periodic **keepalive** packets (a "ping," answered with a "pong") even when there's no real data to send, purely to keep the NAT mapping alive. This is exactly what the `101`/`102` pong packet types and the writer-idle-triggered ping frames in the V720 protocol are doing.

---

## 9. TCP vs UDP for traversal

Hole punching is dramatically easier and more reliable over **UDP** than TCP, for a structural reason: UDP is connectionless, so "sending a packet" doesn't require any prior handshake — the NAT just needs to see an outbound packet to open a mapping. TCP requires a full three-way handshake (SYN/SYN-ACK/ACK) to establish a connection, and getting two NATs to both process simultaneous inbound SYNs correctly ("TCP hole punching") is finicky and unreliable in practice, with much less consistent NAT vendor support. This is the practical reason almost every P2P system — WebRTC, this camera, and most others — uses UDP for the actual peer-to-peer media/data path, and reserves TCP for the simpler client-to-server signaling/coordination connections where a normal handshake to a known, publicly reachable server is no problem at all.

This matches exactly what we found in the V720 protocol: the coordination/control channel is TCP (talking to a known, publicly reachable server — no NAT problem there), while the actual live audio/video, which needs a punched hole between two NAT'd devices, is UDP.

---

## 10. How this maps onto the V720 protocol, end to end

Putting it all together, here's the V720 relay-mode handshake read through this lens:

1. **TCP to a known server** (the coordinator) — trivial, no NAT traversal needed, this is just a normal outbound TCP connection like visiting a website.
2. **Registration** — the client tells the coordinator who it is; the coordinator tells the client the camera's candidate addresses (its own NAT-observed address and a raw reported address) — this is the signaling/candidate-exchange step from section 4.
3. **Candidate probing over UDP** — the client sends packets toward those candidates and waits for a response, exactly the hole-punching mechanism from section 4.
4. **Mode determination** — whichever candidate actually responds determines whether the result is "Direct" (no NAT in the way at all), "NAT" (successfully hole-punched), or neither — triggering a fallback to "Forward" (relay through the coordinator, i.e. TURN-equivalent) if punching doesn't succeed.
5. **AP mode is the trivial case** — when your phone joins the camera's own hotspot, there's no NAT involved at all (you're both on the same tiny network the camera created), so none of this machinery is needed — which is exactly why AP-mode's handshake is so much simpler than relay-mode's.
6. **Keepalive pings** keep whichever path was established alive for the duration of the session.

Understanding this ladder is what makes the relay-mode protocol's structure make sense as a coherent design rather than a pile of arbitrary message codes — every one of those `P2P_MODE_*` constants and every candidate-probing message exists to solve one specific, well-known piece of this problem.
