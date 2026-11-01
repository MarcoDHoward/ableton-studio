"""Scratch-set guard for writes to Ableton Live.

Rule: writes to Live are free only when the open set's name starts with SCRATCH.
Anywhere else the user is asked first.

Used two ways:
  * as a Claude Code PreToolUse hook:  python harness/liveguard.py hook  (reads the hook JSON on stdin)
  * as a library: harness/loop.py calls require_scratch() before it writes.

Classification fails closed: a CLI command not known to be read-only counts as a write.
"""
import ctypes
import json
import os
import re
import sys
from ctypes import wintypes

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
MANIFEST = os.path.join(ROOT, 'cli', 'tools-manifest.json')

# Hand-written CLI commands that only read Live (they may write the CLI's local store).
NOVEL_READS = {
    'journal list', 'mix list', 'mix save', 'snapshot', 'snapshot save', 'snapshot list', 'snapshot diff',
    'chain list', 'chain save', 'notes backups', 'watch', 'doctor', 'scripts status', 'search', 'sync',
    'version', 'help', 'completion', 'health', 'status', 'agent-context', 'api', 'which',
}
# Commands that read with no positional value and write with one (e.g. `tempo` vs `tempo 122`).
READ_WITHOUT_VALUE = {'tempo'}
# Always ask, whatever set is open: these change files outside the set.
ALWAYS_ASK = {'scripts install'}
# harness/loop.py subcommands that write to Live.
LOOP_WRITES = {'write', 'listen', 'iterate', 'export'}


class Commands:
    """Read-only command keys, every known command key, and promoted read verbs (tracks_list -> `tracks`)."""
    def __init__(self, reads, known, promoted):
        self.reads, self.known, self.promoted = reads, known, promoted

    def __contains__(self, key):  # lets tests pass a plain set too
        return key in self.reads


def read_only_commands():
    reads, known, promoted = set(NOVEL_READS), set(), set()
    try:
        with open(MANIFEST, encoding='utf-8') as fh:
            for tool in json.load(fh).get('tools', []):
                key = tool['name'].replace('_', ' ')
                known.add(key)
                if tool.get('method', '').upper() == 'GET':
                    reads.add(key)
                    head, _, verb = key.partition(' ')
                    if verb in ('list', 'get', 'check'):
                        promoted.add(head)
    except (OSError, ValueError):
        pass  # without the manifest every unknown command is treated as a write
    return Commands(reads, known, promoted)


def classify(args, reads=None):
    """Return 'read', 'write' or 'ask' for one ableton-pp-cli argument list."""
    reads = read_only_commands() if reads is None else reads
    if not isinstance(reads, Commands):
        reads = Commands(set(reads), set(), set())
    if any(a in ('-h', '--help', '--dry-run', '--version') for a in args):
        return 'read'
    words = []
    for a in args:
        if a.startswith('-'):
            break
        words.append(a)
    if not words:
        return 'read'
    for n in (3, 2, 1):
        key = ' '.join(words[:n])
        if key in ALWAYS_ASK:
            return 'ask'
        if key in READ_WITHOUT_VALUE:
            return 'write' if len(words) > n else 'read'
        if key in reads.reads:
            return 'read'
    # promoted read verb used completely bare (`tracks`, `status`); anything after it could be a
    # write subcommand the manifest doesn't list (hand-written commands aren't in it), so fail closed
    if words[0] in reads.promoted and len(words) == 1:
        return 'read'
    return 'write'


CLI_RE = re.compile(r'ableton-pp-cli(?:\.exe)?["\']?((?:[^|;&\n]|&&)*)', re.I)
LOOP_RE = re.compile(r'loop\.py["\']?\s+([\w-]+)', re.I)


def split_args(s):
    s = s.split('&&')[0]
    return [t.strip('"\'') for t in re.findall(r'"[^"]*"|\'[^\']*\'|\S+', s)]


def command_actions(command):
    """Yield (kind, description) for every Live-touching invocation inside a shell command."""
    reads = read_only_commands()
    for m in CLI_RE.finditer(command):
        args = split_args(m.group(1))
        yield classify(args, reads), 'ableton-pp-cli ' + ' '.join(args[:6])
    for m in LOOP_RE.finditer(command):
        if m.group(1) in LOOP_WRITES:
            yield 'write', 'loop.py ' + m.group(1)


