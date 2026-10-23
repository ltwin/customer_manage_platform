// Builds backend/internal/creativescene/testdata/moon_frontend.json (W3 tasks
// 3.1): the moon's day as the environment tab works it out (world/astro.ts
// moonTimes, astronomy-engine), for the server's moon reference (moon.go) to
// agree with. Run by hand: node scripts/night-sky/moon-times-frontend.ts
import { writeFileSync } from 'node:fs'
import { moonAt, moonTimes, skyInstant } from '../../src/creative-scene/world/astro.ts'
import { completeSky, type PhysicalSky } from '../../src/creative-scene/world/physicalSky.ts'

const places = [
  { location: 'Beijing', latitude_deg: 39.9042, longitude_deg: 116.4074, timezone: 'Asia/Shanghai' },
  { location: 'Sydney', latitude_deg: -33.8688, longitude_deg: 151.2093, timezone: 'Australia/Sydney' },
]
const cases = []
for (const { location, ...place } of places) {
  for (let day = 1; day <= 31; day++) {
    const date = `2026-10-${String(day).padStart(2, '0')}`
    const sky = completeSky({ kind: 'physical', date, local_time: '22:00', ...place }) as PhysicalSky
    const moon = moonAt(sky.latitude_deg, sky.longitude_deg, skyInstant(sky).utcMs)
    cases.push({ location, ...place, date, local_time: '22:00', elevation_deg: moon.elevationDeg, compass_deg: moon.azimuthDeg, ...moonTimes(sky) })
  }
}
writeFileSync(new URL('../../../backend/internal/creativescene/testdata/moon_frontend.json', import.meta.url), `${JSON.stringify({ source: 'frontend world/astro.ts (astronomy-engine 2.1.19)', cases }, null, 1)}\n`)
