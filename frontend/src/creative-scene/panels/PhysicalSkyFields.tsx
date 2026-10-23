import { useMemo, useRef, useState } from 'react'
import type { Scene } from '../api.ts'
import { withCity, withPreset, withSunMode } from '../editing.ts'
import { moonIlluminated, moonPhaseName, moonTimes, physicalSun, skyInstant, sunAt, sunTimes, type MoonPhaseName, type Span } from '../world/astro.ts'
import { presetOf, type PresetName } from '../world/atmosphere/constants.ts'
import { loadCities, searchCities, type City, type CityTable } from '../world/cities.ts'
import type { NightSky, PhysicalSky } from '../world/physicalSky.ts'
import { Slider } from './EnvironmentPanel.tsx'
import { NumberField } from './Properties.tsx'

// The environment tab's physical sky (W2 design「编辑界面」): where the sun
// is (a place and a time, or manual angles), north, the atmosphere and the
// sun itself, and the night sky (W3 design「编辑界面」). The sun's and the
// moon's hours of the day are shown, never stored: the same figures the
// assistant reads (backend sun.go, moon.go).

type Environment = Scene['environment']

const presetNames: Record<PresetName, string> = { clear: '晴朗', haze: '雾霾', highland: '高原', dust: '沙尘' }

const minutesOf = (hhmm: string) => Number(hhmm.slice(0, 2)) * 60 + Number(hhmm.slice(3, 5))
const hhmmOf = (minutes: number) => `${String(Math.floor(minutes / 60)).padStart(2, '0')}:${String(minutes % 60).padStart(2, '0')}`
const degrees = (v: number) => `${v < 0 ? '−' : ''}${Math.abs(v).toFixed(1)}°`
const points = ['北', '东北', '东', '东南', '南', '西南', '西', '西北']
const bearing = (deg: number) => `${deg.toFixed(0)}°（${points[Math.round(deg / 45) % 8]}）`
const span = (s?: Span) => (s ? `${s[0]}–${s[1]}` : null)
const phaseNames: Record<MoonPhaseName, string> = {
  new: '新月',
  waxing_crescent: '蛾眉月',
  first_quarter: '上弦月',
  waxing_gibbous: '盈凸月',
  full: '满月',
  waning_gibbous: '亏凸月',
  last_quarter: '下弦月',
  waning_crescent: '残月',
}
/** 2026-10-26 as 10月26日. */
const monthDay = (date: string) => `${Number(date.slice(5, 7))}月${Number(date.slice(8, 10))}日`
const nightSwitches = [
  ['moon', '月亮'],
  ['stars', '星星'],
  ['milky_way', '银河'],
] as const

