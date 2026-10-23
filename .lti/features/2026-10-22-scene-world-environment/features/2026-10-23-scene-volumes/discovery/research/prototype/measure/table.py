# Prints run.mjs's JSON lines as a table: per-frame (wait each) and throughput costs.
import sys, json
for l in sys.stdin:
    try:
        r = json.loads(l)
    except Exception:
        print(l.rstrip()[:300]); continue
    if 'base' not in r:
        print(r); continue
    t = r['through']
    print(f"{r['name']:22s} wait-each cost {r['costMs']:6.2f} | throughput base {t['base']:6.2f} with {t['withVolume']:6.2f} cost {t['cost']:6.2f}")
