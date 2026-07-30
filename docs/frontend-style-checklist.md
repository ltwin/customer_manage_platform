# 前端代码 Review Checklist

> 规范主体是工具链（TS strict / typescript-eslint strict / react-hooks / Prettier），本清单只列工具查不到的项。
> 决策记录：`.codestable/compound/2026-07-07-decision-frontend-toolchain-first-standard.md`。
>
> **分层原则**：① CLAUDE.md 硬规则优先级最高（第 5 节）；② 本清单是人工 review 口径；③ tsc / ESLint / Prettier 能查的不重复人查——review 前提是这些工具已通过。

## 0. 工具链前置（不通过不进人工 review）

- [ ] `tsc --noEmit` 通过（`strict: true`）
- [ ] ESLint 通过（typescript-eslint strict + react-hooks；配置在 platform-skeleton 落地，落地前此项暂记为 N/A）
- [ ] Prettier 已格式化

## 1. 类型对齐（契约先行在前端的落点）

- [ ] API 请求/响应类型一律引用 openapi-typescript 生成的 `schema.d.ts`，禁止手写重复 DTO
- [ ] 不用 `as` 强转绕过生成类型；契约类型不合用回 `cs-roadmap update` 改契约，不在前端私自变形
- [ ] `schema.d.ts` 为全量生成文件，不手工编辑；重新生成产生的跨域 diff 在 feature design 中预告（roadmap 待办已有先例）

## 2. 状态设计

- [ ] 派生数据不入 state：能由已有数据计算的（如客龄由 `created_at` 推导）在渲染期计算或 `useMemo`，不另存一份
- [ ] server state（接口数据）与 UI state（弹窗开关、表单草稿）分离，不混在同一个 state 结构里
- [ ] 状态放在最低够用的层级，不默认上提到全局（roadmap §4.7：前端无跨页共享协议约束，页面各自取数）

## 3. 组件边界

- [ ] 数据获取不下穿到展示组件：取数逻辑住页面/容器层，展示组件只收 props——类比后端「`gin.Context` 不下穿 service」
- [ ] 组件不直接拼 URL 调 fetch，统一走 API 层（platform-skeleton 落地的客户端封装）
- [ ] 跨页复用的逻辑提为 hook / 工具函数，不复制粘贴；仅单页使用的不预支抽象

## 4. 错误与加载

- [ ] 统一消费后端错误封套（platform-skeleton 定义），不在各组件散落 try/catch 各自解析错误
- [ ] 列表/详情页有明确的 loading 与 error 呈现，不静默吞错
- [ ] 两步组合流程的失败补救按 feature design 执行（如日历新建档期：订单已建、slot 失败的提示与补救，归 schedule-calendar design）

## 5. 本项目硬规则叠加（优先级最高，来源 CLAUDE.md / ADR）

- [ ] 界面文案与代码标识术语按 `.codestable/requirements/CONTEXT.md`：禁用「用户」，说「账号」（摄影师）/「客户」（拍摄对象）——前端是术语暴露面最大的一层，逐字检查文案
- [ ] 客户端永不传 `account_id`（ADR-001）：请求体/参数出现 `account_id` 即打回
- [ ] 凭证红线：任何 token / key 不进前端源码与构建产物；构建期环境变量只注入公开配置

---

**使用方式**：feature 实现后的 code review 按节走一遍，只勾工具查不到的项；发现某条口径与实际场景冲突，回 `cs-decide` 对决策做 update / supersede，不在 feature 里私自绕开。
