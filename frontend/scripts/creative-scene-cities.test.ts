import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { searchCities, type CityTable } from '../src/creative-scene/world/cities.ts'

// W2 AC-W2-12: the physical sky's city search (design「城市数据」).

const table = JSON.parse(readFileSync(new URL('../src/creative-scene/world/cities.json', import.meta.url), 'utf8')) as CityTable
const first = (q: string) => searchCities(table, q)[0]

test('a city comes first by its Chinese name, its English or pinyin name, and the places the patches added', () => {
  for (const q of ['成都', 'Chengdu', 'chengdu', '稻城', '婺源', '漠河', '林芝']) assert.ok(first(q), q)
  assert.equal(first('成都').name, '成都')
  assert.equal(first('Chengdu').name, '成都')
  assert.equal(first('chengdu').name, '成都')
  assert.equal(first('稻城').name, '稻城')
  assert.equal(first('婺源').name, '婺源')
  assert.equal(first('漠河').timezone, 'Asia/Shanghai')
  assert.deepEqual([first('成都').latitude, first('成都').longitude], [30.67, 104.07])
})

test('a prefix comes before a match inside a name: 京都 before 东京都, 喀什 before 马拉喀什', () => {
  assert.equal(first('京都').en, 'Kyoto')
  assert.equal(first('喀什').name, '喀什')
  assert.ok(searchCities(table, '京都').some((c) => c.name === '东京都'), 'the inside match is still there, after')
})

test('cities of one name are told apart by country', () => {
  const london = searchCities(table, '伦敦').filter((c) => c.name === '伦敦')
  assert.deepEqual(
    london.map((c) => c.label),
    ['伦敦 · 英国', '伦敦 · 加拿大'],
  )
  assert.equal(first('香港').label, '香港 · 中国香港')
  assert.equal(first('臺北').name, '台北', 'traditional characters find the place')
})

test('Xinjiang is on Beijing time; at most 8 results; nothing for nothing', () => {
  for (const q of ['乌鲁木齐', '喀什']) assert.equal(first(q).timezone, 'Asia/Shanghai', q)
  assert.equal(searchCities(table, '州').length, 8)
  assert.deepEqual(searchCities(table, '  '), [])
  assert.deepEqual(searchCities(table, 'zzzzqx'), [])
})

test('the table credits GeoNames and says when it was taken', () => {
  assert.match(table.attribution.text, /GeoNames（CC BY 4\.0），已修改/)
  assert.match(table.attribution.fetched, /^\d{4}-\d{2}-\d{2}$/)
})
