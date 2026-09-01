// Package storetest 为集成测试提供共享的 PostgreSQL 容器基座。
//
// 改造前每个测试各起一个容器：单包 32 个测试就是 32 次容器启动，每次约 2.5s。
// 慢只是表象，真正的代价是 flake —— 每次启动都要抢一次端口映射，
// 全量门禁 100+ 次启动时 `port "5432/tcp" not found` 几乎必然出现，
// 门禁因此退化成「每次都要人工分诊」。
//
// 现在每个测试二进制（= 每个包）共享一个容器，测试之间靠独立 database 隔离：
//
//	容器启动   2.5s   每包一次
//	空库       ~35ms  CREATE DATABASE
//	已迁移库   ~37ms  CREATE DATABASE ... TEMPLATE（模板在 Main 里建一次，~290ms）
//
// 用法：包里加一个 TestMain，helper 改为向本包取库。
//
//	func TestMain(m *testing.M) { storetest.Main(m, store.MigrateUp) }
//
// 隔离性没有下降：每个测试仍拿到自己的 database，跨测试的写入互不可见。
package storetest

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql 驱动，建库用
)

const (
	dbUser     = "crm_test"
	dbPassword = "crm_test"
	dbName     = "crm_test"
	// templateDB 是跑完全量迁移的模板库，NewURL 从它克隆。
	// 克隆要求模板上没有活动连接，所以除了 Main 里建模板那一次，
	// 任何代码都不得连接它。
	templateDB = "crm_test_template"
)

var (
	shared    *tcpostgres.PostgresContainer
	adminDB   *sql.DB
	host      string
	port      string
	dbCounter atomic.Uint64
)

// Main 起容器、建模板库、跑完本包全部测试后销毁容器。
// 各包的 TestMain 直接调用它即可：
//
//	func TestMain(m *testing.M) { storetest.Main(m, store.MigrateUp) }
//
// migrateUp 由调用方注入而不是本包直接 import store：store 包自己的内部测试文件
// （package store）也要取库，本包若依赖 store 就成了导入环。
//
// 不返回退出码而是自己 os.Exit：defer 在 os.Exit 下不执行，
// 容器清理必须在退出前显式做完。
func Main(m *testing.M, migrateUp func(databaseURL string) error) {
	code, err := run(m, migrateUp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "storetest: %v\n", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func run(m *testing.M, migrateUp func(databaseURL string) error) (int, error) {
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase(dbName),
		tcpostgres.WithUsername(dbUser),
		tcpostgres.WithPassword(dbPassword),
		testcontainers.WithWaitStrategy(waitReady()),
	)
	if err != nil {
		return 0, fmt.Errorf("start postgres container: %w", err)
	}
	shared = ctr
	defer func() {
		if err := testcontainers.TerminateContainer(ctr); err != nil {
			fmt.Fprintf(os.Stderr, "storetest: terminate container: %v\n", err)
		}
	}()

	if host, err = ctr.Host(ctx); err != nil {
		return 0, fmt.Errorf("container host: %w", err)
	}
	mapped, err := ctr.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return 0, fmt.Errorf("container mapped port: %w", err)
	}
	port = mapped.Port()

	if adminDB, err = sql.Open("pgx", URLFor(dbName)); err != nil {
		return 0, fmt.Errorf("open admin connection: %w", err)
	}
	defer func() { _ = adminDB.Close() }()
	if err := adminDB.PingContext(ctx); err != nil {
		return 0, fmt.Errorf("ping admin connection: %w", err)
	}

	if err := buildTemplate(ctx, migrateUp); err != nil {
		return 0, err
	}
	return m.Run(), nil
}

// buildTemplate 建一次模板库并跑全量迁移。migrateUp 必须在返回前关掉自己的连接，
// 这是 NewURL 能克隆的前提 —— 模板上留着活动连接会让克隆报 SQLSTATE 55006。
func buildTemplate(ctx context.Context, migrateUp func(string) error) error {
	if _, err := adminDB.ExecContext(ctx, "CREATE DATABASE "+templateDB); err != nil {
		return fmt.Errorf("create template database: %w", err)
	}
	if err := migrateUp(URLFor(templateDB)); err != nil {
		return fmt.Errorf("migrate template database: %w", err)
	}
	return nil
}

