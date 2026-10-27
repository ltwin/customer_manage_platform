"""Compares the lit exports of two or three gi-baseline runs, image by image, as S1's ac-s1-3.md table does:
pixels differing in any RGB channel, the largest difference (0-255), pixels off by more than 8 levels.
usage: python3 -I compare.py <before> <after> [<after again>] [--only webgpu-off,webgl2]"""
import sys
from pathlib import Path

import numpy as np
from PIL import Image

args = [a for a in sys.argv[1:] if not a.startswith('--')]
only = next((a.split('=', 1)[1].split(',') for a in sys.argv[1:] if a.startswith('--only=')), None)
dirs = [Path(a) for a in args]


def load(p):
    return np.asarray(Image.open(p).convert('RGB'), dtype=np.int16)


def diff(a, b):
    d = np.abs(a - b).max(axis=2)
    return (d > 0).mean() * 100, int(d.max()), (d > 8).mean() * 100


pairs = [(0, 1)] + ([(1, 2), (0, 2)] if len(dirs) > 2 else [])
print('| image | ' + ' | '.join(f'{dirs[i].name} ↔ {dirs[j].name}: differing, max, >8' for i, j in pairs) + ' |')
print('|---|' + '---|' * len(pairs))
worst = {p: (0.0, 0, 0.0) for p in pairs}
for f in sorted(dirs[0].glob('*-lit.png')):
    if only and not any(f.name.endswith(f'-{o}-lit.png') for o in only):
        continue
    imgs = [load(d / f.name) for d in dirs]
    cells = []
    for p in pairs:
        r = diff(imgs[p[0]], imgs[p[1]])
        worst[p] = tuple(max(x, y) for x, y in zip(worst[p], r))
        cells.append(f'{r[0]:.2f}%, {r[1]}, {r[2]:.4f}%')
    print(f'| `{f.name.removesuffix("-lit.png")}` | ' + ' | '.join(cells) + ' |')
print('| worst | ' + ' | '.join(f'{w[0]:.2f}%, {w[1]}, {w[2]:.4f}%' for w in worst.values()) + ' |')
