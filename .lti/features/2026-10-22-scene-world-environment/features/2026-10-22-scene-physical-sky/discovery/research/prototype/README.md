# W2 穿刺原型

W2 设计批准前的穿刺代码（`atmosphere-luts.md`、`exposure.md`）。原型在会话临时目录里运行，没有放进 `frontend/`，不参与构建和检查。

| 文件 | 内容 |
|---|---|
| `src/atmo.ts` | 大气常量与参数换算。`mieScale`、`mieAbsorptionTint` 是穿刺用的试验参数 |
| `src/atmoCpu.ts` | Hillaire 2020 的 CPU 双精度实现，离散方式与 GPU 相同；太阳透射率、入射式测光的天空照度 |
| `src/atmoGpu.ts` | 透射率、多次散射、天空视图三张 LUT 的 TSL 片元 pass；物理天空背景（产品视口的 Z 向上约定） |
| `src/scene.ts` | 院子和室内两个场景；物理天空的预曝光链路；直接引用产品的 `render/sky.ts`、`render/litLook.ts` 画程序化天空作对照；GPU 反射式测光；材质输出钳制 |
| `src/main.ts` | 页面入口：LUT 精度、耗时 |
| `measure/run.mjs` | 跑单个用例并打印 JSON |
| `measure/shots.mjs` | 跑 `lists/*.json` 里的一组画面，存 PNG 和统计 |
| `measure/cpu-check.ts` | 在 Node 里打印各太阳高度的照度与测光值 |
| `py/compare.py` | 逐像素比较三种后端的 PNG |

## 重新运行

基于 `develop` 6254ebdd 和 three 0.186.1。

```sh
S=<任意空目录>
cp -R <本目录>/* $S/
ln -s <仓库>/frontend/node_modules $S/node_modules
# scene.ts 用 /@fs/ 绝对路径引用产品代码，换机器时改成本机的仓库路径
cd $S && node_modules/.bin/vite --config vite.config.mjs --port 5292 --strictPort &
node measure/run.mjs luts '{"half":true,"sunElevDeg":30}' webgpu        # 后端：webgpu | webgl | swiftshader
node measure/shots.mjs lists/backends.json out/backends swiftshader
python3 -I py/compare.py out/backends x-noon,x-golden
MIE_SCALE=5 node measure/cpu-check.ts
```

`vite.config.mjs` 把缓存放在原型目录自己的 `.vite-cache`，不写进 `frontend/node_modules/.vite`。
