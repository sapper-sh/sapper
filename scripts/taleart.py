"""Draw the `sapper tale` panels and write them as half-block and ASCII text.

Original artwork for sapper. Each panel is drawn as a moonlit grey-scale scene with
PIL (light from the upper left), sampled two pixels per terminal cell and shaded with
full, half and shade blocks, the same vocabulary as the SAPPER boot emblem. The halo of
light shade around every silhouette follows the emblem too.

Inspiration, not copied: the line economy and negative space of Joan Stark (jgs), the
solid shading of the ACiD and iCE ANSI packs on 16colo.rs, and half-block truecolor
terminal art. The text files this writes are hand tuned afterwards only when noted.

Usage: python3 scripts/taleart.py internal/art/assets/tale
"""
import math, sys, os
from PIL import Image, ImageDraw, ImageFilter

S = 8                      # supersamples per half-cell
W, H = 76, 36              # columns, half-rows (18 text rows)
KX, KY = W / 66, H / 30    # scenes are drawn on a 66x30 grid and scaled up


class Canvas:
    def __init__(self, w=W, h=H):
        self.w, self.h = w, h
        self.im = Image.new("L", (w * S, h * S), 0)
        self.d = ImageDraw.Draw(self.im)

        self.marks = []

    def P(self, pts):
        return [(x * KX * S, y * KY * S) for x, y in pts]

    def poly(self, pts, v):
        self.d.polygon(self.P(pts), fill=v)

    def rect(self, x0, y0, x1, y1, v):
        self.d.rectangle((x0 * KX * S, y0 * KY * S, x1 * KX * S - 1, y1 * KY * S - 1), fill=v)

    def ellipse(self, cx, cy, rx, ry, v):
        self.d.ellipse(((cx - rx) * KX * S, (cy - ry) * KY * S, (cx + rx) * KX * S, (cy + ry) * KY * S), fill=v)

    def line(self, pts, v, w=1.0):
        self.d.line(self.P(pts), fill=v, width=max(1, int(w * S)), joint="curve")

    def stroke(self, f, t0, t1, v, w0, w1=None, n=120):
        """Thick tapered curve: f(t) -> (x, y)."""
        w1 = w0 if w1 is None else w1
        for i in range(n + 1):
            t = t0 + (t1 - t0) * i / n
            x, y = f(t)
            r = (w0 + (w1 - w0) * i / n) / 2
            self.ellipse(x, y, r, r, v)

    def light(self, lo=.55):
        """Moonlight from the upper left: scale every lit pixel by position."""
        px = self.im.load()
        sw, sh = self.im.size
        for y in range(0, sh):
            for x in range(0, sw):
                v = px[x, y]
                if v:
                    k = 1 - (1 - lo) * (x / sw * .7 + y / sh * .3)
                    px[x, y] = max(1, int(v * k))


def cells(c):
    """Return (coverage, brightness) per half-cell."""
    small = c.im.resize((c.w, c.h), Image.BOX)
    mask = c.im.point(lambda v: 255 if v else 0).resize((c.w, c.h), Image.BOX)
    return [[(mask.getpixel((x, y)) / 255, small.getpixel((x, y)) / 255) for x in range(c.w)] for y in range(c.h)]


def halo(cov, w, h):
    out = [[0] * w for _ in range(h)]
    for y in range(h):
        for x in range(w):
            if cov[y][x] > .2:
                continue
            for dy in (-1, 0, 1):
                for dx in (-1, 0, 1):
                    yy, xx = y + dy, x + dx
                    if 0 <= yy < h and 0 <= xx < w and cov[yy][xx] > .5:
                        out[y][x] = 1
    return out


