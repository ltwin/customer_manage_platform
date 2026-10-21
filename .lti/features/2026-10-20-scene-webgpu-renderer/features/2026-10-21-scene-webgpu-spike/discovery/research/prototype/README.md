# R0 穿刺原型

R0 期间放在 `frontend/` 下运行，任务 5.2 整体移到这里存档，不参与构建和检查。

| 原位置 | 现位置 |
|---|---|
| `frontend/spike-webgpu.html` | `spike-webgpu.html` |
| `frontend/spike/webgpu/*.ts(x)`、`*.js` | `webgpu/` |
| `frontend/spike/webgpu/measure/*.mjs` | `measure/` |

各组的研究文档引用的 `prototype/webgpu/…`、`prototype/measure/…` 就是这里。

## 重新运行

代码按原位置写死了相对路径（`../../src/creative-scene/…`，页面引用 `/spike/webgpu/main.tsx`），要先放回原位置：

```sh
# 在仓库根目录
mkdir -p frontend/spike
cp -R <本目录>/webgpu frontend/spike/webgpu
cp -R <本目录>/measure frontend/spike/webgpu/measure
cp <本目录>/spike-webgpu.html frontend/
cd frontend && npm run dev -- --port 5291 --strictPort
# 另开终端，在 frontend 下按各脚本开头的注释运行，例如：
SPIKE_URL=http://localhost:5291 node spike/webgpu/measure/hires.mjs <输出目录>
```

页面按 `?case=` 分发到各组：`viewport`、`shooter`、`materials`、`dof`、`ao`、`sky`、`shadow`、`outline`、`gbuffer`、`gi`、`hires`、`readback`、`parity`。`?backend=webgl` 强制使用 three 的 WebGL2 后端。代码基于 `develop` 9bc7c651 和 three 0.186.1；之后的版本不保证能直接运行。用完删掉放回的文件，不要提交。
