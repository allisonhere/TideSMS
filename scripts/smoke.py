#!/usr/bin/env python3
"""Exercise the actual TUI through a PTY with isolated storage and fake CLIs.

Requires only Python's standard library and a built ./tidesms. Never sends SMS.
"""
import fcntl
import json
import os
from pathlib import Path
import pty
import select
import sqlite3
import struct
import subprocess
import tempfile
import termios
import time

BINARY = Path(__file__).resolve().parents[1] / "tidesms"


def check(condition, message):
    if not condition:
        raise AssertionError(message)


with tempfile.TemporaryDirectory(prefix="tidesms-smoke-") as tmp:
    root = Path(tmp)
    tools = root / "bin"
    tools.mkdir()
    cli = tools / "kdeconnect-cli"
    cli.write_text('''#!/usr/bin/env python3
import json, os, sys
from pathlib import Path
root = Path(os.environ["TIDESMS_SMOKE_ROOT"])
if "--list-devices" in sys.argv:
    print("- Test Phone: test_phone (paired and reachable)")
elif "--send-sms" in sys.argv:
    if (root / "fail").exists():
        print("simulated send failure", file=sys.stderr)
        sys.exit(1)
    with (root / "sent.jsonl").open("a") as f:
        f.write(json.dumps(sys.argv[1:]) + "\\n")
else:
    sys.exit(2)
''')
    cli.chmod(0o700)
    probe = tools / "busctl"
    probe.write_text("#!/bin/sh\nprintf 'b true\\n'\n")
    probe.chmod(0o700)
    env = dict(os.environ, TERM="xterm-256color", COLORTERM="truecolor",
               PATH=str(tools) + os.pathsep + os.environ["PATH"],
               TIDESMS_SMOKE_ROOT=tmp, XDG_CONFIG_HOME=tmp + "/config",
               XDG_DATA_HOME=tmp + "/data", XDG_STATE_HOME=tmp + "/state")
    config = root / "config/tidesms/config.toml"
    config.parent.mkdir(parents=True)
    # Exercise the documented multiline mode, independent of the default.
    config.write_text('[composer]\nmode = "normal"\nenter_sends = false\n')
    db = root / "data/tidesms/state.db"

    def rows(sql):
        if not db.exists():
            return []
        with sqlite3.connect(db) as conn:
            return conn.execute(sql).fetchall()

    def launch():
        master, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))
        proc = subprocess.Popen([str(BINARY)], stdin=slave, stdout=slave,
                                stderr=slave, env=env, start_new_session=True)
        os.close(slave)
        return master, proc

    master, proc = launch()
    output = bytearray()

    def drain(seconds=0.15):
        end = time.monotonic() + seconds
        while time.monotonic() < end:
            if select.select([master], [], [], 0.02)[0]:
                try:
                    data = os.read(master, 65536)
                except OSError:
                    return
                output.extend(data)
                if b"\x1b]11;?" in data:
                    os.write(master, b"\x1b]11;rgb:1e1e/1e1e/2e2e\x1b\\")
                if b"\x1b[6n" in data:
                    os.write(master, b"\x1b[1;1R")

    def until(predicate, message, timeout=5):
        end = time.monotonic() + timeout
        while time.monotonic() < end:
            drain()
            if predicate():
                return
        raise AssertionError(message + "\n" + output[-2000:].decode(errors="replace"))

    def key(data):
        os.write(master, data.encode() if isinstance(data, str) else data)
        drain()

    def palette(command):
        key(b"\x10")
        key(command)
        key(b"\r")

    def draft():
        # Milestone 2 keys drafts by thread once a conversation exists; before
        # that the recipient's number is still the key.
        bodies = dict(rows("SELECT recipient,body FROM drafts"))
        for key, body in bodies.items():
            if key.endswith("local-+15551234567"):
                return body
        return bodies.get("+15551234567")

    try:
        until(lambda: b"Connected" in output, "phone not discovered")
        key("c")  # Threads is the opening pane; c reaches contacts.
        key("a")
        key("Amy")
        key(b"\r")
        key("+1 (555) 123-4567")
        key(b"\r")
        until(lambda: len(rows("SELECT * FROM contacts")) == 1, "contact not saved")
        key("Hello from the PTY")
        key(b"\r")
        key("Second line 💜")
        expected = "Hello from the PTY\nSecond line 💜"
        until(lambda: draft() == expected, "multiline draft not saved")
        check(not (root / "sent.jsonl").exists(), "Enter alone sent a message")
        (root / "fail").touch()
        key(b"\x1b[13;5u")  # Kitty Ctrl+Enter.
        until(lambda: b"could not complete" in output, "send failure not displayed")
        check(draft() == expected, "failure discarded draft")
        (root / "fail").unlink()
        key(b"\x1b[27;5;13~")  # xterm Ctrl+Enter.
        until(lambda: draft() == "", "successful send did not clear draft")
        sent = [json.loads(s) for s in (root / "sent.jsonl").read_text().splitlines()]
        check(len(sent) == 1 and sent[0][3] == expected, "wrong CLI message arguments")
        key("Sent with Alt+Enter")
        key(b"\x1b[13;3u")  # Kitty Alt+Enter.
        until(lambda: len((root / "sent.jsonl").read_text().splitlines()) == 2,
              "Alt+Enter did not send")
        check(draft() == "", "Alt+Enter send did not clear draft")
        # Exercise navigation through real terminal escape sequences.
        output.clear()
        key(b"\x1b2")
        until(lambda: b"History" in output, "Alt+2 did not focus history")
        key(b"\x1b1")
        key(b"\t")
        key("navigation draft")
        until(lambda: draft() == "navigation draft", "Tab did not return to composing")
        key(b"\x1b[Z")  # Shift+Tab back to sidebar.
        key(b"\x1b3")
        key(" preserved")
        until(lambda: draft() == "navigation draft preserved", "Alt+3 lost draft or caret")
        key(b"\x1b[F")  # End of line.
        key(b"\x7f" * len("navigation draft preserved"))
        until(lambda: draft() == "", "navigation test draft did not clear")
        palette("contact theme")
        key(b"\x1b[B")
        key(b"\x1b[B")  # automatic, then TideUI's themes in order
        key(b"\r")
        until(lambda: bool(rows("SELECT theme FROM contacts")) and rows("SELECT theme FROM contacts")[0][0] not in ("", "automatic"),
              "theme not saved")
        palette("toggle")
        until(lambda: 'mode = "vim"' in (root / "config/tidesms/config.toml").read_text(), "mode not saved")
        key("i")
        key("Saved Vim draft")
        key(b"\x1b")
        until(lambda: draft() == "Saved Vim draft", "Vim draft not saved")
        palette("quit")
        proc.wait(timeout=5)
        check(proc.returncode == 0, "unclean exit")
        os.close(master)
        master, proc = launch()
        output.clear()
        until(lambda: b"Amy" in output, "thread missing after restart")
        key(b"\r")
        until(lambda: b"Saved Vim draft" in output, "draft missing after restart")
        # Plain Ripple always reports INSERT, so NORMAL proves Vim was restored.
        check(b"NORMAL" in output, "Vim mode not restored")
        palette("quit")
        proc.wait(timeout=5)
        check(proc.returncode == 0, "unclean second exit")
        check(len(rows("SELECT * FROM threads")) >= 1, "no thread recorded for the send")
        check(rows("SELECT direction,status FROM messages")[0][0] == "outgoing",
              "outgoing message not cached")
        print("PASS: real PTY, panes, contacts, multiline editing, both Ctrl+Enter")
        print("      protocols, Alt+Enter, failure retention, CLI args, cached outgoing")
        print("      message, theme/mode persistence, thread draft restart.")
    finally:
        if proc.poll() is None:
            proc.terminate()
            proc.wait(timeout=5)
        os.close(master)
