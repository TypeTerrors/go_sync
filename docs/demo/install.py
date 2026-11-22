#!/usr/bin/env python3
"""Illustrated install guide with actual output from integration/install.py."""
import json
from pathlib import Path
from PIL import Image
import render as art

HERE = Path(__file__).resolve().parent
trace = json.loads((HERE / 'install-trace.json').read_text())
assert trace['exit_code'] == 0
command = 'curl -fsSL https://raw.githubusercontent.com/TypeTerrors/go_sync/main/install.sh | bash'
frames, durations = [], []


def frame(step, typed='', lines=(), duration=1400):
    im, draw = art.canvas(430)
    art.text(draw, (34, 25), 'ONE COMMAND. YOUR FOLDER, EVERYWHERE.', 20, art.ACCENTS[0], 'bold')
    for index, label in enumerate(('1  COPY', '2  PASTE', '3  PRESS ENTER')):
        x = 35 + index * 408
        art.box(draw, (x, 65, x + 388, 111), art.PANEL,
                art.ACCENTS[0] if index == step else art.LINE, 12)
        art.text(draw, (x + 18, 77), label, 21,
                 art.WHITE if index == step else art.FAINT, 'bold')
    art.box(draw, (34, 134, 1246, 388), art.INSET, art.LINE, 16)
    art.text(draw, (56, 151), 'TERMINAL', 17, art.FAINT, 'mono')
    art.text(draw, (56, 188), '$ ' + typed, 22, art.WHITE, 'mono')
    for i, line in enumerate(lines):
        art.text(draw, (56, 229 + i * 32), line, 22, art.ACCENTS[0] if i == len(lines)-1 else art.MUTED, 'mono')
    art.text(draw, (36, 402), 'macOS + Ubuntu  /  Intel + ARM64', 16, art.MUTED)
    art.text(draw, (1244, 402), 'Illustrated guide · recorded Ubuntu installer output', 16, art.MUTED, anchor='ra')
    frames.append(im);durations.append(duration)

frame(0, command, ('Copy the command from the README code block.',), 2300)
frame(1, '', ('Open Terminal and paste the command.',), 1000)
for n in range(6, len(command), 6):
    frame(1, command[:n] + '▌', duration=50)
frame(1, command, ('Press Enter. Ubuntu may ask for your administrator password.',), 2200)
output = [line.strip() for line in trace['installer_output']]
frame(2, command, (output[0], output[2], output[3]), 2400)
frame(2, command, (output[4], output[5], 'Next: open GoSync and choose your folder.'), 3500)
# Global palette avoids flicker; disposal=1 preserves changed-region encoding.
palette = frames[-1].quantize(colors=128).getpalette()
base = Image.new('P', (1, 1));base.putpalette(palette)
converted = [im.quantize(palette=base, dither=Image.Dither.NONE) for im in frames]
art.OUT.mkdir(exist_ok=True)
converted[0].save(art.OUT/'quick-install.gif', save_all=True, append_images=converted[1:],
                  duration=durations, loop=0, optimize=True, disposal=1)
frames[0].save(art.OUT/'quick-install-poster.png', optimize=True)
print(f'{len(frames)} frames, {sum(durations)/1000:.1f}s')
