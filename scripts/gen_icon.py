#!/usr/bin/env python3
"""Generate the KuGou-style app icon (green rounded square + white music note).

Writes resources/icon.png (release) and resources/icon-dev.png (dev, with a
small DEV badge). Run from the repository root. Drawn at 4x then downscaled
for smooth edges, and the beam gets round caps so it meets the stems cleanly.
"""
import math
import os

from PIL import Image, ImageDraw, ImageFilter

S = 1024
SS = 4  # supersample factor


def _font(size):
    for path in (
        "C:/Windows/Fonts/arial.ttf",
        "C:/Windows/Fonts/segoeuib.ttf",
        "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf",
    ):
        if os.path.exists(path):
            from PIL import ImageFont
            return ImageFont.truetype(path, size)
    from PIL import ImageFont
    return ImageFont.load_default()


def rounded_square(size, radius, top, bot):
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    d.rounded_rectangle([0, 0, size - 1, size - 1], radius=radius, fill=(255, 255, 255, 255))
    grad = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    gd = ImageDraw.Draw(grad)
    for y in range(size):
        t = y / size
        r = int(top[0] + (bot[0] - top[0]) * t)
        g = int(top[1] + (bot[1] - top[1]) * t)
        b = int(top[2] + (bot[2] - top[2]) * t)
        gd.line([(0, y), (size, y)], fill=(r, g, b, 255))
    return Image.composite(grad, img, img.split()[3])


def rotated_head(base_size, center, rx, ry, angle_deg, color, width):
    # A tilted note head drawn on its own transparent layer and rotated about
    # its own centre, then composited over the note.
    cx, cy = center[0] * width, center[1] * width
    layer = Image.new("RGBA", base_size, (0, 0, 0, 0))
    ld = ImageDraw.Draw(layer)
    ld.ellipse([cx - rx * width, cy - ry * width, cx + rx * width, cy + ry * width],
               fill=color + (255,))
    return layer.rotate(angle_deg, resample=Image.BICUBIC, center=(cx, cy))


def draw_note(base, color, width):
    dx, dy = -30, -10

    def P(x, y):
        return ((x + dx) * width, (y + dy) * width)

    # Beam/stem endpoints in canvas space.
    left_top = P(494, 300)
    right_top = P(742, 206)
    left_stem_foot = P(404 + 60, 720 - 12)
    right_stem_foot = P(650 + 60, 624 - 12)
    # Heads, kept in source space for rotated_head, which scales them.
    left_head = (404 + dx, 720 + dy)
    right_head = (650 + dx, 624 + dy)

    img = base.copy()
    d = ImageDraw.Draw(img, "RGBA")
    beam_w = 72 * width
    stem_w = 40 * width
    # beam with round caps
    d.line([left_top, right_top], fill=color + (255,), width=int(beam_w))
    for p in (left_top, right_top):
        d.ellipse([p[0] - beam_w / 2, p[1] - beam_w / 2, p[0] + beam_w / 2, p[1] + beam_w / 2],
                  fill=color + (255,))
    # stems, drawn over the beam ends so they fuse
    d.line([left_stem_foot, left_top], fill=color + (255,), width=int(stem_w))
    d.line([right_stem_foot, right_top], fill=color + (255,), width=int(stem_w))
    # note heads cover the stem feet
    for head in (left_head, right_head):
        h = rotated_head(img.size, head, rx=92, ry=66, angle_deg=-24, color=color, width=width)
        img = Image.alpha_composite(img, h)
    return img


def render(size):
    width = size / S
    base = rounded_square(size, int(230 * width), (0x1F, 0xD7, 0x5E), (0x00, 0xA8, 0x4A))
    note = draw_note(base, (255, 255, 255), width)
    return note.resize((S, S), Image.LANCZOS)


def badge(img):
    out = img.copy()
    d = ImageDraw.Draw(out, "RGBA")
    bw, bh = 250, 86
    x0, y0 = S - bw - 70, S - bh - 72
    d.rounded_rectangle([x0, y0, x0 + bw, y0 + bh], radius=43, fill=(0, 0, 0, 150))
    d.text((x0 + 125, y0 + 46), "DEV", fill=(255, 255, 255, 255),
           font=_font(46), anchor="mm")
    return out


def main():
    os.makedirs("resources", exist_ok=True)
    note = render(S * SS)
    note.save("resources/icon.png")
    badge(note).save("resources/icon-dev.png")
    note.resize((256, 256)).save("resources/_preview.png")
    print("wrote resources/icon.png and resources/icon-dev.png")


if __name__ == "__main__":
    main()
