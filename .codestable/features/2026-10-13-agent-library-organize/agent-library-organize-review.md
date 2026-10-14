# V6-078 实现独立审查记录

2026-10-13，独立 reviewer `/root/library_organize_change_review`，gpt-6-astra / xhigh。按 cs-review 同一会话三轮只读审查，候选冻结后不编辑；未提交代码。

| 轮次 | 冻结补丁 SHA-256 | 结论 |
|---|---|---|
| 1 | `65d25e3c85ee80fea5d5c2d12de60a29611f76b28070d41d2d9e10cd8a8758d7` | 3 blocking |
| 2 | `2fcecf5b110808b5d8bf596ae03e4434e1ae55041c0370067c4c3fca1e0a2bb8` | 原三项解决；新增 1 blocking |
| 3 | `35351c0b5d473d389c2ffff1b3ecd96a7173097e6cd18a33ace71bf832baa9e4` | 四项全部 resolved，无 blocking / important / nit；整体验收由主流程完成 |

四项发现及修复：

1. 旧 attempt 的失败能覆盖新 attempt 已提交事实：结果登记绑定 exact frozen command + attempt；过时执行者先 fence，再核对当前命令的真实回执。
2. retry 的 Command=nil 与收口交错会 panic：每轮检查调用/阶段状态和命令存在性，完成后安全退出；pending 不能直接完成收口。
3. settling 当次完成的第一页游标无法继续：摘要使用统一 JSON canonical hash，不哈希 PostgreSQL JSONB 原始空格/键序。
4. 同 attempt 的旧失败掩盖后续提交：`LookupCommandInTx` 持操作锁核对准确回执，同事务按操作锁→调用行顺序登记终态。已有回执优先 applied；无回执持锁关闭阶段并由 Guard 拒绝迟到写入。

reviewer 使用真实 PostgreSQL，在 `/tmp` overlay 中加入确定调度，不修改候选。第 3 轮原四项复现 3.684s 全过；额外锁/迟到写入、批次边界、保存绑定/标签事实：creativeops 4.384s、creativeagent 3.615s 全过。每轮结束文件哈希完整匹配。最终 53 文件哈希见 [清单](evidence/reviewed-files.json)。

图谱为 2026-09-28 旧 generation，相关路径 coverage 为 metadata_changed / not_tracked；采用当前源码补证，未用图谱空结果作不存在的证明。模型行为确认不属于服务端强制保证，审查没有扩大已批准契约。

## 审查后的验收收尾

独立审查通过后，真实模型验收暴露图片历史提示歧义及验收夹具问题。没有追加第4轮审查，也没有把下列修改算进第3轮冻结目标：

- `context.go`：仅调整 `omitImage` 的提示文字，明确“仅本轮未附加，不代表此前读取失败”，防止模型把历史图片移出上下文误判为从未看过。图片选择、容量、授权及渲染策略未改。
- `library_organize_live_test.go`：保存场景直接创建测试节点，避免查找助手夹具的测试模型不在真实模型目录；输出上限8192以容纳完整旧描述与新方案；旧描述改为1628码点的编号场景文字，仍按完整原文精确断言。验收断言未放宽。

两文件由主流程核对；Agent全包build/lint/test通过48.983s，图片上下文专项通过5.740s。新的真实20图测试通过（20次读取、20个不同修订、先读后写、按标签命中20）；三个确认场景通过。最终文件指纹见 [最终清单](evidence/final-files.json)，与冻结清单的差异仅上述两文件。
