# S0 原型（存档）

S0 的穿刺页面、参考解和测量脚本，S0 结束时从 `frontend/` 原样移到这里（任务 5.2），不进产品构建，也不再随产品代码更新。

## 重新运行

代码里的导入按原位置写（`../../src/...`），所以要先放回原处：

```sh
cp -R <本目录>/spike-light.html <本目录>/spike frontend/
cd frontend && npx vite --port 5193 --strictPort
```

打开 `http://localhost:5193/spike-light.html`，参数见 `spike/light/main.tsx` 开头的注释。产品代码已经变化时可能需要小改才能编译。用完删掉 `frontend/spike-light.html` 和 `frontend/spike/`，不要提交。
