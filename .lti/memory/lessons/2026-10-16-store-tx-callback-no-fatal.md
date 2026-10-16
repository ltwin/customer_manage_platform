---
slug: store-tx-callback-no-fatal
status: observed
scope: backend Go 集成测试：store.WithTxScope / WithReadSnapshot 回调、测试 goroutine
createdAt: 2026-10-16T18:08:28.107Z
source: d04f6c6d-5e57-41d1-8e19-62777ff4d5fb
---
事务回调里不要调用 t.Fatal / t.FailNow：Goexit 时连接没有归还，t.Cleanup 里的 st.Close() 会一直等，测试卡到 10 分钟超时只报 panic，看不到真实断言。回调里返回 error，断言放到回调外面；并发 goroutine 里同样只记录错误。来源：creative-document-foundation 的查询次数守卫测试卡死。