# ---- Live's window title -------------------------------------------------------------

def live_window_title():
    """Title of Ableton Live's main window, e.g. 'ProgAcidAI* - Ableton Live 12 Suite'; None if not running."""
    if os.name != 'nt':
        return None
    user32, kernel32 = ctypes.windll.user32, ctypes.windll.kernel32
    found = []
    PROC = ctypes.WINFUNCTYPE(wintypes.BOOL, wintypes.HWND, wintypes.LPARAM)

    def cb(hwnd, _):
        if not user32.IsWindowVisible(hwnd):
            return True
        n = user32.GetWindowTextLengthW(hwnd)
        if n == 0:
            return True
        buf = ctypes.create_unicode_buffer(n + 1)
        user32.GetWindowTextW(hwnd, buf, n + 1)
        title = buf.value
        if 'Ableton Live' not in title:
            return True
        pid = wintypes.DWORD()
        user32.GetWindowThreadProcessId(hwnd, ctypes.byref(pid))
        h = kernel32.OpenProcess(0x1000, False, pid.value)  # PROCESS_QUERY_LIMITED_INFORMATION
        if h:
            path = ctypes.create_unicode_buffer(1024)
            size = wintypes.DWORD(1024)
            if kernel32.QueryFullProcessImageNameW(h, 0, path, ctypes.byref(size)):
                if os.path.basename(path.value).lower().startswith('ableton live'):
                    found.append(title)
            kernel32.CloseHandle(h)
        return True

    user32.EnumWindows(PROC(cb), 0)
    return found[0] if found else None


def set_name(title):
    """'ProgAcidAI* - Ableton Live 12 Suite' -> 'ProgAcidAI'; a bare 'Ableton Live 12 Suite' -> 'Untitled'."""
    if not title:
        return None
    m = re.match(r'^(.*?)\*?\s+-\s+Ableton Live', title)
    return m.group(1).strip() if m else 'Untitled'


def is_scratch(name):
    return bool(name) and name.upper().startswith('SCRATCH')


class NotScratch(RuntimeError):
    pass


def require_scratch(action='write to Live'):
    """For harness code: raise unless the open set is a scratch set (or Live isn't running)."""
    title = live_window_title()
    if title is None:
        return None
    name = set_name(title)
    if not is_scratch(name):
        raise NotScratch('Live set "%s" is not a SCRATCH set; refusing to %s without the user\'s OK.' % (name, action))
    return name


# ---- hook entry point ----------------------------------------------------------------

def hook():
    try:
        payload = json.load(sys.stdin)
    except ValueError:
        return 0
    command = (payload.get('tool_input') or {}).get('command') or ''
    if 'ableton-pp-cli' not in command.lower() and 'loop.py' not in command.lower():
        return 0
    actions = list(command_actions(command))
    always = [d for k, d in actions if k == 'ask']
    writes = [d for k, d in actions if k == 'write']
    if not always and not writes:
        return 0
    reason = None
    if always:
        reason = 'This changes files outside the Live set (%s). It needs your OK.' % '; '.join(always)
    else:
        title = live_window_title()
        if title is None:
            return 0  # Live isn't running; the bridges will just fail
        name = set_name(title)
        if is_scratch(name):
            return 0
        reason = ('The open Live set is "%s", not a SCRATCH set. This will change it: %s. '
                  'Take a snapshot first (ableton-pp-cli snapshot save).' % (name, '; '.join(writes)))
    json.dump({'hookSpecificOutput': {'hookEventName': 'PreToolUse', 'permissionDecision': 'ask',
                                      'permissionDecisionReason': reason}}, sys.stdout)
    return 0


if __name__ == '__main__':
    if len(sys.argv) > 1 and sys.argv[1] == 'hook':
        sys.exit(hook())
    if len(sys.argv) > 1 and sys.argv[1] == 'status':
        t = live_window_title()
        print(json.dumps({'title': t, 'set': set_name(t), 'scratch': is_scratch(set_name(t))}))
        sys.exit(0)
    print(__doc__)
