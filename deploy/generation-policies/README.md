# 媒体生成规则文件

当前使用本地JSON作为规则源，后续配置平台可实现同一个`GenerationPolicySource`接口。每个模型部署独立管理一个`GenerationPolicyCatalog`，明确指定要加载的文件，不扫描目录自动启用所有JSON。

## 文件位置与内容

- 仓库路径：`deploy/generation-policies/<部署名称>.<规则版本>.json`。
- `example-video.v1.json`是由自动化测试验证的完整示例，模型、参数限制与价格都是虚构值；没有生产Adapter，不会被服务启动自动加载。
- JSON保持规则包原格式，包含模型/模式规则、价格、版本和部署绑定。不放API密钥、账号信息、临时素材URL。
- 文件原始大小及编译后的规范表示均不得超过512KiB；具体字段见`docs/product/creative-canvas-system/modules/tool-generation-runtime.md`第17节。

## 加载与发布

服务端装配已注册的Adapter后，调用下面的入口。`target`必须来自Adapter注册，不能从JSON反向构造；尤其是实际计量单位和硬上限。`catalog`应当在整个部署生命周期内复用，不能每次重载重新创建。

```go
source, err := llmgateway.NewGenerationPolicyFileSource(policyPath)
if err != nil {
    return err
}
revision, err := catalog.PublishFromSource(
    ctx, source, target, expectedRevision, validUntil, now,
)
if err != nil {
    return err
}
// 保存返回的目录修订号，供下一次显式发布核对。
_ = revision
```

- `policyPath`由服务端配置提供，建议绝对路径；相对路径在构造时按进程工作目录转为绝对路径。
- 首次发布`expectedRevision=0`。后续发布传入已知修订号，冲突后先核对当前版本，不盲目重试覆盖。
- `validUntil`与`now`由受信发布控制面提供；文件读取不自动续期，已过期配置不能用于新请求。
- 加载时读取整个文件并经过原有编译、版本检查和原子发布。文件不存在、不可读、过大、格式错误或规则不合法时返回错误，已发布目录保持原状。
- 读取普通本地文件；部署目录不允许外部请求控制路径或不受信进程写入。读取前后检查取消，但本地文件系统I/O不能被context强制中断。

## 修改与生效

1. 复制上一份文件，修改规则并提升`version`，保留旧文件用于回滚。
2. 将新文件完整写入临时路径，再通过同一文件系统原子替换或使用新的版本文件路径，避免发布半份内容。
3. 在服务端显式调用`PublishFromSource`；编译通过且修订号匹配后，新请求读取新版本，已准备任务保留旧快照。
4. 回滚时加载保存好的旧文件，仍使用当前目录修订号发布；旧文件内容不得改写。

仅编辑文件不会自动生效：当前没有文件监听、后台轮询、重载HTTP接口或CLI。生产生成Adapter/运行时尚未装配，因此目前是可调用且有测试的加载入口，没有添加无消费者的启动环境变量。后续接入启动装配时明确调用该入口，首次加载失败不能启用对应部署。

目录仍是单进程内存状态：重启要重新加载文件，256个版本摘要上限及跨重启版本不可变记录尚需持久仓库补齐。文件加载不解决多实例同步、审计或发布授权；后续配置平台适配只负责取得JSON，共用相同编译发布逻辑。
