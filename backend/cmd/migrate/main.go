// Command migrate 是 schema 迁移的手动入口（CMD-003）：对 DATABASE_URL 指向的库执行 migrate up。
// 服务进程启动时也会自动执行同一迁移（见 cmd/server）。
package main

import (
	"log"
	"os"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL 未设置")
	}
	if err := store.MigrateUp(databaseURL); err != nil {
		log.Fatalf("migrate up 失败: %v", err)
	}
	log.Println("migrate up 完成")
}
