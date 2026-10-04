#!/usr/bin/env python3
"""Live test of the call stream (GET /call/stream/{callId}): audio, and optionally video.

Needs `pip install websockets`. The instance must have callsEnabled turned on and
must have been (re)connected after that.

Incoming (default): call the instance's number from another phone. The script waits
for the ringing call, opens the stream, answers, and

  * records what the caller says into a WAV file (16 kHz mono), and
  * echoes it back (--echo) and/or plays a 440 Hz tone (--tone SECONDS),

so you can hear both directions work. Ctrl+C hangs the call up.

Outgoing: --dial NUMBER places the call, connects the stream before the other phone
rings, and does the same once it is picked up.

Video: --video also carries the call's video. What the peer sends is written to
<name>.h264 (play it with `ffplay file.h264`), and <name>.orient has one line per picture:
index, seconds, keyframe, rotation in clockwise quarter turns to show it upright. --video-in FILE sends that H.264 file
(Annex-B, see below) to the peer at --fps; it starts over from its first keyframe
whenever WhatsApp asks for one. With --dial, --video places a video call; on an audio
call, --video-in asks the peer to upgrade to video once the call is up (an iPhone accepts
it and the call turns into video; just announcing "camera on" does not work, see the
"enable" video action). A peer's upgrade request is accepted automatically.

    python call-stream-test.py --apikey TOKEN --echo
    python call-stream-test.py --apikey TOKEN --dial 5511999990000 --tone 3
    python call-stream-test.py --apikey TOKEN --dial 5511999990000 --video --video-in test.h264

A test file (constrained baseline, one slice per picture, headers repeated on every
keyframe, which is what the stream expects). Phones show a call in portrait, so send
portrait pictures (360x640) to fill the screen; a landscape one (640x360) is letterboxed.
-pix_fmt yuv420p is needed: the baseline profile refuses the test source's 4:4:4.

    ffmpeg -f lavfi -i testsrc=size=360x640:rate=15 -t 60 -pix_fmt yuv420p -c:v libx264 \\
      -profile:v baseline -x264-params keyint=30:repeat-headers=1:slices=1:bframes=0 \\
      -f h264 test.h264
"""
import argparse
import asyncio
import base64
import json
import math
import re
import struct
import sys
import time
import urllib.error
import urllib.request
import wave

import websockets

SAMPLE_RATE = 16000
FRAME_MS = 60
FRAME_SAMPLES = SAMPLE_RATE * FRAME_MS // 1000

# --format: what the stream carries (the call itself always runs at 16 kHz)
FORMATS = {
    "pcm16": ("audio/pcm-s16le", 16000),
    "pcm8": ("audio/pcm-s16le", 8000),
    "pcm24": ("audio/pcm-s16le", 24000),
    "mulaw": ("audio/x-mulaw", 8000),
    "alaw": ("audio/x-alaw", 8000),
}
STREAM_ENCODING, STREAM_RATE = FORMATS["pcm16"]


def mulaw_encode(v):
    sign = 0x80 if v < 0 else 0
    v = min(abs(v), 32635) + 0x84
    exp = 7
    mask = 0x4000
    while not v & mask and exp > 0:
        exp -= 1
        mask >>= 1
    return ~(sign | exp << 4 | ((v >> (exp + 3)) & 0x0F)) & 0xFF


def mulaw_decode(u):
    u = ~u & 0xFF
    v = (((u & 0x0F) << 3) + 0x84) << ((u >> 4) & 7)
    v -= 0x84
    return -v if u & 0x80 else v


def alaw_encode(v):
    v >>= 3
    mask = 0xD5
    if v < 0:
        mask = 0x55
        v = -v - 1
    seg = next((i for i, end in enumerate((0x1F, 0x3F, 0x7F, 0xFF, 0x1FF, 0x3FF, 0x7FF, 0xFFF)) if v <= end), 8)
    if seg >= 8:
        return 0x7F ^ mask
    a = seg << 4 | ((v >> 1) & 0x0F if seg < 2 else (v >> seg) & 0x0F)
    return a ^ mask


def alaw_decode(a):
    a ^= 0x55
    t = (a & 0x0F) << 4
    seg = (a & 0x70) >> 4
    t = t + 8 if seg == 0 else (t + 0x108 if seg == 1 else (t + 0x108) << (seg - 1))
    return t if a & 0x80 else -t


async def send_audio(ws, data, binary):
    """Audio for the peer: a binary frame (0x01 | audio) or a JSON message."""
    if binary:
        await ws.send(b"\x01" + data)
    else:
        await ws.send(json.dumps({"event": "media", "payload": base64.b64encode(data).decode()}))


