#!/usr/bin/env python3
"""Crops a console screenshot for a guide: drops the sidebar and top bar and keeps rows y0..y1 of the page content.

    scripts/docs/crop.py docs/console/158-runbook-fix-approve.png runbooks-approve-fix.png 118 500

Optional x0 x1 after y1 for screenshots that are not 1440 wide. The new file is written to docs/console/. Full-window shots are 1440 wide; the content column is x 276..1404.
Look at the source first (open the PNG) to choose y0 and y1: crop to the part the page is talking about.
"""
import sys
from PIL import Image

src, name, y0, y1 = sys.argv[1], sys.argv[2], int(sys.argv[3]), int(sys.argv[4])
im = Image.open(src).convert("RGB")
w, h = im.size
x0, x1 = (276, 1404) if w == 1440 else (0, w)
if len(sys.argv) == 7:  # other window sizes: give x0 and x1 yourself
    x0, x1 = int(sys.argv[5]), int(sys.argv[6])
out = im.crop((x0, y0, x1, min(y1, h)))
out.save("docs/console/" + name, optimize=True)
print(name, out.size)
