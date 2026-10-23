# W5 穿刺原型

W5 起草前的穿刺代码（`volume-lighting.md`）。原型在会话临时目录里运行，没有放进 `frontend/`，不参与构建和检查。

| 文件 | 内容 |
|---|---|
| `src/room.ts` | 测试场景：开东窗、带十字窗棂的 6 × 6 × 3 m 屋子，院子；太阳（直接引用产品的 `render/sunShadow.ts` 级联阴影）、聚光、点光、半球光；体积的几组摆法 |
| `src/vol.ts` | 体积光照：两种做法共用的光照求值（阴影贴图、相函数、自遮挡、环境光），做法 A（逐像素步进、深度加权模糊与上采样），做法 B（froxel 图集） |
| `src/main.ts` | 页面入口：搭场景、出图、计时（逐帧等待与连续提交两种） |
| `measure/run.mjs` | 跑 `lists/*.json` 里的一组用例，打印 JSON，存 PNG |
| `measure/table.py` | 把 `run.mjs` 的输出排成表 |
| `measure/compare.py` | 逐像素比较两种后端的 PNG |

## 重新运行

基于 `develop` b52806bf 和 three 0.186.1。

```sh
S=<任意空目录>
cp -R <本目录>/* $S/
ln -s <仓库>/frontend/node_modules $S/node_modules
# room.ts 用 /@fs/ 绝对路径引用产品代码，换机器时改成本机的仓库路径
cd $S && node_modules/.bin/vite --config vite.config.mjs --port 5294 --strictPort &
node measure/run.mjs @lists/timing.json webgpu out/timing | python3 -I measure/table.py   # 后端：webgpu | webgl | swiftshader
node measure/run.mjs @lists/shots.json webgl out/shots
python3 -I measure/compare.py out/shots webgpu webgl window-A spot-A
```

`vite.config.mjs` 把缓存放在原型目录自己的 `.vite-cache`，不写进 `frontend/node_modules/.vite`。
