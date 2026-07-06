// Package webui 把前端构建产物 go:embed 进二进制（design D7）：
// 编译期恒定行为，无运行时模式开关；dev 期前端流量走 Vite dev server 不经这里。
// dist/ 内容由 `make build`（webui-sync）从 frontend/dist 同步，不入 git（.gitkeep 除外）。
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Dist 返回静态产物文件系统（根 = 构建产物根）。
func Dist() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// dist 目录由 go:embed 保证存在，此处不可达
		panic(err)
	}
	return sub
}
