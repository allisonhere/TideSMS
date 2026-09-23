#!/usr/bin/env python3
"""Drive the real TideSMS binary through a pty and read the screen back.

The unit tests exercise the model with fakes. This drives the built binary the
way a terminal does, which is the only way to see what the app actually shows:
a settings flow whose every test passed still had the reader pressing Enter
against a row that only reopened, because the test double always succeeded and
the failure path never ran.

Requires pyte (`pip install pyte`).

    python3 scripts/drive.py            # start it and dump the first screen
    python3 scripts/drive.py --keys ",jjj\\r"   # send keys, then dump

Everything runs against a throwaway HOME seeded from the real config. Every XDG
root is overridden, not just HOME: the app resolves its paths through
XDG_CONFIG_HOME when that is set, so overriding HOME alone sends writes to the
real configuration.
"""

import argparse
import fcntl
import os
import pty
import select
import shutil
import struct
import subprocess
import sys
import tempfile
import termios
import time

try:
    import pyte
except ImportError:
    sys.exit("pyte is required: pip install pyte")

COLS, ROWS = 150, 45


def build(repo):
    binary = os.path.join(tempfile.mkdtemp(), "tidesms")
    subprocess.run(["go", "build", "-o", binary, "./cmd/tidesms"], cwd=repo, check=True)
    return binary


def sandbox():
    """A throwaway HOME seeded from the real config, so a probe cannot write to it."""
    home = tempfile.mkdtemp()
    os.makedirs(os.path.join(home, ".config", "tidesms"))
    real = os.path.expanduser("~/.config/tidesms/config.toml")
    if os.path.exists(real):
        shutil.copy(real, os.path.join(home, ".config", "tidesms", "config.toml"))
    env = dict(os.environ)
    env["HOME"] = home
    env["XDG_CONFIG_HOME"] = os.path.join(home, ".config")
    env["XDG_DATA_HOME"] = os.path.join(home, ".local", "share")
    env["XDG_CACHE_HOME"] = os.path.join(home, ".cache")
    env["XDG_STATE_HOME"] = os.path.join(home, ".local", "state")
    env["TERM"] = "xterm-256color"
    env["COLORTERM"] = "truecolor"
    return home, env


class Session:
    def __init__(self, binary, env):
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.execvpe(binary, [binary], env)
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
        self.screen = pyte.Screen(COLS, ROWS)
        self.stream = pyte.ByteStream(self.screen)

    def pump(self, seconds=0.8):
        end = time.time() + seconds
        while time.time() < end:
            ready, _, _ = select.select([self.fd], [], [], 0.05)
            if not ready:
                continue
            try:
                data = os.read(self.fd, 65536)
            except OSError:
                return
            if not data:
                return
            # The program queries the terminal and blocks until answered.
            if b"\x1b]11;?" in data:
                os.write(self.fd, b"\x1b]11;rgb:1e1e/1e1e/2e2e\x1b\\")
            if b"\x1b[6n" in data:
                os.write(self.fd, b"\x1b[1;1R")
            self.stream.feed(data)

    def send(self, keys, wait=0.8):
        os.write(self.fd, keys if isinstance(keys, bytes) else keys.encode())
        self.pump(wait)

    def dump(self, label=""):
        if label:
            print("=" * 25, label, "=" * 25)
        for line in self.screen.display:
            if line.strip():
                print("|" + line.rstrip())


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--keys", default="", help="keys to send after startup")
    parser.add_argument("--wait", type=float, default=4.0, help="seconds to wait for startup")
    args = parser.parse_args()

    repo = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    binary = build(repo)
    home, env = sandbox()
    session = Session(binary, env)
    session.pump(args.wait)
    session.dump("startup")
    if args.keys:
        session.send(args.keys.encode().decode("unicode_escape").encode(), 2.5)
        session.dump("after keys")
    print("--- sandbox config ---")
    print(open(os.path.join(home, ".config", "tidesms", "config.toml")).read())


if __name__ == "__main__":
    main()
