#!/usr/bin/env python3
"""Take the README screenshot from invented data.

Nothing here can reach a real phone or a real conversation: it drives
scripts/demo, the real interface over the in-memory test backend, under a
throwaway HOME. No window opens either: the screen is read through a
pseudo-terminal and drawn to a PNG with ImageMagick.

Requires pyte (`pip install pyte`), ImageMagick and JetBrains Mono.

    python3 scripts/screenshot.py              # writes docs/screenshot.png
    python3 scripts/screenshot.py --text       # print the screen instead
"""

import argparse
import os
import subprocess
import sys
import tempfile
import time

sys.path.insert(0, os.path.dirname(__file__))
sys.dont_write_bytecode = True
import drive  # noqa: E402

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
COLS, ROWS = 132, 38
FONT_DIR = "/usr/share/fonts/TTF"
FONTS = {False: "JetBrainsMonoNerdFont-Regular.ttf", True: "JetBrainsMonoNerdFont-Bold.ttf"}
SIZE = 26
# JetBrains Mono's metrics: 600 units wide, 1020 up and 300 down, per 1000.
CELL_W, CELL_H, BASELINE = SIZE * 0.6, SIZE * 1.32, SIZE * 1.02
DEFAULT_BG, DEFAULT_FG = "1a1b26", "c0caf5"


def isolated():
    """A fresh HOME, so nothing is read from or written to the real one."""
    home = tempfile.mkdtemp(prefix="tidesms-shot-")
    env = {k: v for k, v in os.environ.items() if not k.startswith("XDG_")}
    env.update(HOME=home, XDG_CONFIG_HOME=os.path.join(home, "config"), XDG_DATA_HOME=os.path.join(home, "data"),
               XDG_STATE_HOME=os.path.join(home, "state"), XDG_CACHE_HOME=os.path.join(home, "cache"),
               TERM="xterm-256color", COLORTERM="truecolor")
    return home, env


def build():
    binary = os.path.join(tempfile.mkdtemp(), "tidesms-demo")
    subprocess.run(["go", "build", "-o", binary, "./scripts/demo"], cwd=REPO, check=True)
    return binary


def capture():
    drive.COLS, drive.ROWS = COLS, ROWS
    home, env = isolated()
    session = drive.Session(build(), env)
    session.pump(3)
    # Open the first thread, ready to reply, as Enter does.
    session.send("\r", 1)
    screen = session.screen
    os.kill(session.pid, 9)
    return screen


def colour(value, default):
    if not value or value == "default":
        return "#" + default
    if len(value) == 6 and all(c in "0123456789abcdefABCDEF" for c in value):
        return "#" + value
    return value  # pyte's names for the 16 base colours are valid here too


def quote(text):
    return "'" + text.replace("\\", "\\\\").replace("'", "\\'") + "'"


def render(screen, out):
    """Draw every cell: its background as a rectangle, its glyph at its column,
    so the image keeps the terminal's grid whatever the font's advance."""
    width, height = round(COLS * CELL_W), round(ROWS * CELL_H)
    mvg = [f"viewbox 0 0 {width} {height}", f"fill #{DEFAULT_BG}", f"rectangle 0,0 {width},{height}",
           f"font-size {SIZE}", "text-antialias 1"]
    for y in range(ROWS):
        row = screen.buffer[y]
        for x in range(COLS):
            cell = row[x]
            fg, bg = colour(cell.fg, DEFAULT_FG), colour(cell.bg, DEFAULT_BG)
            if cell.reverse:
                fg, bg = bg, fg
            x0, y0 = x * CELL_W, y * CELL_H
            if bg != "#" + DEFAULT_BG:
                # A hair of overlap so neighbouring cells leave no seam.
                mvg.append(f"fill {bg}")
                mvg.append(f"rectangle {x0:.2f},{y0:.2f} {x0 + CELL_W + 0.6:.2f},{y0 + CELL_H + 0.6:.2f}")
            if cell.data.strip():
                mvg.append(f"font {quote(os.path.join(FONT_DIR, FONTS[bool(cell.bold)]))}")
                mvg.append(f"fill {fg}")
                mvg.append(f"text {x0:.2f},{y0 + BASELINE:.2f} {quote(cell.data)}")
    with tempfile.NamedTemporaryFile("w", suffix=".mvg", delete=False) as f:
        f.write("\n".join(mvg))
    os.makedirs(os.path.dirname(out), exist_ok=True)
    subprocess.run(["magick", "-size", f"{width}x{height}", f"mvg:{f.name}", "-strip", out], check=True)
    os.unlink(f.name)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--text", action="store_true", help="print the screen instead of drawing it")
    parser.add_argument("--out", default=os.path.join(REPO, "docs", "screenshot.png"))
    args = parser.parse_args()
    screen = capture()
    if args.text:
        for line in screen.display:
            print("|" + line.rstrip())
        return
    start = time.time()
    render(screen, args.out)
    print(f"wrote {args.out} in {time.time() - start:.1f}s")


if __name__ == "__main__":
    main()
