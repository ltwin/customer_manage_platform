# Eino v0.9.19 隔离适配实验

这是[接入决策](../../eino-adoption.md)的证据代码，**真实运行Eino ADK和PostgreSQL，模型响应与业务表是最小实验替身**。不连接供应商、OSS或应用数据库，不生成真实模型费用，不修改backend/go.mod/go.sum。不能将此目录当生产Gateway/Harness实现。

在仓库根目录运行：

```sh
python3 docs/product/creative-canvas-system/probes/eino/run.py
```

需要Go1.25.5与Docker。脚本在backend/internal下创建临时嵌套模块，借用实际backend的storetest基座，执行后清理；一个父测试容器、各用例独立数据库，恢复子进程复用该用例的测试库。probe.mod/sum锁定Eino及所需依赖，默认`-mod=readonly`；显式`--refresh-lock`才更新实验锁文件，不改产品依赖。首次依赖下载需要网络。

## 范围与结果

| 实验 | 实际验证 | 不证明什么 |
|---|---|---|
| TestCheckpointAcrossProcesses | 真实Runner中断、PG Get/Set持久化；另一个Go进程Resume；工具执行一次；跨账号checkpoint查询不可见 | 正式Checkpoint adapter的run/epoch/CAS/TTL、完整认证/授权链 |
| TestCommitBeforeCheckpointCrash | 实际工具事务提交后，Checkpoint.Set写入前子进程以71退出；无checkpoint但有回执；新进程在固定两轮轨迹回放原模型结果/工具回执，只生成一份效果 | 通用多轮恢复、模型输入完整hash、同参数不同意图、任意进程崩溃都可自动恢复 |
| TestCancelledResumeCannotWrite | PG取消/epoch更新后另进程Resume，工具守卫拒绝、无效果 | 真实供应商取消/费用、业务slot/租约全部竞态 |
| TestSkillProgressiveLoading | 实际Skill Middleware第一次只向模型提供目录描述，Get未调用；模型调用skill工具后才见固定正文 | 第三方包、参考资源读取、生产发布/版本权限机制 |
| TestSummarizationUsesInjectedModel | 实际摘要中间件使用注入的模型接口；主模型收到压缩结果，两次调用分别可计数 | 摘要质量、中文token估算、来源/授权闭包、真实Gateway费用账本 |
| TestReductionPreservesFullToolResult | 实际Reduction将大工具结果写入PG Backend，模型上下文缩减，完整结果仍可取 | ReadRunResult正式只读工具、生产文件/媒体保留和越权防护 |
| TestUnknownModelErrorNotAutomaticallyRetried | 未配置Eino模型重试/failover时，一个unknown错误只调用一次注入端口 | 供应商SDK隐藏重试、所有流中断分类和Gateway核实 |
| TestAgentAsToolIsolatesInput | 实际子Agent只收到明确task，不继承父私有文字，结果回到主Agent | 多Agent生产调度、共享预算/取消传播/并行写入安全 |

TestProbeChild是由上面恢复用例启动的子进程入口，父进程发现阶段按设计skip；它在各子进程真正运行，不是漏测用例。

固定源码已编译：model.BaseChatModel/ToolCallingChatModel、WithTools独立配置、ToolsNode顺序执行、Skill Backend List/Get、Checkpoint Get/Set、接口式Handlers。Stream适配方法只有签名/单完整消息实现，本轮没有运行真实分片流或SSE，不宣称流式故障验证通过。

最终一轮（锁文件只读，runner隔离外部测试控制环境变量）8项PASS，包2.750s：跨进程恢复0.10s、提交间隙退出/回放0.08s、取消恢复0.08s、Reduction0.06s，其余不足0.01s。没有运行全应用编译/测试。

## 接入时不能复制的实验简化

- replayGateway只模拟预先定义的model-1/model-2，hash仅覆盖该固定脚本语义；真实模型调用必须绑定持久model step和完整规范化输入，不能按prompt相同就当同一次请求。
- writeTool只有一个预分配tool-1和一行效果，便于观察重放；真实工具须使用完整operation/read set/slot/epoch/权限与receipt事务协议。
- pgCheckpoint仅实现账号隔离Get/Set，用于验证框架序列化与跨进程；生产必须加run绑定、当前执行权、版本/CAS、大小上限与清理，读取旧checkpoint后重新注入当前受信主体，不能采信序列化的旧授权。
- 显式os.Exit是有界故障注入；目标数据库来自storetest，父容器清理由标准基座负责。ReadRunResult、Skill引用资源及多媒体句柄的真正实现仍在FND-07/08。

上游源码依据：[模型接口](https://github.com/cloudwego/eino/blob/v0.9.19/components/model/interface.go)、[Runner](https://github.com/cloudwego/eino/blob/v0.9.19/adk/runner.go)、[Checkpoint接口](https://github.com/cloudwego/eino/blob/v0.9.19/internal/core/interrupt.go)、[Skill](https://github.com/cloudwego/eino/blob/v0.9.19/adk/middlewares/skill/skill.go)。本次直接读取Go模块缓存中的固定tag源码，未修改依赖源。
