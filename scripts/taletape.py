"""Record `sapper tale` from a real pseudo-terminal at real timing and encode it.

Usage: taletape.py OUT.(mp4|gif) "COMMAND" [start_seconds] [fps]
The command runs in an 80x32 pty; its output is timestamped as it arrives, replayed
into pyte and sampled at fps, and each sample is drawn with termshot.screen_image.
"""
import os, pty, select, subprocess, sys, time, struct, fcntl, termios
import pyte
sys.path.insert(0, os.path.dirname(__file__))
from termshot import screen_image

COLS, ROWS = 80, 32


def capture(cmd):
    pid, fd = pty.fork()
    if pid == 0:
        env = dict(os.environ); env.pop("NO_COLOR", None); env.pop("SAPPER_NO_ANIM", None)
        env["TERM"] = "xterm-256color"
        os.execvpe("bash", ["bash", "-c", cmd], env)
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
    t0, chunks = time.time(), []
    while True:
        r, _, _ = select.select([fd], [], [], 0.5)
        if r:
            try:
                data = os.read(fd, 65536)
            except OSError:
                break
            if not data:
                break
            chunks.append((time.time() - t0, data))
    os.waitpid(pid, 0)
    return chunks


def main():
    out, cmd = sys.argv[1], sys.argv[2]
    start = float(sys.argv[3]) if len(sys.argv) > 3 else 0
    fps = int(sys.argv[4]) if len(sys.argv) > 4 else 25
    chunks = capture(cmd)
    screen = pyte.Screen(COLS, ROWS); stream = pyte.ByteStream(screen)
    end = chunks[-1][0] if chunks else 0
    # The final normal-screen summary replaces the cutscene; stop at the last frame of it.
    alt_exit = next((t for t, d in chunks if b"\x1b[?1049l" in d), end)
    stop = alt_exit
    enc = ["ffmpeg", "-y", "-loglevel", "error", "-f", "rawvideo", "-pix_fmt", "rgb24"]
    first = None
    i, k, t = 0, 0, start
    proc = None
    while t <= stop:
        while i < len(chunks) and chunks[i][0] <= t:
            data = chunks[i][1]
            if b"\x1b[?1049l" in data:
                data = data.split(b"\x1b[?1049l")[0]
            stream.feed(data); i += 1
        img = screen_image(screen, rows=30)
        if proc is None:
            w, h = img.size
            w, h = w - w % 2, h - h % 2
            args = enc + ["-s", "%dx%d" % (w, h), "-r", str(fps), "-i", "-"]
            if out.endswith(".gif"):
                args += ["-vf", "fps=%d,split[a][b];[a]palettegen=max_colors=96[p];[b][p]paletteuse=dither=none" % min(fps, 15), out]
            else:
                args += ["-c:v", "libx264", "-pix_fmt", "yuv420p", "-crf", "20", out]
            proc = subprocess.Popen(args, stdin=subprocess.PIPE)
        proc.stdin.write(img.crop((0, 0, w, h)).tobytes())
        k += 1; t = start + k / fps
    proc.stdin.close(); proc.wait()
    print("%s %.1fs" % (out, k / fps))


if __name__ == "__main__":
    main()