export function PhysicalSkyFields({ environment, sky, readOnly, onEnvironment }: {
  environment: Environment
  sky: PhysicalSky
  readOnly: boolean
  onEnvironment: (env: Environment, label: string, merge?: string) => void
}) {
  const setSky = (patch: Partial<PhysicalSky>, what: string, field?: string) => onEnvironment({ ...environment, sky: { ...sky, ...patch } }, `调整${what}`, field && `environment.${field}`)
  const location = sky.sun_mode === 'location'
  const where = useMemo(() => {
    const instant = skyInstant(sky)
    const compass = sunAt(sky.latitude_deg, sky.longitude_deg, instant.utcMs)
    return { ...compass, zoneKnown: instant.zoneKnown }
  }, [sky])
  // The day's hours depend on the date and the place, not the time: dragging the time does not recompute them.
  const dayKey = location ? [sky.date, sky.latitude_deg, sky.longitude_deg, sky.timezone].join('|') : ''
  const latest = useRef(sky)
  latest.current = sky
  const day = useMemo(() => (dayKey ? sunTimes(latest.current) : null), [dayKey])
  // The moon's day likewise; its phase and lit fraction follow the time.
  const moonDay = useMemo(() => (dayKey ? moonTimes(latest.current) : null), [dayKey])
  const now = skyInstant(sky).utcMs
  const setNight = (patch: Partial<NightSky>, what: string, field?: string) => setSky({ night: { ...sky.night, ...patch } }, what, field && `night.${field}`)
  const toDate = (date: string, what: string) => date !== sky.date && onEnvironment({ ...environment, sky: { ...sky, date } }, `日期改为${what}（${monthDay(date)}）`)
  const moonDate = (date: string, what: string) => (
    <button type="button" aria-label={`${what} ${date}`} onClick={() => toDate(date, what)}>
      {monthDay(date)}
    </button>
  )
  const zones = useMemo(() => {
    const all = Intl.supportedValuesOf('timeZone')
    return all.includes(sky.timezone) ? all : [sky.timezone, ...all]
  }, [sky.timezone])
  const preset = presetOf(sky)
  const toManual = () => {
    const sun = physicalSun(sky)
    onEnvironment(withSunMode(environment, 'manual', sun), '太阳改为手动角度')
  }
  return (
    <>
      <fieldset disabled={readOnly}>
        <legend>太阳位置</legend>
        <div className="cc-scene-segmented" role="group" aria-label="太阳位置">
          <button type="button" aria-pressed={location} onClick={() => !location && onEnvironment(withSunMode(environment, 'location', physicalSun(sky)), '太阳改为按地点和时间')}>
            地点和时间
          </button>
          <button type="button" aria-pressed={!location} onClick={() => location && toManual()}>
            手动角度
          </button>
        </div>
        {!location && (
          <>
            <Slider label="太阳高度（°）" value={sky.sun_elevation_deg} min={-10} max={90} step={1} onCommit={(v) => setSky({ sun_elevation_deg: v }, '太阳高度', 'sun_elevation_deg')} />
            <Slider label="太阳方位（°）" value={sky.sun_azimuth_deg} min={0} max={360} step={1} onCommit={(v) => setSky({ sun_azimuth_deg: v }, '太阳方位', 'sun_azimuth_deg')} />
          </>
        )}
      </fieldset>
      {location && (
        <>
          <fieldset disabled={readOnly}>
            <legend>地点</legend>
            <CitySearch place={sky.place} onPick={(city) => onEnvironment(withCity(environment, city), `选择城市「${city.name}」`)} />
            <NumberField label="纬度（°）" value={sky.latitude_deg} step={0.01} min={-90} max={90} onCommit={(v) => setSky({ latitude_deg: v, place: '' }, '纬度', 'latitude_deg')} />
            <NumberField label="经度（°）" value={sky.longitude_deg} step={0.01} min={-180} max={180} onCommit={(v) => setSky({ longitude_deg: v, place: '' }, '经度', 'longitude_deg')} />
            <label className="cc-scene-field">
              <span>时区</span>
              <select aria-label="时区" value={sky.timezone} onChange={(e) => setSky({ timezone: e.target.value }, '时区')}>
                {zones.map((z) => (
                  <option key={z} value={z}>
                    {z}
                  </option>
                ))}
              </select>
            </label>
          </fieldset>
          <fieldset disabled={readOnly}>
            <legend>时间</legend>
            <label className="cc-scene-field">
              <span>日期</span>
              <input type="date" aria-label="日期" min="1900-01-01" max="2100-12-31" value={sky.date} onChange={(e) => /^\d{4}-\d{2}-\d{2}$/.test(e.target.value) && setSky({ date: e.target.value }, '日期', 'date')} />
            </label>
            <div className="cc-scene-slider">
              <label className="cc-scene-field">
                <span>当地时刻 {sky.local_time}</span>
                <input type="range" aria-label="当地时刻" min={0} max={1439} step={1} value={minutesOf(sky.local_time)} onChange={(e) => setSky({ local_time: hhmmOf(Number(e.target.value)) }, '当地时刻', 'local_time')} />
              </label>
            </div>
            <div className="cc-scene-sun-now">
              <span>
                太阳高度 <output aria-label="太阳高度">{degrees(where.elevationDeg)}</output>
              </span>
              <span>
                方位 <output aria-label="太阳方位">{bearing(where.azimuthDeg)}</output>
              </span>
            </div>
            {day && (
              <section className="cc-scene-sun-day" aria-label="太阳的一天">
                <small>{day.polar === 'day' ? '极昼：太阳整天不落' : day.polar === 'night' ? '极夜：太阳整天不升' : `日出 ${day.sunrise ?? '—'} · 日落 ${day.sunset ?? '—'}`}</small>
                <small>
                  正午 {day.solarNoon}，高 {degrees(day.noonElevationDeg)}
                </small>
                {(day.morning.goldenHour || day.evening.goldenHour) && <small>黄金时刻 {[span(day.morning.goldenHour), span(day.evening.goldenHour)].filter(Boolean).join('、')}</small>}
                {(day.morning.blueHour || day.evening.blueHour) && <small>蓝调时刻 {[span(day.morning.blueHour), span(day.evening.blueHour)].filter(Boolean).join('、')}</small>}
              </section>
            )}
            {moonDay && (
              <section className="cc-scene-sun-day" aria-label="月亮的一天">
                <small>
                  <output aria-label="月相">{phaseNames[moonPhaseName(now)]}</output>，照亮 <output aria-label="月亮照亮比例">{Math.round(moonIlluminated(now) * 100)}%</output>
                </small>
                <small>
                  月出 {moonDay.moonrise ?? '—'} · 月落 {moonDay.moonset ?? '—'}
                </small>
                <small className="cc-scene-moon-dates">
                  满月 {moonDate(moonDay.previousFullMoon, '上一个满月')} {moonDate(moonDay.nextFullMoon, '下一个满月')}
                </small>
                <small className="cc-scene-moon-dates">
                  新月 {moonDate(moonDay.previousNewMoon, '上一个新月')} {moonDate(moonDay.nextNewMoon, '下一个新月')}
                </small>
              </section>
            )}
            {!where.zoneKnown && <small>浏览器不认识时区「{sky.timezone}」，时刻按经度推算（UTC{sky.longitude_deg >= 0 ? '+' : '−'}{Math.abs(Math.round(sky.longitude_deg / 15))}）</small>}
          </fieldset>
          <fieldset disabled={readOnly}>
            <legend>方向</legend>
            <Slider label="正北方向（°）" value={sky.north_azimuth_deg} min={0} max={360} step={1} onCommit={(v) => setSky({ north_azimuth_deg: v }, '正北方向', 'north_azimuth_deg')} />
            <small>场景里正北所在的方位：太阳的方位从这里量起（默认 180°，即 −Z 方向为北）</small>
          </fieldset>
        </>
      )}
      <fieldset disabled={readOnly}>
        <legend>大气</legend>
        <label className="cc-scene-field">
          <span>预设</span>
          <select
            aria-label="大气预设"
            value={preset ?? 'custom'}
            onChange={(e) => {
              const name = e.target.value
              if (name in presetNames) onEnvironment(withPreset(environment, name as PresetName), `大气改为${presetNames[name as PresetName]}`)
            }}
          >
            {(Object.keys(presetNames) as PresetName[]).map((p) => (
              <option key={p} value={p}>
                {presetNames[p]}
              </option>
            ))}
            <option value="custom" disabled>
              自定义
            </option>
          </select>
        </label>
        <NumberField label="海拔（m）" value={sky.altitude_m} step={100} min={0} max={20000} onCommit={(v) => setSky({ altitude_m: v }, '海拔', 'altitude_m')} />
        <NumberField label="空气密度" value={sky.air_density} step={0.1} min={0} max={10} onCommit={(v) => setSky({ air_density: v }, '空气密度', 'air_density')} />
        <NumberField label="尘埃密度" value={sky.dust_density} step={0.5} min={0} max={50} onCommit={(v) => setSky({ dust_density: v }, '尘埃密度', 'dust_density')} />
        <NumberField label="臭氧" value={sky.ozone_density} step={0.1} min={0} max={10} onCommit={(v) => setSky({ ozone_density: v }, '臭氧', 'ozone_density')} />
      </fieldset>
      <fieldset disabled={readOnly}>
        <legend>太阳</legend>
        <NumberField label="太阳亮度" value={sky.sun_strength} step={0.1} min={0} max={10} onCommit={(v) => setSky({ sun_strength: v }, '太阳亮度', 'sun_strength')} />
        <Slider label="色温（K）" value={sky.sun_temperature_k} min={2000} max={12000} step={100} onCommit={(v) => setSky({ sun_temperature_k: v }, '太阳色温', 'sun_temperature_k')} />
        <label className="cc-scene-field">
          <span>太阳盘</span>
          <input type="checkbox" aria-label="太阳盘" checked={sky.sun_disc} onChange={(e) => setSky({ sun_disc: e.target.checked }, e.target.checked ? '显示太阳盘' : '隐藏太阳盘')} />
        </label>
        <NumberField label="太阳盘大小（°）" value={sky.sun_size_deg} step={0.05} min={0.1} max={10} onCommit={(v) => setSky({ sun_size_deg: v }, '太阳盘大小', 'sun_size_deg')} />
      </fieldset>
      <fieldset disabled={readOnly} aria-label="夜空">
        <legend>夜空</legend>
        {!location && <small>月亮和星空需要日期和地点，只在「地点和时间」下生效；夜色照常生效</small>}
        {nightSwitches.map(([key, label]) => (
          <label key={key} className="cc-scene-field">
            <span>{label}</span>
            <input type="checkbox" aria-label={label} checked={sky.night[key]} disabled={!location} onChange={(e) => setNight({ [key]: e.target.checked }, `${e.target.checked ? '显示' : '隐藏'}${label}`)} />
          </label>
        ))}
        {location && sky.night.stars && <small>星星包括无月晴夜天空本身的微光（夜天光）</small>}
        <NumberField label="月亮亮度" value={sky.night.moon_strength} step={0.1} min={0} max={10} disabled={!location} onCommit={(v) => setNight({ moon_strength: v }, '月亮亮度', 'moon_strength')} />
        <Slider label="月亮色温（K）" value={sky.night.moon_temperature_k} min={2000} max={12000} step={100} disabled={!location} onCommit={(v) => setNight({ moon_temperature_k: v }, '月亮色温', 'moon_temperature_k')} />
        <NumberField label="星空亮度" value={sky.night.stars_strength} step={0.1} min={0} max={10} disabled={!location} onCommit={(v) => setNight({ stars_strength: v }, '星空亮度', 'stars_strength')} />
        <Slider label="星空色温（K）" value={sky.night.stars_temperature_k} min={2000} max={12000} step={100} disabled={!location} onCommit={(v) => setNight({ stars_temperature_k: v }, '星空色温', 'stars_temperature_k')} />
        <Slider label="夜色" value={sky.night.night_look} min={0} max={1} step={0.05} onCommit={(v) => setNight({ night_look: v }, '夜色', 'night_look')} />
        <small>夜色：入夜后画面偏蓝、降饱和，0 为物理计算的颜色</small>
        <small>星表：Yale 亮星星表（Hoffleit &amp; Warren 1991，经 CDS/VizieR）；月面：NASA SVS</small>
      </fieldset>
    </>
  )
}

