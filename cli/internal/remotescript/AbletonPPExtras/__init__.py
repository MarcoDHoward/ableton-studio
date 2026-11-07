# AbletonPPExtras - a small, locked-down Live Remote Script for ableton-pp-cli.
#
# What it does: listens on 127.0.0.1:9878 (loopback only), accepts ONE JSON
# command per TCP connection from a fixed allowlist, runs it on Live's main
# thread, and replies with ONE JSON object followed by "\n".
#
# What it deliberately does NOT do: no eval/exec/code compilation, no file
# access, no child processes, no outbound network, no listeners on Live
# objects (it never watches your edits), and it never logs request contents
# (only the command type, and only on failure).
#
# Written from scratch for ableton-pp-cli. Python stdlib + Live's _Framework.

import json
import math
import queue
import socket
import threading
import time

from _Framework.ControlSurface import ControlSurface

SCRIPT_NAME = "AbletonPPExtras"
SCRIPT_VERSION = "1.0.2"
PROTOCOL_VERSION = 1

HOST = "127.0.0.1"  # loopback only; intentionally not configurable
PORT = 9878
MAX_REQUEST_BYTES = 1024 * 1024
READ_TIMEOUT_SECONDS = 5.0
MAIN_THREAD_TIMEOUT_SECONDS = 10.0
MAX_CLIENTS = 4
MAX_TASKS_PER_TICK = 16

MAX_INDEX = 1000000
MAX_BEATS = 1000000.0
MAX_POINTS = 10000
MAX_SAMPLES = 4096
MIN_DB = -70.0
MAX_DB = 6.0
SEARCH_ITERATIONS = 30

COMMANDS = (
    "ping",
    "version",
    "delete_device",
    "rack_chains",
    "drum_pads",
    "arrangement_add_clip",
    "arrangement_delete_clip",
    "automation_get",
    "automation_set",
    "automation_clear",
    "mixer_db",
    "set_volume_db",
)

NOTE_NAMES = ("C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B")


class CommandError(Exception):
    """An error reported to the client as {"status":"error","code":...}."""

    def __init__(self, code, message):
        Exception.__init__(self, message)
        self.code = code
        self.message = message


def _bad(message):
    return CommandError("bad_request", message)


# ---------------------------------------------------------------------------
# Request parsing and validation (pure Python; no Live objects touched).
# ---------------------------------------------------------------------------

def _reject_constant(name):
    raise ValueError("non-finite number %s is not allowed" % name)


def parse_request(raw):
    """Decode bytes into (command_type, params dict). Raises CommandError."""
    if len(raw) > MAX_REQUEST_BYTES:
        raise _bad("request exceeds %d bytes" % MAX_REQUEST_BYTES)
    try:
        text = raw.decode("utf-8")
    except UnicodeDecodeError:
        raise _bad("request is not valid UTF-8")
    try:
        obj = json.loads(text, parse_constant=_reject_constant)
    except (ValueError, RecursionError):
        raise _bad("request is not a valid JSON object")
    if not isinstance(obj, dict):
        raise _bad("request must be a JSON object")
    extra = set(obj.keys()) - {"type", "params"}
    if extra:
        raise _bad("unexpected top-level field(s): %s" % ", ".join(sorted(extra)))
    cmd = obj.get("type")
    if not isinstance(cmd, str) or cmd not in COMMANDS:
        raise _bad("unknown command type")
    params = obj.get("params", {})
    if params is None:
        params = {}
    if not isinstance(params, dict):
        raise _bad("params must be a JSON object")
    return cmd, params


def _is_int(value):
    return isinstance(value, int) and not isinstance(value, bool)


def _is_number(value):
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        return False
    return math.isfinite(float(value))


def check_index(params, name):
    if name not in params:
        raise _bad("missing required param %r" % name)
    value = params[name]
    if not _is_int(value):
        raise _bad("param %r must be an integer (0-based index)" % name)
    if value < 0 or value > MAX_INDEX:
        raise _bad("param %r must be between 0 and %d" % (name, MAX_INDEX))
    return value


