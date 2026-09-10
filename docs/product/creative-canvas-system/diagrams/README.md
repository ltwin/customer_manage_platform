# 架构与领域图源

| 图 | 图源 | 矢量图 | 预览图 |
|---|---|---|---|
| 系统架构 | [architecture.dot](architecture.dot) | [SVG](architecture.svg) | [PNG](architecture.png) |
| 领域协作 | [domains.dot](domains.dot) | [SVG](domains.svg) | [PNG](domains.png) |
| 核心对象 | [content-model.dot](content-model.dot) | [SVG](content-model.svg) | [PNG](content-model.png) |

图源采用 Graphviz DOT；生成物不手工修改。对应解释见[领域设计](../domain-design.md)，技术契约以架构和数据模型为准。部署框、领域边界和业务对象使用不同图表达，不把图中关系当数据库外键。

在本目录使用已安装的 Graphviz 重新生成：

```sh
for diagram in architecture domains content-model; do
  dot -Tsvg "$diagram.dot" -o "$diagram.svg"
  dot -Tpng -Gdpi=120 "$diagram.dot" -o "$diagram.png"
done
```

字体为 PingFang SC；在其他环境生成时应安装可用中文字体或统一替换图源 fontname 后重新验证。此目录不依赖原型服务，也不访问外部图片或模型服务。