/**
 * The city search: the table loads the first time the box is focused, then
 * each keystroke lists up to 8 cities; GeoNames is credited at the foot of
 * the list (CC BY 4.0).
 */
function CitySearch({ place, onPick }: { place: string; onPick: (city: City) => void }) {
  const [table, setTable] = useState<CityTable | null>(null)
  const [query, setQuery] = useState('')
  const [open, setOpen] = useState(false)
  const [failed, setFailed] = useState(false)
  const closing = useRef<number | null>(null)
  const results = useMemo(() => (table ? searchCities(table, query) : []), [table, query])
  const load = () => {
    if (table) return
    loadCities().then(setTable, () => setFailed(true))
  }
  return (
    <div className="cc-scene-field cc-scene-city">
      <span>城市</span>
      <input
        role="combobox"
        aria-label="城市"
        aria-expanded={open && query.trim() !== ''}
        aria-controls="cc-scene-city-list"
        aria-autocomplete="list"
        placeholder={place || '搜索城市'}
        value={query}
        onFocus={() => {
          load()
          setOpen(true)
        }}
        onBlur={() => {
          // A click on an option lands after the blur.
          closing.current = window.setTimeout(() => setOpen(false), 150)
        }}
        onChange={(e) => {
          setQuery(e.target.value)
          setOpen(true)
        }}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && results[0]) {
            onPick(results[0])
            setQuery('')
            setOpen(false)
          }
          if (e.key === 'Escape') setOpen(false)
        }}
      />
      {open && query.trim() !== '' && (
        <div className="cc-scene-city-menu">
          <ul id="cc-scene-city-list" role="listbox" aria-label="城市">
            {results.map((c) => (
              <li
                key={`${c.label}|${c.latitude}|${c.longitude}`}
                role="option"
                aria-selected={false}
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => {
                  onPick(c)
                  setQuery('')
                  setOpen(false)
                }}
              >
                {c.label}
              </li>
            ))}
          </ul>
          {table && results.length === 0 && <small>没找到？直接填经纬度</small>}
          {!table && <small>{failed ? '城市表加载失败，可以直接填经纬度' : '正在加载城市表…'}</small>}
          {table && (
            <small className="cc-scene-city-credit">
              城市数据：
              <a href={table.attribution.source} target="_blank" rel="noreferrer">
                GeoNames（CC BY 4.0），已修改
              </a>
              <a href={table.attribution.license} target="_blank" rel="noreferrer" aria-label="CC BY 4.0 许可证">
                许可证
              </a>
            </small>
          )}
        </div>
      )}
    </div>
  )
}
