# Go 后端代码 Review Checklist

> 规范源：[Uber Go Style Guide 中文版](https://github.com/xxjwxc/uber_go_guide_cn)（歧义时以[英文原版](https://github.com/uber-go/guide)为准）。
> 决策记录：`.codestable/compound/2026-07-06-decision-go-uber-style-guide.md`。
>
> **分层原则**：① CLAUDE.md 硬规则优先级最高（第 7 节）；② 本清单是人工 review 口径，只列工具查不到或查不全的项；③ gofmt / goimports / go vet / golangci-lint 能查的不重复人查——review 前提是这些工具已通过。

## 0. 工具链前置（不通过不进人工 review）

- [ ] gofmt / goimports 已格式化，import 分组（标准库 / 第三方）正确
- [ ] `go vet` 无告警
- [ ] golangci-lint 通过（配置在后端工程初始化 feature 落地；落地前此项暂记为 N/A）

## 1. 错误处理（指导原则 · Errors）

- [ ] 错误只处理一次：要么处理、要么返回，不允许 log 完再原样 return
- [ ] 需要调用方匹配的包装用 `fmt.Errorf(... %w)`，刻意隐藏底层错误才用 `%v`；包装信息不堆叠 "failed to" 类冗余前缀
- [ ] 静态错误定义为包级 `var ErrXxx = errors.New(...)`；需要携带数据的用自定义 `XxxError` 类型
- [ ] 错误匹配用 `errors.Is` / `errors.As`，禁止字符串比较
- [ ] 类型断言一律双返回值 `v, ok := i.(T)`
- [ ] 业务代码不 panic；不可恢复的启动期错误只允许在 `main` 里 `log.Fatal`（且程序只从 main 退出一次）
- [ ] 不忽略错误返回值；确需忽略的 `_ =` 必须有注释说明为何安全

## 2. 并发（指导原则 · goroutine / 同步原语）

- [ ] 每个 goroutine 生命周期可控：明确它何时退出、如何被通知退出（context / channel），可等待其结束，无泄漏
- [ ] 不 fire-and-forget：后台任务（如提醒扫描 worker）由 main 统一编排启停，不在 service / repository 里随手 `go func`
- [ ] channel 只用无缓冲或 size 1，更大 buffer 必须有理由并写明
- [ ] Mutex 零值直接用；结构体内用私有字段 `mu sync.Mutex`，不嵌入导出
- [ ] slice / map 进出结构体边界时拷贝，防止外部持有引用篡改内部状态
- [ ] 原子操作用类型化 atomic（`sync/atomic` 的类型或 `go.uber.org/atomic`），不裸用整型加锁混搭

## 3. 类型、接口与包设计（指导原则）

- [ ] 接口定义在消费方：repository 接口住 domain / service 侧，不在实现侧（与 ADR-003 一致）
- [ ] 导出类型实现导出接口时加编译期校验 `var _ Iface = (*Impl)(nil)`
- [ ] 同一类型的 receiver 值 / 指针不混用；含锁或需修改状态的用指针
- [ ] 不在公开结构体中嵌入类型（泄露实现细节、破坏封装）
- [ ] 避免 `init()`；初始化显式发生在 main 或构造函数
- [ ] 避免可变全局变量，用依赖注入（时间相关逻辑注入 now/clock，便于提醒规则测试）
- [ ] 枚举从 1 开始（除非零值有明确语义）
- [ ] 时刻用 `time.Time`、时段用 `time.Duration`，不用裸 int 传秒 / 毫秒
- [ ] 序列化结构体的字段都有显式 tag（json 等）

## 4. 风格与可读性（规范）

- [ ] 包名小写、无下划线、无复数，不叫 util / common / shared
- [ ] 减少嵌套：错误与边界 case 早返回，消除不必要的 else
- [ ] 变量作用域最小化，`if err := f(); err != nil` 内联写法
- [ ] 结构体初始化必须带字段名，零值字段省略；零值结构体用 `var`，取引用用 `&T{}`
- [ ] 裸参数（`true` / 魔法数字直接入参）加 `/* name */` 注释或改用自定义类型
- [ ] 相似声明分组（const / var / type 块）；本地变量在最接近使用处声明
- [ ] 返回空集合时利用 nil 是有效 slice，不强造 `[]T{}`
- [ ] Printf 风格的格式串尽量提为 const；此类函数命名以 `f` 结尾

## 5. 性能（性能）

- [ ] 基本类型与字符串互转用 strconv，不用 fmt.Sprint
- [ ] 热路径避免重复 string ↔ []byte 转换
- [ ] 能预估容量的 make 指定 cap / hint（slice、map）

## 6. 测试（模式）

- [ ] 用例 ≥ 2 的测试写成表驱动，子测试命名可读
- [ ] 需要多可选配置的构造器用 functional options，不铺参数列表

## 7. 本项目硬规则叠加（优先级最高，来源 CLAUDE.md / ADR）

- [ ] 业务表查询全部限定 `account_id`，过滤在 repository 基座强制，客户端永不传（ADR-001）
- [ ] `gin.Context` 不下穿 service / repository；领域逻辑不 import 路由框架（ADR-003）
- [ ] 凭证只经环境变量注入，不入库、不入 git
- [ ] 术语按 `.codestable/requirements/CONTEXT.md`：禁用「用户」，说「账号」/「客户」

---

**使用方式**：feature 实现后的 code review 按节走一遍，只勾工具查不到的项；发现某条规范与实际场景冲突，回 `cs-decide` 对决策做 update / supersede，不在 feature 里私自绕开。
