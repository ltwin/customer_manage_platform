# Pixel differences between backends' PNGs: python3 -I measure/compare.py <dir> <a> <b> name...
import sys
import numpy as np
from PIL import Image
d, a, b, *names = sys.argv[1:]
for n in names:
    x = np.asarray(Image.open(f'{d}/{n}-{a}.png').convert('RGB')).astype(float)
    y = np.asarray(Image.open(f'{d}/{n}-{b}.png').convert('RGB')).astype(float)
    diff = np.abs(x - y).max(axis=2)
    print(f'{n:14s} mean {np.abs(x - y).mean():.3f}  >8: {(diff > 8).mean() * 100:.3f}%  max {diff.max():.0f}')
