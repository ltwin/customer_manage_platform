package store

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql 驱动，仅迁移用
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// MigrateUp 把 schema 迁移到最新版本；带版本号可重复执行（幂等）。
// 启动序约束：必须先于 HTTP 监听完成（design 2.2）。
func MigrateUp(databaseURL string) error {
	return migrateUpFS(databaseURL, migrationsFS, "migrations", "schema_migrations")
}

// migrateUpFS 对指定迁移目录执行 up；migrationsTable 允许测试专用迁移序列
// （探针表）与生产序列使用互不冲突的版本记录表。
func migrateUpFS(databaseURL string, fsys embed.FS, dir, migrationsTable string) error {
	src, err := iofs.New(fsys, dir)
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open database for migrate: %w", err)
	}
	driver, err := migratepg.WithInstance(db, &migratepg.Config{MigrationsTable: migrationsTable})
	if err != nil {
		_ = db.Close() // 迁移失败路径，关闭错误无可操作
		return fmt.Errorf("init migrate driver: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "postgres", driver)
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("init migrate: %w", err)
	}
	defer func() {
		_, _ = m.Close() // 收尾关闭，错误无可操作
	}()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

func migrateStepsFS(databaseURL string, fsys embed.FS, dir, migrationsTable string, steps int) error {
	src, err := iofs.New(fsys, dir)
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open database for migrate: %w", err)
	}
	driver, err := migratepg.WithInstance(db, &migratepg.Config{MigrationsTable: migrationsTable})
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("init migrate driver: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "postgres", driver)
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("init migrate: %w", err)
	}
	defer func() {
		_, _ = m.Close()
	}()
	if err := m.Steps(steps); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate %d steps: %w", steps, err)
	}
	return nil
}
