"""Eino设计适配实验：临时Go模块+标准storetest；不修改产品依赖或连接供应商。"""
from pathlib import Path
import argparse
import os
import shutil
import subprocess
import tempfile

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--refresh-lock', action='store_true', help='显式重新解析实验模块依赖并保存锁文件')
args = parser.parse_args()
env = os.environ.copy()
# 子进程测试的数据库入口只允许由父测试从storetest生成，忽略外部同名环境值。
for key in ('EINO_PROBE_CHILD', 'EINO_PROBE_DB'):
    env.pop(key, None)
here = Path(__file__).resolve().parent
root = here.parents[4]
with tempfile.TemporaryDirectory(prefix='einodesignprobe_', dir=root / 'backend/internal') as directory:
    scratch = Path(directory)
    shutil.copyfile(here / 'probe.mod', scratch / 'go.mod')
    if (here / 'probe.sum').exists():
        shutil.copyfile(here / 'probe.sum', scratch / 'go.sum')
    shutil.copyfile(here / 'adapter_probe_test.go', scratch / 'adapter_probe_test.go')
    subprocess.run(['gofmt', '-w', 'adapter_probe_test.go'], cwd=scratch, env=env, check=True)
    if args.refresh_lock:
        subprocess.run(['go', 'mod', 'tidy'], cwd=scratch, env=env, check=True)
        shutil.copyfile(scratch / 'go.mod', here / 'probe.mod')
        shutil.copyfile(scratch / 'go.sum', here / 'probe.sum')
    subprocess.run(['go', 'test', '-mod=readonly', '-count=1', '-timeout=180s', '-v', '.'], cwd=scratch, env=env, check=True)