def to_blocks(c, glow=True):
    g = cells(c)
    cov = [[v[0] for v in row] for row in g]
    hal = halo(cov, c.w, c.h) if glow else None
    lines = []
    for r in range(c.h // 2):
        s = ""
        for x in range(c.w):
            (ct, bt), (cb, bb) = g[2 * r][x], g[2 * r + 1][x]
            vt, vb = bt / max(ct, 1e-6), bb / max(cb, 1e-6)
            if ct > .55 and cb > .55:
                v = (vt + vb) / 2
                if abs(vt - vb) > .35:
                    s += "▀" if vt > vb else "▄"
                elif v > .66:
                    s += "█"
                elif v > .42:
                    s += "▓"
                elif v > .22:
                    s += "▒"
                else:
                    s += "░"
            elif ct > .55 and vt > .3:
                s += "▀"
            elif cb > .55 and vb > .3:
                s += "▄"
            elif ct + cb > .45:
                s += "▒" if max(vt, vb) > .45 else "░"
            elif ct + cb > .12:
                s += "░"
            elif hal and (hal[2 * r][x] or hal[2 * r + 1][x]):
                s += "░"
            else:
                s += " "
        lines.append(s)
    return trim(place(lines, c, 2))


def place(lines, c, k):
    for x, y, u, a in c.marks:
        if 0 <= y < len(lines) and x < len(lines[y]) and lines[y][x] == " ":
            ch = u if k == 2 else a
            lines[y] = lines[y][:x] + ch + lines[y][x + 1:]
    return [l.rstrip() for l in lines]


RAMP = " .:-=+*#%@"


def to_ascii(c):
    g = cells(c)
    lines = []
    for r in range(c.h // 2):
        s = ""
        for x in range(c.w):
            (ct, bt), (cb, bb) = g[2 * r][x], g[2 * r + 1][x]
            if ct > .5 and cb < .2:
                s += "'" if bt < .5 else '"'
            elif cb > .5 and ct < .2:
                s += "." if bb < .5 else "_"
            else:
                v = (bt + bb) / 2
                s += RAMP[min(len(RAMP) - 1, int(round(v * (len(RAMP) - 1) * 1.15)))] if ct + cb > .15 else " "
        lines.append(s)
    return trim(place(lines, c, 1))


def trim(lines):
    while lines and not lines[0].strip():
        lines.pop(0)
    while lines and not lines[-1].strip():
        lines.pop()
    pad = min(len(l) - len(l.lstrip()) for l in lines if l.strip())
    return [l[pad:] if l.strip() else "" for l in lines]


# ---------------------------------------------------------------- scenes

def stars(c, pts):
    """Stars are single characters placed after shading, so they get no halo."""
    for i, (x, y) in enumerate(pts):
        c.marks.append((int(x * KX), int(y * KY / 2), "+" if i % 3 == 0 else "·", "+" if i % 3 == 0 else "."))


def castle(c, ox=0, oy=0, flag=False, cracked=False):
    def tower(x, w, top, roof):
        c.rect(ox + x, oy + top, ox + x + w, oy + 28, 230)
        for i in range(0, int(w), 2):           # merlons
            c.rect(ox + x + i, oy + top - 1.6, ox + x + i + 1, oy + top, 230)
        c.poly([(ox + x - .6, oy + top - 1.6), (ox + x + w / 2, oy + top - roof), (ox + x + w + .6, oy + top - 1.6)], 255)
        c.rect(ox + x + w / 2 - .5, oy + top + 3, ox + x + w / 2 + .6, oy + top + 6, 0)   # arrow slit
    # curtain wall with bricks
    c.rect(ox + 8, oy + 14, ox + 50, oy + 28, 175)
    for i in range(8, 50, 2):
        c.rect(ox + i, oy + 12.4, ox + i + 1, oy + 14, 175)
    for row, y in enumerate(range(16, 28, 2)):
        for i in range(8 + (row % 2) * 2, 50, 4):
            c.rect(ox + i, oy + y, ox + i + .35, oy + y + 2, 120)
        c.rect(ox + 8, oy + y, ox + 50, oy + y + .3, 120)
    tower(2, 7, 9, 7)
    tower(49, 7, 9, 7)
    tower(23, 12, 5, 9)
    # keep window, glowing
    c.ellipse(ox + 29, oy + 9.5, 1.6, 2, 0)
    c.ellipse(ox + 29, oy + 9.8, .9, 1.2, 255)
    # gate
    c.rect(ox + 24.5, oy + 21, ox + 33.5, oy + 28, 0)
    c.ellipse(ox + 29, oy + 21, 4.5, 3.2, 0)
    for i in range(25, 34, 2):
        c.rect(ox + i, oy + 19, ox + i + .4, oy + 28, 90)       # portcullis
    for y in (22, 25):
        c.rect(ox + 24.5, oy + y, ox + 33.5, oy + y + .4, 90)
    if flag:
        fx = ox + 5.2                            # flag on the left tower
        c.rect(fx, oy - .5, fx + .6, oy + 3, 255)
        c.poly([(fx + .6, oy - .5), (fx + 7, oy + .6), (fx + 4, oy + 1.4), (fx + 7.5, oy + 2.4), (fx + .6, oy + 2.2)], 255)
    if cracked:
        c.line([(ox + 14, oy + 13), (ox + 16, oy + 17), (ox + 14.5, oy + 20), (ox + 17.5, oy + 24), (ox + 16, oy + 28)], 0, .9)
        c.line([(ox + 43, oy + 15), (ox + 41, oy + 19), (ox + 44, oy + 22)], 0, .8)
        c.rect(ox + 50, oy + 9, ox + 56, oy + 13, 0)            # broken tower top
        for x, y, r in ((58, 26, 1.3), (60.5, 27, .9), (6, 27, 1.0)):
            c.ellipse(ox + x, oy + y, r, r * .7, 150)            # rubble


def ground(c, y=28, v=140):
    c.poly([(0, y), (16, y - .8), (34, y - .3), (50, y - 1), (66, y), (66, 30), (0, 30)], v)


def p_castle():
    c = Canvas()
    stars(c, [(1, 2), (9, 5), (15, 1), (40, 2), (47, 6), (62, 3), (57, 1)])
    c.ellipse(60, 6, 3, 3, 255)        # moon
    c.ellipse(61.4, 5.2, 2.6, 2.6, 0)
    castle(c, ox=4)
    ground(c)
    c.light()
    return c


def p_chest():
    c = Canvas()
    cx = 33
    # lid dome
    c.ellipse(cx, 11, 15, 5.5, 225)
    c.rect(cx - 15, 11, cx + 15, 13, 225)
    c.rect(cx - 15, 13, cx + 15, 14, 80)          # seam
    # body
    c.rect(cx - 15, 14, cx + 15, 26, 190)
    for i in range(-15, 15, 3):                  # planks
        c.rect(cx + i, 14, cx + i + .35, 26, 140)
    for x in (-12, 10):                          # iron bands
        c.rect(cx + x, 6, cx + x + 2, 26, 255)
    c.rect(cx - 15, 24, cx + 15, 26, 255)
    # lock plate and keyhole
    c.rect(cx - 2.5, 12, cx + 2.5, 19, 255)
    c.ellipse(cx, 15, .9, .9, 0)
    c.poly([(cx - .5, 15), (cx + .5, 15), (cx + .8, 17.6), (cx - .8, 17.6)], 0)
    # spilled drawings and coins
    for x, y in ((14, 26.5), (16.5, 27.5), (49, 27), (52, 26.2), (54.5, 27.6)):
        c.ellipse(x, y, 1.3, .9, 255)
    c.poly([(5, 28), (11, 25.5), (13, 28.5)], 200)   # a rolled-up drawing
    c.poly([(56, 28.5), (60, 24.5), (63, 27.5)], 200)
    # sparkle
    for x, y, r in ((12, 7, 2.2), (55, 5, 1.8), (48, 13, 1.2)):
        c.line([(x - r, y), (x + r, y)], 255, .35)
        c.line([(x, y - r * 1.3), (x, y + r * 1.3)], 255, .35)
    ground(c, 28.6, 110)
    c.light(.6)
    return c


def p_dragon():
    c = Canvas()
    # wings (back)
    c.poly([(30, 12), (20, 0.5), (14, 3.5), (8, 2), (4, 8), (14, 10), (22, 14)], 150)
    # body and tail
    c.ellipse(32, 15, 9, 4.2, 220)
    c.stroke(lambda t: (23 - 18 * t, 16 + 7 * math.sin(t * 2.6)), 0, 1, 210, 4.5, .9)
    c.poly([(3.5, 18.8), (0.5, 17), (2.5, 21.5)], 230)       # tail spade
    # neck and head
    c.stroke(lambda t: (39 + 9 * t, 13 - 6 * math.sin(t * 1.7)), 0, 1, 230, 4.2, 2.6)
    c.poly([(47, 5.5), (54, 6.2), (57.5, 8.3), (52.5, 9.1), (55.5, 10.3), (49, 10.6), (46.5, 9)], 245)
    c.poly([(47.5, 6), (45, 1.5), (49, 5.4)], 255)              # horn
    c.ellipse(51, 7.2, .55, .5, 0)                              # eye
    # front wing with finger bones
    c.poly([(34, 12), (40, -1), (46, 1), (44, 4), (47, 6), (41, 9), (38, 14)], 255)
    for a, b in (((35, 12), (40.5, 0)), ((36, 12.5), (45.5, 1.3)), ((37, 13), (46.5, 6))):
        c.line([a, b], 120, .45)
    # belly scales
    for i in range(26, 39, 2):
        c.line([(i, 17.5), (i + 1, 18.8)], 130, .35)
    # legs and claws
    for x in (27, 35):
        c.stroke(lambda t, x=x: (x + 1.5 * t, 18 + 5 * t), 0, 1, 210, 2.6, 1.6)
        c.poly([(x + .3, 23), (x + 3.4, 23), (x + 3.8, 24.3), (x - .4, 24.2)], 230)
    # castle being smashed, small, bottom right
    c.rect(58, 22, 66, 30, 120)
    c.rect(59, 19.5, 62, 22, 120)
    c.poly([(62.5, 22), (64, 20), (65.5, 22)], 120)
    c.rect(61, 25, 63, 30, 0)
    c.light(.5)
    # fire breath, drawn after the moonlight so it stays the brightest thing
    for i in range(18):
        t = i / 17
        c.ellipse(57.5 + 6 * t, 9.3 + 9 * t * t, .5 + 2.1 * t, .4 + 1.7 * t, int(255 - 60 * t))
    return c


def p_knight():
    c = Canvas()
    cx = 26
    # cape
    c.poly([(cx - 4, 9), (cx + 4, 9), (cx + 8, 26), (cx - 8, 26)], 110)
    # helmet, visor, plume
    c.ellipse(cx, 5, 3.3, 3.6, 240)
    c.rect(cx - 3.3, 5, cx + 3.3, 8.5, 240)
    c.rect(cx - 2.4, 5.2, cx + 2.4, 5.9, 0)
    for i in range(-2, 3):
        c.rect(cx + i - .1, 6.6, cx + i + .2, 8, 120)
    c.stroke(lambda t: (cx + 7 * t, 1.6 - 3 * math.sin(t * 2.2) + 2 * t), 0, 1, 255, 1.8, .6)
    # body armour
    c.poly([(cx - 4.2, 9), (cx + 4.2, 9), (cx + 3.2, 18), (cx - 3.2, 18)], 225)
    c.rect(cx - 3.4, 13.5, cx + 3.4, 14.1, 100)
    c.rect(cx - 3.6, 17.5, cx + 3.6, 19, 255)                 # belt
    # legs
    c.rect(cx - 3, 19, cx - .6, 27, 210)
    c.rect(cx + .6, 19, cx + 3, 27, 200)
    c.rect(cx - 3.6, 26.3, cx - .3, 28, 255)
    c.rect(cx + .3, 26.3, cx + 3.6, 28, 240)
    # sword arm, raised
    c.stroke(lambda t: (cx + 4 + 4 * t, 10 - 3 * t), 0, 1, 230, 2.2, 1.8)
    c.rect(cx + 7, 6.2, cx + 11, 7.2, 255)                     # crossguard
    c.poly([(cx + 8.4, 6.2), (cx + 9.6, 6.2), (cx + 9.4, -1.5), (cx + 9, -3.5), (cx + 8.6, -1.5)], 255)
    c.rect(cx + 8.65, 7.2, cx + 9.35, 9.4, 160)
    # shield with the sapper emblem diamond
    sx, sy = cx - 8, 15
    c.poly([(sx - 5.5, sy - 6), (sx + 5.5, sy - 6), (sx + 5.5, sy + 1), (sx, sy + 8), (sx - 5.5, sy + 1)], 250)
    c.poly([(sx - 4.4, sy - 5), (sx + 4.4, sy - 5), (sx + 4.4, sy + .6), (sx, sy + 6.6), (sx - 4.4, sy + .6)], 130)
    c.poly([(sx, sy - 4), (sx + 2.6, sy - .5), (sx, sy + 3), (sx - 2.6, sy - .5)], 255)
    c.ellipse(sx, sy - .5, .8, .8, 130)
    # practice dummy dragon, a wooden target with a painted dragon head
    dx = 50
    c.rect(dx - .5, 9, dx + .5, 28, 150)
    c.rect(dx - 5, 13, dx + 5, 14, 150)
    c.ellipse(dx, 8, 4, 3.6, 190)
    c.ellipse(dx, 8, 2.6, 2.3, 0)
    c.poly([(dx - 1.6, 7), (dx + 2.2, 8), (dx - 1.6, 9)], 200)
    c.poly([(dx - 3, 4.8), (dx - 4.5, 1.8), (dx - 1.5, 4.6)], 190)
    c.poly([(dx + 3, 4.8), (dx + 4.5, 1.8), (dx + 1.5, 4.6)], 190)
    # practice marks on the ground
    for x in (40, 43, 57):
        c.line([(x, 27), (x + 1.2, 25.8)], 160, .35)
    stars(c, [(4, 2), (14, 4), (40, 1), (60, 3), (64, 9)])
    ground(c)
    c.light(.55)
    return c


def p_survived():
    c = Canvas()
    stars(c, [(3, 2), (12, 5), (18, 1), (46, 1), (52, 4), (61, 2), (64, 7)])
    castle(c, ox=4, oy=2, flag=True)
    ground(c)
    c.light(.6)
    return c


def p_breached():
    c = Canvas()
    stars(c, [(3, 2), (12, 5), (40, 1)])
    for i in range(14):                    # smoke drifting right
        t = i / 13
        c.ellipse(57 + 8 * t, 9 - 8 * t, .8 + 1.6 * t, .7 + 1.3 * t, int(150 - 70 * t))
    castle(c, ox=4, cracked=True)
    ground(c)
    c.light(.6)
    return c


PANELS = {"castle": p_castle, "chest": p_chest, "dragon": p_dragon, "knight": p_knight,
          "survived": p_survived, "breached": p_breached}

if __name__ == "__main__":
    out = sys.argv[1]
    os.makedirs(out, exist_ok=True)
    for name, f in PANELS.items():
        c = f()
        with open(os.path.join(out, name + ".txt"), "w") as fh:
            fh.write("\n".join(to_blocks(c)) + "\n")
        with open(os.path.join(out, name + ".ascii"), "w") as fh:
            fh.write("\n".join(to_ascii(c)) + "\n")
