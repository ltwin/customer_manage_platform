"""Builds scripts/fixtures/astro/skyfield-stars.json (W3 AC-W3-3).

Four bright stars of the Yale Bright Star Catalogue (BSC5, J2000 positions
and proper motions as the catalogue gives them) seen from the reference
places, worked out by Skyfield (an implementation independent of
astronomy-engine): apparent altitude and azimuth without refraction.

Run by hand in a virtualenv with skyfield installed:
    python -I scripts/night-sky/skyfield_stars.py <BSC5 catalog.gz> <directory for de421.bsp>
"""
import gzip
import json
import sys
from pathlib import Path

from skyfield.api import Loader, Star, wgs84

STARS = {424: "Polaris", 2491: "Sirius", 7001: "Vega", 4730: "Acrux"}
PLACES = {
    "Beijing": (39.9042, 116.4074),
    "Shanghai": (31.2304, 121.4737),
    "Sydney": (-33.8688, 151.2093),
}
INSTANTS = [
    ("Beijing", "2026-10-26T14:00:00Z"),
    ("Beijing", "2026-10-09T19:00:00Z"),
    ("Shanghai", "2026-10-26T12:00:00Z"),
    ("Sydney", "2026-10-18T12:00:00Z"),
    ("Sydney", "2026-10-26T15:30:00Z"),
]


def bsc5(path):
    rows = {}
    with gzip.open(path, "rt", encoding="latin-1") as f:
        for line in f:
            hr = int(line[0:4])
            if hr not in STARS:
                continue
            ra = int(line[75:77]) + int(line[77:79]) / 60 + float(line[79:83]) / 3600
            dec = int(line[84:86]) + int(line[86:88]) / 60 + int(line[88:90]) / 3600
            if line[83] == "-":
                dec = -dec
            rows[hr] = {"ra_hours": ra, "dec_degrees": dec, "pm_ra_arcsec": float(line[148:154]), "pm_dec_arcsec": float(line[154:160])}
    return rows


def main():
    catalog, cache = sys.argv[1], sys.argv[2]
    load = Loader(cache)
    ts = load.timescale()
    earth = load("de421.bsp")["earth"]
    stars = bsc5(catalog)
    points = []
    for place, iso in INSTANTS:
        lat, lon = PLACES[place]
        observer = earth + wgs84.latlon(lat, lon)
        y, mo, d = (int(v) for v in iso[:10].split("-"))
        h, mi = int(iso[11:13]), int(iso[14:16])
        t = ts.utc(y, mo, d, h, mi)
        for hr, name in STARS.items():
            s = stars[hr]
            # BSC5's proper motion in RA is in seconds of arc of a great circle; Skyfield's star takes mas/yr.
            star = Star(ra_hours=s["ra_hours"], dec_degrees=s["dec_degrees"], ra_mas_per_year=s["pm_ra_arcsec"] * 1000, dec_mas_per_year=s["pm_dec_arcsec"] * 1000)
            alt, az, _ = observer.at(t).observe(star).apparent().altaz()
            points.append({"star": name, "hr": hr, "ra_hours": s["ra_hours"], "dec_degrees": s["dec_degrees"], "location": place, "latitude": lat, "longitude": lon, "utc": iso, "elevation": round(alt.degrees, 5), "azimuth": round(az.degrees, 5)})
    out = {
        "source": "Skyfield 1.55 with JPL DE421, BSC5 (CDS V/50) J2000 positions and proper motions; apparent altitude and azimuth without refraction, observer at sea level",
        "fetched": "2026-10-23",
        "points": points,
    }
    target = Path(__file__).resolve().parent.parent / "fixtures" / "astro" / "skyfield-stars.json"
    target.write_text(json.dumps(out, indent=1, ensure_ascii=False) + "\n")


if __name__ == "__main__":
    main()