def check_number(params, name, low=None, high=None, low_exclusive=False, default=None):
    if name not in params:
        if default is not None:
            return float(default)
        raise _bad("missing required param %r" % name)
    value = params[name]
    if not _is_number(value):
        raise _bad("param %r must be a finite number" % name)
    value = float(value)
    if low is not None and (value < low or (low_exclusive and value == low)):
        raise _bad("param %r must be %s %s" % (name, ">" if low_exclusive else ">=", low))
    if high is not None and value > high:
        raise _bad("param %r must be <= %s" % (name, high))
    return value


def check_points(params):
    points = params.get("points")
    if not isinstance(points, list) or not points:
        raise _bad("param 'points' must be a non-empty list")
    if len(points) > MAX_POINTS:
        raise _bad("param 'points' may hold at most %d items" % MAX_POINTS)
    clean = []
    for i, point in enumerate(points):
        if not isinstance(point, dict):
            raise _bad("points[%d] must be an object" % i)
        extra = set(point.keys()) - {"time", "value", "duration"}
        if extra:
            raise _bad("points[%d] has unexpected field(s): %s" % (i, ", ".join(sorted(extra))))
        clean.append({
            "time": check_number(point, "time", low=0.0, high=MAX_BEATS),
            "value": check_number(point, "value"),
            # None = hold until the next point (or the clip end); see automation_set.
            "duration": (check_number(point, "duration", low=0.0, high=MAX_BEATS, low_exclusive=True)
                         if "duration" in point else None),
        })
    return clean


_TRACK_DEVICE = ("track", "device")
_CLIP_PARAM = ("track", "slot", "device", "param")

# Allowed param names per command (anything else is rejected).
ALLOWED_PARAMS = {
    "ping": (),
    "version": (),
    "delete_device": _TRACK_DEVICE,
    "rack_chains": _TRACK_DEVICE,
    "drum_pads": _TRACK_DEVICE,
    "arrangement_add_clip": ("track", "start", "length"),
    "arrangement_delete_clip": ("track", "clip"),
    "automation_get": _CLIP_PARAM + ("step",),
    "automation_set": _CLIP_PARAM + ("points",),
    "automation_clear": _CLIP_PARAM,
    "mixer_db": (),
    "set_volume_db": ("track", "db"),
}


def validate_params(cmd, params):
    """Type/range-check params for cmd and return a normalized dict."""
    allowed = ALLOWED_PARAMS[cmd]
    extra = set(params.keys()) - set(allowed)
    if extra:
        raise _bad("unexpected param(s) for %s: %s" % (cmd, ", ".join(sorted(extra))))
    out = {}
    for name in allowed:
        if name in ("track", "device", "slot", "param", "clip"):
            out[name] = check_index(params, name)
    if cmd == "arrangement_add_clip":
        out["start"] = check_number(params, "start", low=0.0, high=MAX_BEATS)
        out["length"] = check_number(params, "length", low=0.0, high=MAX_BEATS, low_exclusive=True)
    elif cmd == "automation_get":
        out["step"] = check_number(params, "step", low=0.0, high=MAX_BEATS, low_exclusive=True, default=1.0)
    elif cmd == "automation_set":
        out["points"] = check_points(params)
    elif cmd == "set_volume_db":
        out["db"] = check_number(params, "db")
    return out


def encode_reply(obj):
    """Serialize a reply; non-finite floats are not allowed on the wire."""
    return (json.dumps(obj, separators=(",", ":"), allow_nan=False) + "\n").encode("utf-8")


def success(result):
    return {"status": "success", "result": result}


def failure(code, message):
    return {"status": "error", "message": message, "code": code}


# ---------------------------------------------------------------------------
# Small helpers for Live objects (called on the main thread only).
# ---------------------------------------------------------------------------

def _finite(value):
    try:
        value = float(value)
    except (TypeError, ValueError):
        return None
    return value if math.isfinite(value) else None


def _name(obj):
    try:
        return str(obj.name)
    except Exception:
        return ""


def _pick(seq, index, what, container):
    items = list(seq)
    if index >= len(items):
        raise CommandError("not_found", "%s %d does not exist (%s has %d)" % (what, index, container, len(items)))
    return items[index]


def _require(obj, attr, what):
    if not hasattr(obj, attr):
        raise CommandError("unsupported", "%s is not available in this Live version (%s)" % (what, attr))


