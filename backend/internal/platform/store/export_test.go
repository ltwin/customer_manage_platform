package store

import "embed"

// 测试专用探针表迁移：经同一迁移机制建于测试库，但只编译进测试二进制，
// 不进入生产迁移序列（design D5）；版本记录表独立避免与生产序列冲突。
//
//go:embed testmigrations/*.sql
var probeMigrationsFS embed.FS

// MigrateProbeUpForTest 在测试库上建探针表（仅测试可达：定义于 _test 文件）。
func MigrateProbeUpForTest(databaseURL string) error {
	return migrateUpFS(databaseURL, probeMigrationsFS, "testmigrations", "schema_migrations_probe")
}

// MigrateDownOneForTest 回滚生产迁移序列的最后一步，仅供 migration up/down 测试。
func MigrateDownOneForTest(databaseURL string) error {
	return migrateStepsFS(databaseURL, migrationsFS, "migrations", "schema_migrations", -1)
}
