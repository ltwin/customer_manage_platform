#!/usr/bin/env bash
# 复现城市数据调研（2026-10-22）。
# 约定：每个下载放进自己新建的空目录（DATA 下），脚本在本目录，中间结果在 WORK。
# 用法：DATA=/path/to/city-data WORK=/path/to/city-work bash run-all.sh
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
: "${DATA:?set DATA}" "${WORK:?set WORK}"
mkdir -p "$WORK"

# 1. 下载（各自的空目录）
NE_SHA=789c9904087846cc3361302857aa2e76b0ae71ff # natural-earth-vector v5.1.2，该文件最后一次提交
mkdir -p "$DATA/ne-10m" "$DATA/gn-15000" "$DATA/gn-5000" "$DATA/gn-altnames" "$DATA/wd-labels-15000"
curl -sSL -o "$DATA/ne-10m/ne_10m_populated_places.geojson" \
  "https://raw.githubusercontent.com/nvkelso/natural-earth-vector/$NE_SHA/geojson/ne_10m_populated_places.geojson"
curl -sS -o "$DATA/gn-15000/cities15000.zip" https://download.geonames.org/export/dump/cities15000.zip
curl -sS -o "$DATA/gn-5000/cities5000.zip" https://download.geonames.org/export/dump/cities5000.zip
curl -sS -o "$DATA/gn-altnames/alternateNamesV2.zip" https://download.geonames.org/export/dump/alternateNamesV2.zip
(cd / && unzip -o -q "$DATA/gn-15000/cities15000.zip" -d "$DATA/gn-15000/")
(cd / && unzip -o -q "$DATA/gn-5000/cities5000.zip" -d "$DATA/gn-5000/")
(cd / && unzip -o -q "$DATA/gn-altnames/alternateNamesV2.zip" -d "$DATA/gn-altnames/")

# 2. 统一记录格式
node "$HERE/ne-inspect.mjs" "$DATA/ne-10m/ne_10m_populated_places.geojson" > "$WORK/ne-inspect.txt"
node "$HERE/build-ne.mjs" "$DATA/ne-10m/ne_10m_populated_places.geojson" "$WORK/ne.json"
node "$HERE/build-gn.mjs" "$DATA/gn-15000/cities15000.txt" "$DATA/gn-altnames/alternateNamesV2.txt" "$WORK/gn15000.json" "$WORK/gn15000-stats.json"
node "$HERE/build-gn.mjs" "$DATA/gn-5000/cities5000.txt" "$DATA/gn-altnames/alternateNamesV2.txt" "$WORK/gn5000.json" "$WORK/gn5000-stats.json"

# 3. Wikidata 中文标签（CC0），按 GeoNames ID 反查，约 86 次 SPARQL 请求
node "$HERE/wd-fetch-labels.mjs" "$WORK/gn15000.json" "$DATA/wd-labels-15000" 400
node "$HERE/merge.mjs" "$WORK/gn15000.json" "$DATA/wd-labels-15000" "$WORK/gn15000-wd.json"
node "$HERE/merge.mjs" "$WORK/gn15000.json" "$DATA/wd-labels-15000" "$WORK/gn15000-wd-ne.json" "$WORK/ne.json"

# 4. 单一来源的覆盖率、时区、体积
node "$HERE/coverage.mjs" "$WORK/gn15000-wd-ne.json" "gn15000+wd+ne" "$WORK/cov-gn15000-wd-ne-all.json"
node "$HERE/coverage.mjs" "$WORK/ne.json" "ne-10m" "$WORK/cov-ne-all.json"
node "$HERE/coverage.mjs" "$WORK/gn15000.json" "gn-15000" "$WORK/cov-gn15000-all.json"
node "$HERE/coverage.mjs" "$WORK/gn5000.json" "gn-5000" "$WORK/cov-gn5000-all.json"
node "$HERE/tz-crosscheck.mjs" "$WORK/ne.json" "$WORK/gn5000.json" "$WORK/tz-crosscheck-ne-vs-gn5000.json"
(cd "$HERE/tzcheck" && GO111MODULE=off go run main.go "$WORK/cov-ne-all.tz.txt" "$WORK/cov-gn15000-all.tz.txt" "$WORK/cov-gn5000-all.tz.txt")
node "$HERE/size.mjs" "$WORK/ne.json" ne-10m "$WORK/size-ne.json" 2000,5000,all

# 5. cities5000 + Wikidata（约 175 次 SPARQL 请求），以及两种底表的分档覆盖率与体积
# CHECKLIST_MODULE 是相对 coverage.mjs 的模块路径
mkdir -p "$DATA/wd-labels-5000"
node "$HERE/wd-fetch-labels.mjs" "$WORK/gn5000.json" "$DATA/wd-labels-5000" 400
node "$HERE/merge.mjs" "$WORK/gn5000.json" "$DATA/wd-labels-5000" "$WORK/gn5000-wd.json"
for t in 2000 5000 10000 cn+2000 cn+5000 cn+5000z cn+10000 cn+99999z all; do
  node "$HERE/coverage.mjs" "$WORK/gn15000-wd.json" "gn15000+wd" "$WORK/cov-gn15000-wd-$t.json" "$t"
  node "$HERE/coverage.mjs" "$WORK/gn5000-wd.json" "gn5000+wd" "$WORK/cov-gn5000-wd-$t.json" "$t"
done
for f in gn15000-wd gn5000-wd; do
  for t in cn+5000 cn+5000z cn+10000z cn+99999z all; do
    CHECKLIST_MODULE=./checklist-extra.mjs node "$HERE/coverage.mjs" "$WORK/$f.json" "$f-extra" "$WORK/extra-$f-$t.json" "$t"
  done
done
node "$HERE/size.mjs" "$WORK/gn15000-wd.json" gn15000-wd "$WORK/size-gn15000-wd.json" 2000,5000,10000,cn+2000,cn+5000,cn+5000z,cn+10000,cn+99999z,all
node "$HERE/size.mjs" "$WORK/gn5000-wd.json" gn5000-wd "$WORK/size-gn5000-wd.json" 2000,5000,10000,cn+2000,cn+5000,cn+5000z,cn+10000,cn+99999z,all
