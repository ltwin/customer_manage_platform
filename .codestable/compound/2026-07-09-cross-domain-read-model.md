# 跨域聚合读模型放在展示域仓库内

## 背景

`order-tracking` 首次让一个域展示另一个域的数据：客户列表 / 详情要展示订单数、最近拍摄日期、订单总额；套系列表要展示历史约单数；套系删除还要查是否被任一订单引用。这里很容易误以为应该让 `order` 域暴露一个 Go 接口，再让 `customer` / `package` service 注入调用。

但这个项目是单体同库，roadmap 已经把这些聚合定义为同进程读模型。抽域间 Go 接口会制造一个看起来很“干净”、实际只转发 SQL 读数的假 seam，还会把账号隔离和聚合口径分散到多个服务边界里。

## 结论

跨域聚合读模型默认落在“展示该读数的域”的 repository 内：

- customer 页面要展示订单聚合，就在 `customer` repository 内按账号查 `orders` 表；
- package 页面要展示订单聚合，就在 `package` repository 内按账号查 `orders` 表；
- 不为这种同库读模型抽 `order` 域 Go 接口，不做 service-to-service 假依赖；
- 所有聚合查询仍必须经过 `AccountScope`，需要 `max` / `sum` / `count` 等能力时，显式扩展 `AccountScope` 的受控 API 和测试，不绕开 scope 拼裸 SQL；
- 列表摘要可以页内批量查或 join，但禁止逐行 N+1。

这条规则适用于后续 schedule / reminder / dashboard 这类“在一个页面展示多个域的只读摘要”的场景。若未来出现跨域写入、外部系统边界、异步一致性或可独立部署需求，再重新评估 seam。

## 证据

- 设计拍板：`.codestable/features/2026-07-08-order-tracking/order-tracking-design.md` D3 / D11 / §2.5 明确“各域 repository 内查外域表 + AccountScope 受控聚合方法，不抽域间 Go 接口”。
- 实现落点：
  - `backend/internal/customer/repository.go`：`orderStatsForCustomer` 用 `AccountScope.ScalarAggregate` 计算 customer 侧订单聚合。
  - `backend/internal/package/repository.go`：`countActiveOrderReferences` 用非 cancelled 口径给列表读数；`countOrderReferences` 保持 any-reference 口径给删除 in-use。
  - `backend/internal/platform/store/scope.go`：`ScalarAggregate` 只开放受控聚合能力。
  - `backend/internal/platform/store/scope_test.go`：覆盖 count / sum / max、空集、跨账号、非法 op 与非法列名。
- 既有边界沉淀：`.codestable/compound/2026-07-06-accountscope-fail-loud.md` 已记录 AccountScope 必须 fail-loud；本条是它在跨域读模型里的具体落点。