async def send_video_unit(ws, unit, binary):
    """One H.264 access unit for the peer: 0x02 | access unit, or JSON."""
    if binary:
        await ws.send(b"\x02" + unit)
    else:
        await ws.send(json.dumps({"event": "video", "payload": base64.b64encode(unit).decode()}))


def parse_binary(raw):
    """A binary frame from the server as the dict its JSON counterpart would be, with the
    bytes under "raw" (layout: see the protocol description in handler.go)."""
    if raw[0] == 0x01:
        return {"event": "media", "raw": raw[9:], "seq": int.from_bytes(raw[1:5], "big"), "timestamp": int.from_bytes(raw[5:9], "big")}
    if raw[0] == 0x02:
        return {"event": "video", "raw": raw[10:], "keyframe": bool(raw[1] & 1), "orientation": (raw[1] >> 1) & 3,
                "seq": int.from_bytes(raw[2:6], "big"), "timestamp": int.from_bytes(raw[6:10], "big")}
    return {"event": "unknown"}


def to_pcm16(data):
    """What the stream sent, as 16-bit PCM for the WAV file."""
    if STREAM_ENCODING == "audio/x-mulaw":
        return b"".join(struct.pack("<h", mulaw_decode(b)) for b in data)
    if STREAM_ENCODING == "audio/x-alaw":
        return b"".join(struct.pack("<h", alaw_decode(b)) for b in data)
    return data


def encode_samples(samples):
    """16-bit samples (ints) in the stream's encoding."""
    if STREAM_ENCODING == "audio/x-mulaw":
        return bytes(mulaw_encode(v) for v in samples)
    if STREAM_ENCODING == "audio/x-alaw":
        return bytes(alaw_encode(v) for v in samples)
    return b"".join(struct.pack("<h", v) for v in samples)