// waitReady 等两件事，缺一不可：
//   - 日志出现两次「ready to accept connections」（第一次是 initdb 阶段的临时实例）
//   - 端口映射真的可连
//
// 只等日志是这个仓库长期 flake 的真正根因：日志行出现时 Docker 可能还没发布端口
// 映射，紧接着的 MappedPort 就报 `port "5432/tcp" not found`。串行跑碰不到，
// 是因为串行根本没有并发启动；-p>1 时多个容器同时起，几乎每轮必现。
func waitReady() wait.Strategy {
	return wait.ForAll(
		wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		wait.ForListeningPort("5432/tcp"),
	).WithStartupTimeoutDefault(60 * time.Second)
}

// URLFor 拼出共享容器上某个 database 的连接串。
func URLFor(database string) string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		dbUser, dbPassword, host, port, database)
}

// Container 返回本包共享的容器句柄，供需要在容器内执行 psql 的测试使用
// （pgx 直连被 depguard 限制在 store 系包内，ADR-001 基座守护）。
func Container() *tcpostgres.PostgresContainer {
	return shared
}

// NewURL 从模板库克隆一个已跑完迁移的独立 database，返回连接串。
// 给需要「一个可用的库」的测试用 —— 绝大多数集成测试属于这类。
func NewURL(t *testing.T) string {
	t.Helper()
	_, url := NewDatabase(t)
	return url
}

// NewDatabase 同 NewURL，但同时返回库名，供需要在容器内用 psql 直连的测试使用。
func NewDatabase(t *testing.T) (name, url string) {
	t.Helper()
	return newDatabase(t, "CREATE DATABASE %s TEMPLATE "+templateDB)
}

// NewRawURL 建一个空 database，不跑任何迁移。
// 给自己驱动迁移的测试用（MigrateStepsForTest 那一类要从零推进到指定步数）。
func NewRawURL(t *testing.T) string {
	t.Helper()
	_, url := newDatabase(t, "CREATE DATABASE %s")
	return url
}

// NewDedicated 为少数与共享容器不兼容的测试起一个专属容器。
//
// 目前只有一个真实用例：dataexport 的批量读证明要开 log_statement=all 并断言
// 整份容器日志里某条语句恰好出现一次 —— 它同时要求特殊启动参数和日志隔离，
// 共享容器两条都给不了。
//
// 用之前先确认真的不能共享：每次调用都是一次约 2.5s 的容器启动，
// 以及一次 `port "5432/tcp" not found` 的竞态机会。
func NewDedicated(t *testing.T, migrateUp func(string) error, opts ...testcontainers.ContainerCustomizer) (string, *tcpostgres.PostgresContainer) {
	t.Helper()
	ctx := context.Background()
	args := []testcontainers.ContainerCustomizer{
		tcpostgres.WithDatabase(dbName),
		tcpostgres.WithUsername(dbUser),
		tcpostgres.WithPassword(dbPassword),
		testcontainers.WithWaitStrategy(waitReady()),
	}
	ctr, err := tcpostgres.Run(ctx, "postgres:17-alpine", append(args, opts...)...)
	if err != nil {
		t.Fatalf("start dedicated postgres container: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(ctr) })
	url, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("dedicated container connection string: %v", err)
	}
	if err := migrateUp(url); err != nil {
		t.Fatalf("migrate dedicated database: %v", err)
	}
	return url, ctr
}

// ExecSQL 在共享容器内用 psql 对指定库执行语句。
// 业务包的测试要注入 DDL 只能走这条路 —— pgx 直连被 depguard 限制在 store 系包内
// （ADR-001 账号隔离基座守护）。
func ExecSQL(t *testing.T, database, statement string) {
	t.Helper()
	code, output, err := shared.Exec(context.Background(),
		[]string{"psql", "-U", dbUser, "-d", database, "-c", statement})
	if err != nil || code != 0 {
		t.Fatalf("exec %q on %s: code=%d err=%v output=%v", statement, database, code, err, output)
	}
}

func newDatabase(t *testing.T, stmtFmt string) (name, url string) {
	t.Helper()
	if adminDB == nil {
		t.Fatal("storetest: 共享容器未初始化，包里缺 " +
			"func TestMain(m *testing.M) { storetest.Main(m, store.MigrateUp) }")
	}
	name = fmt.Sprintf("crm_test_%d", dbCounter.Add(1))
	if _, err := adminDB.ExecContext(context.Background(), fmt.Sprintf(stmtFmt, name)); err != nil {
		t.Fatalf("create test database %s: %v", name, err)
	}
	// 不 DROP：容器随包退出一起销毁，省掉每个测试一次 DROP 的往返，
	// 也避开测试残留连接导致 DROP 失败（"being accessed by other users"）。
	return name, URLFor(name)
}
