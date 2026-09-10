"""在仓库标准 storetest 容器内验证设计 SQL；不连接应用数据库。"""
from pathlib import Path
import argparse
import os
import subprocess
import tempfile

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--scope', choices=['content-media', 'library-canvas', 'gateway-harness'], default='content-media')
args = parser.parse_args()
here = Path(__file__).resolve().parent
repo = here.parents[3]
backend = repo / 'backend'
# 唯一临时测试包位于 internal 内，确保可以使用标准 storetest 工具。
with tempfile.TemporaryDirectory(prefix='cmdesignprobe_', dir=backend / 'internal') as temp_dir:
    target = Path(temp_dir) / 'schema_probe_test.go'
    fixture = {'content-media': 'schema_probe_test.go', 'library-canvas': 'library_canvas_probe_test.go', 'gateway-harness': 'gateway_harness_probe_test.go'}[args.scope]
    target.write_bytes((here / fixture).read_bytes())
    subprocess.run(['gofmt', '-w', str(target)], check=True)
    env = os.environ.copy()
    env['CREATIVE_MODULE_SCHEMA'] = str(here / 'content-media-schema.sql')
    if args.scope == 'library-canvas':
        env['CREATIVE_LIBRARY_CANVAS_SCHEMA'] = str(here / 'library-canvas-schema.sql')
        env['CREATIVE_LIBRARY_QUERY'] = str(here / 'library-search.sql')
    if args.scope == 'gateway-harness':
        env['CREATIVE_GATEWAY_HARNESS_SCHEMA'] = str(here / 'gateway-harness-protocol.sql')
    relative = './internal/' + Path(temp_dir).name
    subprocess.run(['go', 'test', '-count=1', '-v', relative], cwd=backend, env=env, check=True)
