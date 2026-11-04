"""Render terminal output (ANSI) to PNG screenshots and asciinema casts to GIF, using pyte + Pillow."""
# Usage: termshot.py shot OUT.png "COMMAND"   |   termshot.py gif IN.cast OUT.gif
import json, os, pty, select, subprocess, sys, struct, fcntl, termios
import pyte
from PIL import Image, ImageDraw, ImageFont

FONT = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf", 15)
CW, CH = 9, 19
BG = (5, 10, 18)
FG = (205, 214, 224)
NAMED = {"black": (0,0,0), "red": (235,20,40), "green": (80,200,120), "brown": (220,180,60), "blue": (0,140,200),
         "magenta": (190,90,200), "cyan": (120,225,255), "white": (230,230,230), "default": None}

def color(c, default):
    if c in NAMED:
        return NAMED[c] or default
    if isinstance(c, str) and len(c) == 6:
        return tuple(int(c[i:i+2], 16) for i in (0, 2, 4))
    return default

def screen_image(screen, rows=None, pad=14):
    lines = screen.display
    if rows is None:
        rows = max((i for i, l in enumerate(lines) if l.strip()), default=0) + 1
    cols = screen.columns
    img = Image.new("RGB", (cols * CW + 2 * pad, rows * CH + 2 * pad), BG)
    d = ImageDraw.Draw(img)
    for y in range(rows):
        row = screen.buffer[y]
        for x in range(cols):
            ch = row[x]
            px, py = pad + x * CW, pad + y * CH
            bg = color(ch.bg, None)
            if bg is not None:  # background colours carry the lower half of half-block pixels
                d.rectangle([px, py, px + CW - 1, py + CH - 1], fill=bg)
            if ch.data in (" ", ""):
                continue
            fg = color(ch.fg, FG)
            if ch.bold and fg == FG:
                fg = (240, 252, 255)
            px, py = pad + x * CW, pad + y * CH
            # Draw block elements as rectangles so they tile without font gaps.
            blocks = {"█": (0, 0, 1, 1), "▀": (0, 0, 1, .5), "▄": (0, .5, 1, 1), "▌": (0, 0, .5, 1), "▐": (.5, 0, 1, 1)}
            if ch.data in blocks:
                a, b, c2, e = blocks[ch.data]
                d.rectangle([px + a * CW, py + b * CH, px + c2 * CW - 1, py + e * CH - 1], fill=fg)
            else:
                d.text((px, py), ch.data, font=FONT, fill=fg)
    return img

def run_pty(cmd, cols, rows, env):
    pid, fd = pty.fork()
    if pid == 0:
        os.execvpe("bash", ["bash", "-c", cmd], env)
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
    out = b""
    while True:
        try:
            r, _, _ = select.select([fd], [], [], 30)
            if not r:
                break
            data = os.read(fd, 65536)
            if not data:
                break
            out += data
        except OSError:
            break
    os.waitpid(pid, 0)
    return out.decode("utf-8", "replace")

def shot(cmd, path, cols=80, rows=120):
    env = dict(os.environ); env.pop("NO_COLOR", None); env["TERM"] = "xterm-256color"; env["COLUMNS"] = str(cols)
    text = run_pty("stty cols %d rows %d; %s" % (cols, rows, cmd), cols, rows, env)
    screen = pyte.Screen(cols, rows); stream = pyte.Stream(screen); stream.feed(text.replace("\n", "\r\n"))
    screen_image(screen).save(path, optimize=True)
    return text

def gif(cast, path, fps=6, max_frames=900):
    lines = open(cast).read().splitlines()
    head = json.loads(lines[0]); cols, rows = head["width"], head["height"]
    events = [json.loads(l) for l in lines[1:]]
    screen = pyte.Screen(cols, rows); stream = pyte.Stream(screen)
    frames, durs, t_next, last = [], [], 0.0, None
    for t, kind, data in events:
        while t >= t_next and len(frames) < max_frames:
            frames.append(screen_image(screen, rows).convert("P", palette=Image.ADAPTIVE, colors=64)); durs.append(int(1000 / fps)); t_next += 1 / fps
        if kind == "o":
            stream.feed(data)
    frames.append(screen_image(screen, rows).convert("P", palette=Image.ADAPTIVE, colors=64)); durs.append(3000)
    # Collapse identical consecutive frames.
    out, od = [], []
    for f, dd in zip(frames, durs):
        if out and f.tobytes() == out[-1].tobytes():
            od[-1] += dd
        else:
            out.append(f); od.append(dd)
    out[0].save(path, save_all=True, append_images=out[1:], duration=od, loop=0, optimize=True)

if __name__ == "__main__":
    if sys.argv[1] == "shot":
        sys.stdout.write(shot(sys.argv[3], sys.argv[2]))
    else:
        gif(sys.argv[2], sys.argv[3])
