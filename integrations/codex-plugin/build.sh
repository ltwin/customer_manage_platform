#!/usr/bin/env bash
# 生成 Codex 插件的发布目录：只发一个指向生产地址的包，不按环境拆包。
# 用法：integrations/codex-plugin/build.sh https://<生产域名> <输出目录>
set -euo pipefail

base="${1:-}"
out="${2:-}"
if [[ ! "$base" =~ ^https://[^/]+$ || -z "$out" ]]; then
  echo "usage: $0 https://<host> <out-dir>   (base must be an https origin without a path)" >&2
  exit 2
fi
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
mkdir -p "$out"
cp -R "$here/template/." "$out/"
tmpl="$out/plugins/creative/.mcp.json.tmpl"
sed "s#{{MCP_URL}}#${base}/mcp#" "$tmpl" > "$out/plugins/creative/.mcp.json"
rm "$tmpl"
echo "codex plugin written to $out (MCP ${base}/mcp)"