def api(base, apikey, method, path, body=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(base + path, data=data, method=method)
    req.add_header("apikey", apikey)
    req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            return resp.status, json.loads(resp.read() or b"{}")
    except urllib.error.HTTPError as e:
        try:
            return e.code, json.loads(e.read() or b"{}")
        except ValueError:
            return e.code, {}


def wait_for_ringing_call(base, apikey):
    print("Waiting for an incoming call... (call the instance's number now)")
    shown = False
    while True:
        status, body = api(base, apikey, "GET", "/call/active")
        if status != 200:
            sys.exit(f"GET /call/active failed: {status} {body}")
        if not body.get("enabled"):
            sys.exit(
                "The instance has no working call engine: "
                f"state={body.get('state')!r} error={body.get('error')!r}. "
                "Turn on callsEnabled and reconnect the instance."
            )
        for call in body.get("calls", []):
            if call["direction"] == "incoming" and call["phase"] == "ringing":
                return call
        if not shown:
            print("  (engine is active, nobody is calling yet)")
            shown = True
        time.sleep(1)


def tone_frames(seconds):
    total = int(seconds * STREAM_RATE)
    samples = [int(12000 * math.sin(2 * math.pi * 440 * i / STREAM_RATE)) for i in range(total)]
    step = STREAM_RATE * FRAME_MS // 1000
    return [encode_samples(samples[i : i + step]) for i in range(0, len(samples), step)]


START_CODE = re.compile(b"\x00\x00\x01")


def access_units(data):
    """Split an Annex-B H.264 file into access units, one per picture.

    A picture ends at its slice NAL (type 1 or 5); the SPS, PPS, SEI and delimiter NALs in
    front of it belong to it. Right for streams with one slice per picture.
    """
    starts = [m.start() for m in START_CODE.finditer(data)]
    units, pending = [], b""
    for i, at in enumerate(starts):
        end = starts[i + 1] if i + 1 < len(starts) else len(data)
        nal = data[at:end].rstrip(b"\x00")
        nal = b"\x00\x00\x00\x01" + nal[3:]
        pending += nal
        if nal[4] & 0x1F in (1, 5):
            units.append(pending)
            pending = b""
    return units


def is_keyframe(au):
    return any(m.end() < len(au) and au[m.end()] & 0x1F == 5 for m in START_CODE.finditer(au))


async def main(args):
    global STREAM_ENCODING, STREAM_RATE
    STREAM_ENCODING, STREAM_RATE = FORMATS[args.format]
    audio = {"encoding": STREAM_ENCODING, "sampleRate": STREAM_RATE, "binary": args.binary, "speechEvents": args.speech}
    outgoing = bool(args.dial)
    want_video = args.video or bool(args.video_in)
    if outgoing:
        status, call = api(
            args.base, args.apikey, "POST", "/call/dial",
            {"number": args.dial, "stream": True, "video": want_video, **audio},
        )
        if status != 200:
            sys.exit(f"dial failed: {status} {call}")
        ticket = call["streamTicket"]
        print(f"Calling {args.dial}... (call {call['callId']}, phase {call['phase']}, video {call['video']})")
    else:
        call = {"callId": args.call_id} if args.call_id else wait_for_ringing_call(args.base, args.apikey)
        print(f"Call {call['callId']} from {call.get('peer')} (video={call.get('video')})")
        status, ticket = api(
            args.base, args.apikey, "POST", "/call/stream-ticket",
            {"callId": call["callId"], "video": want_video, **audio},
        )
        if status != 200:
            sys.exit(f"stream-ticket failed: {status} {ticket}")
    call_id = call["callId"]
    ws_url = "ws" + args.base[4:] + ticket["path"]

    def control(action, **extra):
        status, body = api(args.base, args.apikey, "POST", "/call/video", {"callId": call_id, "action": action, **extra})
        print(f"video {action}: {status}" + ("" if status == 200 else f" {body}"))

    wav_path = args.record or f"call-{call_id}.wav"
    h264_path = wav_path.rsplit(".", 1)[0] + ".h264"
    received = video_units = keyframes = 0
    video_file = open(h264_path, "wb") if want_video else None
    orient_file = open(h264_path.rsplit(".", 1)[0] + ".orient", "w") if want_video else None
    t0 = time.time()
    last_frame_orient = None
    restart = asyncio.Event()

    async with websockets.connect(ws_url, max_size=4 << 20) as ws, _wav(wav_path) as wav:
        start = json.loads(await ws.recv())
        print("start:", {k: v for k, v in start.items() if k != "event"})

        # The stream is attached while the call still rings, so no audio is lost.
        if not outgoing:
            status, body = api(args.base, args.apikey, "POST", "/call/answer", {"callId": call_id})
            if status != 200:
                sys.exit(f"answer failed: {status} {body}")
            print("answered; phase:", body.get("phase"))
        else:
            print("stream open; waiting for the other side to pick up (Ctrl+C hangs up)")

        marks_sent = {}

        async def send_tone():
            for frame in tone_frames(args.tone):
                await send_audio(ws, frame, args.binary)
                if not args.tone_burst:
                    await asyncio.sleep(FRAME_MS / 1000)
            marks_sent["tone-end"] = time.time()
            await ws.send(json.dumps({"event": "mark", "name": "tone-end"}))
            print(f"[{time.time()-t0:6.1f}s] tone queued; mark tone-end sent")

        async def send_video(units):
            await asyncio.sleep(2)  # let the call come up
            if not start.get("video"):
                print("the call is audio only: asking the peer to upgrade to video")
                control("start")
                await asyncio.sleep(3)
            i, sent = 0, 0
            while True:
                if restart.is_set():
                    restart.clear()
                    i = 0
                    print("WhatsApp asked for a keyframe: starting the file over")
                await send_video_unit(ws, units[i], args.binary)
                sent += 1
                i = (i + 1) % len(units)
                await asyncio.sleep(1 / args.fps)

        tasks = []
        if args.tone > 0:
            tasks.append(asyncio.create_task(send_tone()))
        if args.video_in:
            units = access_units(open(args.video_in, "rb").read())
            if not units or not is_keyframe(units[0]):
                sys.exit(f"{args.video_in}: expected Annex-B H.264 starting with a keyframe (see --help)")
            print(f"sending {len(units)} pictures from {args.video_in} at {args.fps} fps")
            tasks.append(asyncio.create_task(send_video(units)))

        try:
            async for raw in ws:
                msg = parse_binary(raw) if isinstance(raw, bytes) else json.loads(raw)
                event = msg["event"]
                if event == "media":
                    data = msg["raw"] if "raw" in msg else base64.b64decode(msg["payload"])
                    pcm = to_pcm16(data)
                    # Audio is not steady (a quiet peer sends two or three frames a second):
                    # place each frame by its timestamp and fill the gap with silence, so
                    # the file is as long as the call.
                    if msg.get("timestamp") is not None:
                        gap = int(msg["timestamp"] * STREAM_RATE / 1000) - received
                        if gap > STREAM_RATE * FRAME_MS // 1000:
                            wav.writeframes(b"\x00\x00" * gap)
                            received += gap
                    wav.writeframes(pcm)
                    received += len(pcm) // 2
                    if args.echo:
                        await send_audio(ws, data, args.binary)
                elif event == "video":
                    video_file.write(msg["raw"] if "raw" in msg else base64.b64decode(msg["payload"]))
                    orient_file.write(f"{video_units} {time.time()-t0:.2f} {int(bool(msg.get('keyframe')))} {msg.get('orientation')}" + chr(10))
                    if msg.get("orientation") != last_frame_orient:
                        last_frame_orient = msg.get("orientation")
                        print(f"[{time.time()-t0:6.1f}s] frame #{video_units} rotation to show upright (clockwise turns) -> {last_frame_orient}")
                    video_units += 1
                    if msg.get("keyframe"):
                        keyframes += 1
                        if keyframes == 1:
                            print(f"first video keyframe (orientation {msg.get('orientation')})")
                elif event in ("speech_start", "speech_end"):
                    extra = f" (lasted {msg.get('durationMs')} ms)" if event == "speech_end" else ""
                    print(f"[{time.time()-t0:6.1f}s] {event} at stream time {msg.get('timestamp')} ms{extra}")
                elif event == "mark":
                    waited = time.time() - marks_sent.get(msg.get("name"), time.time())
                    print(f"[{time.time()-t0:6.1f}s] mark {msg.get('name')!r} came back {waited:.1f}s after it was sent"
                          + (f" (the tone is {args.tone:.1f}s long)" if args.tone_burst else ""))
                elif event == "keyframe_request":
                    restart.set()
                elif event == "video_state":
                    print(f"[{time.time()-t0:6.1f}s] peer video state:", {k: msg.get(k) for k in ("state", "stateCode", "active", "upgrade", "orientation")})
                    if msg.get("upgrade"):
                        if args.accept_delay:
                            # a person needs a few seconds to read the request and click: see if the phone waits
                            print(f"[{time.time()-t0:6.1f}s] upgrade requested; accepting in {args.accept_delay:g} s")
                            await asyncio.sleep(args.accept_delay)
                        control("accept")
                elif event == "stop":
                    print("call ended:", msg.get("reason"))
                    break
                elif event == "error":
                    print("stream error:", msg.get("code"), msg.get("message"))
        except asyncio.CancelledError:
            api(args.base, args.apikey, "POST", "/call/hangup", {"callId": call_id})
            print("hung up")
            raise
        finally:
            for t in tasks:
                t.cancel()
            if video_file:
                video_file.close()
    print(f"received {received / STREAM_RATE:.1f} s of audio -> {wav_path}")
    if want_video:
        print(f"received {video_units} video pictures ({keyframes} keyframes) -> {h264_path}")


class _wav:
    def __init__(self, path):
        self.path = path

    async def __aenter__(self):
        self.w = wave.open(self.path, "wb")
        self.w.setnchannels(1)
        self.w.setsampwidth(2)
        self.w.setframerate(STREAM_RATE)
        return self.w

    async def __aexit__(self, *exc):
        self.w.close()


if __name__ == "__main__":
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--base", default="http://localhost:4000", help="server address")
    p.add_argument("--apikey", required=True, help="the instance token")
    p.add_argument("--call-id", help="use this call instead of waiting for a ringing one")
    p.add_argument("--dial", metavar="NUMBER", help="place a call to NUMBER instead of waiting for one")
    p.add_argument("--record", help="WAV file for the caller's audio (default call-<id>.wav)")
    p.add_argument("--format", choices=sorted(FORMATS), default="pcm16", help="audio format of the stream: PCM at 16 (default), 8 or 24 kHz, or G.711 mu-law/A-law at 8 kHz; the WAV file keeps that rate")
    p.add_argument("--binary", action="store_true", help="carry audio and video as binary WebSocket frames instead of base64 JSON")
    p.add_argument("--speech", action="store_true", help="ask the stream to report when the peer begins and stops talking (speech_start / speech_end)")
    p.add_argument("--accept-delay", type=float, default=0, metavar="SECONDS", help="wait this long before accepting the peer's request to turn the call into video (what a person clicking in the manager takes)")
    p.add_argument("--echo", action="store_true", help="send the caller's audio back")
    p.add_argument("--tone", type=float, default=0, metavar="SECONDS", help="play a 440 Hz tone, then send a mark")
    p.add_argument("--tone-burst", action="store_true", help="queue the whole tone at once instead of in real time: the mark then comes back when the tone has been played (up to 30 s)")
    p.add_argument("--video", action="store_true", help="carry the call's video too (with --dial: place a video call)")
    p.add_argument("--video-in", metavar="FILE", help="send this Annex-B H.264 file as video (implies --video)")
    p.add_argument("--fps", type=float, default=15, help="pictures per second of --video-in")
    try:
        asyncio.run(main(p.parse_args()))
    except KeyboardInterrupt:
        pass