def note_name(note):
    # Live labels MIDI note 36 as C1 (middle C, 60, is C3).
    return "%s%d" % (NOTE_NAMES[note % 12], note // 12 - 2)


def parse_db(text):
    """Parse Live's volume text ("-6.0 dB", "-inf dB", "0 dB") into a float."""
    token = str(text).strip().split(" ")[0].strip()
    if token.lower() in ("-inf", "-\u221e"):
        return float("-inf")
    try:
        value = float(token)
    except ValueError:
        return None
    return value if not math.isnan(value) else None


def _value_text(param, value):
    try:
        return str(param.str_for_value(value))
    except Exception:
        return ""


# ---------------------------------------------------------------------------
# Command implementations. Each takes (song, params) and runs on the main
# thread. `song` is Live's Song object; `params` is already validated.
# ---------------------------------------------------------------------------

class Commands(object):

    def __init__(self, live_version):
        self._live_version = live_version

    def version(self, song, p):
        return {
            "name": SCRIPT_NAME,
            "script_version": SCRIPT_VERSION,
            "protocol_version": PROTOCOL_VERSION,
            "live_version": self._live_version(),
            "commands": list(COMMANDS),
        }

    def _track(self, song, index):
        return _pick(song.tracks, index, "track", "song")

    def _device(self, track, index):
        return _pick(track.devices, index, "device", "track %r" % _name(track))

    def delete_device(self, song, p):
        track = self._track(song, p["track"])
        device = self._device(track, p["device"])
        _require(track, "delete_device", "Track.delete_device")
        name = _name(device)
        track.delete_device(p["device"])
        return {"deleted": name, "track": _name(track)}

    def rack_chains(self, song, p):
        track = self._track(song, p["track"])
        device = self._device(track, p["device"])
        if not getattr(device, "can_have_chains", False):
            raise _bad("device %r is not a rack" % _name(device))
        out = []
        for i, chain in enumerate(device.chains):
            mixer = getattr(chain, "mixer_device", None)
            vol = getattr(mixer, "volume", None)
            pan = getattr(mixer, "panning", None)
            out.append({
                "index": i,
                "name": _name(chain),
                "volume_db": _value_text(vol, vol.value) if vol is not None else "",
                "volume": _finite(vol.value) if vol is not None else None,
                "pan": _finite(pan.value) if pan is not None else None,
                "mute": bool(getattr(chain, "mute", False)),
                "solo": bool(getattr(chain, "solo", False)),
                "devices": [_name(d) for d in getattr(chain, "devices", [])],
            })
        return out

    def drum_pads(self, song, p):
        track = self._track(song, p["track"])
        device = self._device(track, p["device"])
        if not getattr(device, "can_have_drum_pads", False):
            raise _bad("device %r is not a Drum Rack" % _name(device))
        out = []
        for pad in device.drum_pads:
            chains = len(list(pad.chains))
            if chains == 0:
                continue
            note = int(pad.note)
            out.append({
                "note": note,
                "note_name": note_name(note),
                "name": _name(pad),
                "mute": bool(pad.mute),
                "solo": bool(pad.solo),
                "chains": chains,
            })
        return out

    def arrangement_add_clip(self, song, p):
        track = self._track(song, p["track"])
        if not getattr(track, "has_midi_input", False):
            raise _bad("track %r is not a MIDI track" % _name(track))
        _require(track, "create_midi_clip", "Track.create_midi_clip (Live 12+)")
        _require(track, "arrangement_clips", "Track.arrangement_clips")
        created = track.create_midi_clip(p["start"], p["length"])
        clips = list(track.arrangement_clips)
        index = None
        for i, clip in enumerate(clips):
            if created is not None and clip == created:
                index = i
                break
        if index is None:  # fall back to matching the start position
            for i, clip in enumerate(clips):
                if abs(float(clip.start_time) - p["start"]) < 1e-6:
                    index = i
                    break
        if index is None:
            raise CommandError("internal", "clip was created but could not be located")
        clip = clips[index]
        return {"index": index, "name": _name(clip),
                "start": _finite(clip.start_time), "end": _finite(clip.end_time)}

    def arrangement_delete_clip(self, song, p):
        track = self._track(song, p["track"])
        _require(track, "arrangement_clips", "Track.arrangement_clips")
        _require(track, "delete_clip", "Track.delete_clip")
        clip = _pick(track.arrangement_clips, p["clip"], "arrangement clip", "track %r" % _name(track))
        name, start = _name(clip), _finite(clip.start_time)
        track.delete_clip(clip)
        return {"deleted": name, "start": start}

    def _clip_and_param(self, song, p):
        track = self._track(song, p["track"])
        slot = _pick(track.clip_slots, p["slot"], "clip slot", "track %r" % _name(track))
        if not slot.has_clip:
            raise CommandError("not_found", "clip slot %d on track %r is empty" % (p["slot"], _name(track)))
        device = self._device(track, p["device"])
        param = _pick(device.parameters, p["param"], "parameter", "device %r" % _name(device))
        return slot.clip, param

    def automation_get(self, song, p):
        clip, param = self._clip_and_param(song, p)
        _require(clip, "automation_envelope", "Clip.automation_envelope")
        envelope = clip.automation_envelope(param)
        points = []
        if envelope is not None:
            length = float(clip.length)
            count = int(math.floor(length / p["step"])) + 1
            if count > MAX_SAMPLES:
                raise _bad("step too small: would return more than %d samples" % MAX_SAMPLES)
            for i in range(count):
                t = i * p["step"]
                points.append({"time": t, "value": _finite(envelope.value_at_time(t))})
        return {"parameter": _name(param), "has_envelope": envelope is not None, "points": points}

    def automation_set(self, song, p):
        clip, param = self._clip_and_param(song, p)
        _require(clip, "automation_envelope", "Clip.automation_envelope")
        _require(clip, "create_automation_envelope", "Clip.create_automation_envelope")
        low, high = float(param.min), float(param.max)
        for i, point in enumerate(p["points"]):
            if point["value"] < low or point["value"] > high:
                raise _bad("points[%d].value %s is outside %r range %s..%s"
                           % (i, point["value"], _name(param), low, high))
        # insert_step writes a flat step of the given length; a zero-length step
        # writes nothing, so every step needs a length before anything is changed.
        length = float(clip.length)
        steps = sorted(p["points"], key=lambda q: q["time"])
        planned = []
        for i, point in enumerate(steps):
            dur = point["duration"]
            if dur is None:
                end = steps[i + 1]["time"] if i + 1 < len(steps) else length
                dur = end - point["time"]
            if dur <= 0:
                raise _bad("the point at beat %s has no length: it is at or after the next point "
                           "or the clip end (%s beats); give it a duration or move it" % (point["time"], length))
            planned.append((point["time"], dur, point["value"]))
        envelope = clip.automation_envelope(param)
        if envelope is None:
            envelope = clip.create_automation_envelope(param)
        if envelope is None:
            raise CommandError("unsupported", "Live cannot automate %r in this clip" % _name(param))
        for start, dur, value in planned:
            envelope.insert_step(start, dur, value)
        return {"written": len(planned)}

    def automation_clear(self, song, p):
        clip, param = self._clip_and_param(song, p)
        _require(clip, "automation_envelope", "Clip.automation_envelope")
        _require(clip, "clear_envelope", "Clip.clear_envelope")
        if clip.automation_envelope(param) is None:
            return {"cleared": False}
        clip.clear_envelope(param)
        return {"cleared": True}

    def mixer_db(self, song, p):
        out = []
        for i, track in enumerate(song.tracks):
            vol = track.mixer_device.volume
            out.append({"track": i, "name": _name(track),
                        "volume": _finite(vol.value), "db": _value_text(vol, vol.value)})
        return out

    def set_volume_db(self, song, p):
        track = self._track(song, p["track"])
        vol = track.mixer_device.volume
        target = min(MAX_DB, max(MIN_DB, p["db"]))
        before = _value_text(vol, vol.value)

        def db_at(value):
            db = parse_db(_value_text(vol, value))
            if db is None:
                raise CommandError("unsupported", "cannot read dB text from the volume parameter")
            return db

        low, high = float(vol.min), float(vol.max)
        if target <= db_at(low):
            best = low
        elif target >= db_at(high):
            best = high
        else:
            for _ in range(SEARCH_ITERATIONS):
                mid = (low + high) / 2.0
                if db_at(mid) < target:
                    low = mid
                else:
                    high = mid
            best = low if abs(db_at(low) - target) <= abs(db_at(high) - target) else high
        vol.value = best
        return {"track": p["track"], "name": _name(track), "before_db": before,
                "after_db": _value_text(vol, vol.value), "value": _finite(vol.value)}


def dispatch(raw, run_on_main, commands, get_song):
    """Turn one raw request into (command type or None, reply dict).

    run_on_main(fn) runs fn() on Live's main thread and returns its value;
    it raises CommandError on timeout.
    """
    cmd = None
    try:
        cmd, params = parse_request(raw)
        p = validate_params(cmd, params)
        if cmd == "ping":
            return cmd, success({"ok": True})
        handler = getattr(commands, cmd)
        return cmd, success(run_on_main(lambda: handler(get_song(), p)))
    except CommandError as err:
        return cmd, failure(err.code, err.message)
    except Exception as err:  # Live raised; report the class and text only
        return cmd, failure("internal", "Live rejected the operation: %s: %s"
                            % (type(err).__name__, str(err)[:200]))


# ---------------------------------------------------------------------------
# Main-thread task queue and loopback socket server.
# ---------------------------------------------------------------------------

class _Task(object):

    def __init__(self, fn):
        self.fn = fn
        self.event = threading.Event()
        self.lock = threading.Lock()
        self.cancelled = False
        self.started = False
        self.result = None
        self.error = None


class MainThreadQueue(object):
    """Socket threads submit callables; Live's main thread drains them."""

    def __init__(self):
        self._queue = queue.Queue()
        self._closed = False

    def run(self, fn, timeout=MAIN_THREAD_TIMEOUT_SECONDS):
        if self._closed:
            raise CommandError("internal", "AbletonPPExtras is shutting down")
        task = _Task(fn)
        self._queue.put(task)
        if not task.event.wait(timeout):
            with task.lock:
                if not task.started:
                    task.cancelled = True  # never run it after we gave up
            if not task.event.is_set():
                raise CommandError("internal", "Live did not respond in time")
        if task.error is not None:
            raise task.error
        return task.result

    def drain(self, limit=MAX_TASKS_PER_TICK):
        """Called on Live's main thread."""
        for _ in range(limit):
            try:
                task = self._queue.get_nowait()
            except queue.Empty:
                return
            with task.lock:
                if task.cancelled:
                    continue
                task.started = True
            try:
                task.result = task.fn()
            except Exception as err:
                task.error = err
            task.event.set()

    def close(self):
        self._closed = True
        while True:
            try:
                task = self._queue.get_nowait()
            except queue.Empty:
                return
            task.error = CommandError("internal", "AbletonPPExtras is shutting down")
            task.event.set()


# A client without a newline or half-close gets this many early parse
# attempts; after that the request ends only at "\n" or EOF. This bounds the
# work a slow, trickling client can force onto Live's Python.
MAX_EARLY_PARSES = 4


def read_request(conn):
    """Read one JSON object from conn (client may or may not half-close).

    The whole request must arrive within READ_TIMEOUT_SECONDS of the first
    read, not per recv, so a trickling client cannot hold a slot forever.
    """
    deadline = time.time() + READ_TIMEOUT_SECONDS
    buf = bytearray()
    early_parses = 0
    while True:
        remaining = deadline - time.time()
        if remaining <= 0:
            raise _bad("timed out reading the request")
        conn.settimeout(remaining)
        try:
            chunk = conn.recv(65536)
        except socket.timeout:
            raise _bad("timed out reading the request")
        if not chunk:
            return bytes(buf)
        buf.extend(chunk)
        if len(buf) > MAX_REQUEST_BYTES:
            raise _bad("request exceeds %d bytes" % MAX_REQUEST_BYTES)
        if chunk.endswith(b"\n"):
            return bytes(buf)
        if early_parses < MAX_EARLY_PARSES and bytes(buf).rstrip().endswith(b"}"):
            early_parses += 1
            try:
                json.loads(bytes(buf).decode("utf-8"), parse_constant=_reject_constant)
                return bytes(buf)
            except (ValueError, RecursionError):
                continue  # incomplete; keep reading


class LoopbackServer(object):

    def __init__(self, handle_raw, log):
        self._handle_raw = handle_raw
        self._log = log
        self._stop = threading.Event()
        self._slots = threading.BoundedSemaphore(MAX_CLIENTS)
        self._clients = []
        self._clients_lock = threading.Lock()
        self._sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        if hasattr(socket, "SO_EXCLUSIVEADDRUSE"):
            # Windows: SO_REUSEADDR would let another process bind the same
            # port and intercept traffic, so ask for exclusive use instead.
            self._sock.setsockopt(socket.SOL_SOCKET, socket.SO_EXCLUSIVEADDRUSE, 1)
        else:
            self._sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        try:
            self._sock.bind((HOST, PORT))
            self._sock.listen(8)
        except OSError:
            self._sock.close()
            raise
        self._sock.settimeout(0.5)
        self._thread = threading.Thread(target=self._accept_loop, name="AbletonPPExtras-accept")
        self._thread.daemon = True

    def start(self):
        self._thread.start()

    def _accept_loop(self):
        while not self._stop.is_set():
            try:
                conn, _addr = self._sock.accept()
            except socket.timeout:
                continue
            except OSError:
                break
            if not self._slots.acquire(blocking=False):
                self._reply_and_close(conn, failure("internal", "too many concurrent connections"))
                continue
            worker = threading.Thread(target=self._serve, args=(conn,), name="AbletonPPExtras-client")
            worker.daemon = True
            with self._clients_lock:
                self._clients = [t for t in self._clients if t.is_alive()]
                self._clients.append(worker)
            worker.start()

    def _serve(self, conn):
        try:
            try:
                raw = read_request(conn)
                reply = self._handle_raw(raw)
            except CommandError as err:
                reply = failure(err.code, err.message)
            except Exception as err:
                self._log("connection error: %s" % type(err).__name__)
                reply = failure("internal", "internal error")
            self._reply_and_close(conn, reply)
        finally:
            self._slots.release()

    def _reply_and_close(self, conn, reply):
        try:
            try:
                data = encode_reply(reply)
            except (TypeError, ValueError):
                data = encode_reply(failure("internal", "result could not be encoded"))
            conn.settimeout(READ_TIMEOUT_SECONDS)
            conn.sendall(data)
        except OSError:
            pass
        finally:
            try:
                conn.close()
            except OSError:
                pass

    def stop(self):
        self._stop.set()
        try:
            self._sock.close()
        except OSError:
            pass
        self._thread.join(2.0)
        with self._clients_lock:
            clients = list(self._clients)
        for worker in clients:
            worker.join(1.0)


# ---------------------------------------------------------------------------
# The ControlSurface Live loads.
# ---------------------------------------------------------------------------

def _live_version():
    try:
        import Live  # Live's own API module, only present inside Live
        app = Live.Application.get_application()
        return "%d.%d.%d" % (app.get_major_version(), app.get_minor_version(), app.get_bugfix_version())
    except Exception:
        return "unknown"


class AbletonPPExtras(ControlSurface):
    # Attributes are prefixed _pp_ because ControlSurface already owns names like _tasks (a read-only property).

    def __init__(self, c_instance):
        ControlSurface.__init__(self, c_instance)
        self._pp_queue = MainThreadQueue()
        self._pp_commands = Commands(_live_version)
        self._pp_server = None
        try:
            self._pp_server = LoopbackServer(self._pp_handle_raw, self._pp_log)
            self._pp_server.start()
            self.show_message("%s: listening on %s:%d" % (SCRIPT_NAME, HOST, PORT))
        except OSError as err:
            self._pp_server = None
            self.show_message("%s: could not listen on %s:%d (%s)" % (SCRIPT_NAME, HOST, PORT, type(err).__name__))

    def _pp_log(self, text):
        self.log_message("%s: %s" % (SCRIPT_NAME, text))

    def _pp_handle_raw(self, raw):
        cmd, reply = dispatch(raw, self._pp_queue.run, self._pp_commands, self.song)
        if reply.get("code") == "internal":
            # Only the command type is ever logged: never params or names.
            self._pp_log("command %s failed with an internal error" % (cmd or "?"))
        return reply

    def update_display(self):
        # Called by Live on its main thread roughly every 100 ms.
        ControlSurface.update_display(self)
        self._pp_queue.drain()

    def disconnect(self):
        self._pp_queue.close()
        if self._pp_server is not None:
            self._pp_server.stop()
            self._pp_server = None
        ControlSurface.disconnect(self)


def create_instance(c_instance):
    return AbletonPPExtras(c_instance)
