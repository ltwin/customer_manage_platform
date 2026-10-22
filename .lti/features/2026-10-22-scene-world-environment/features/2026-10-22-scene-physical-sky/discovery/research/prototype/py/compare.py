# Compare PNGs of the same shot between backends: mean |diff|, share of pixels off by more than 8 and 2, max.
import sys, json
import numpy as np
from PIL import Image
d = sys.argv[1]
names = sys.argv[2].split(',')
pairs = [('webgpu', 'webgl'), ('webgpu', 'swiftshader')]
out = {}
for n in names:
    imgs = {}
    for b in ['webgpu', 'webgl', 'swiftshader']:
        try:
            imgs[b] = np.asarray(Image.open(f'{d}/{n}-{b}.png').convert('RGB')).astype(np.int16)
        except FileNotFoundError:
            pass
    row = {}
    for a, b in pairs:
        if a in imgs and b in imgs:
            diff = np.abs(imgs[a] - imgs[b]).max(axis=2)
            row[f'{a}-{b}'] = {'mean': round(float(np.abs(imgs[a] - imgs[b]).mean()), 3), 'over8': round(float((diff > 8).mean() * 100), 3), 'over2': round(float((diff > 2).mean() * 100), 3), 'max': int(diff.max())}
    out[n] = row
    print(n, json.dumps(row))
json.dump(out, open(f'{d}/compare.json', 'w'), indent=1)
