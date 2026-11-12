# 场景助手工具契约增量

## api: ADD 助手场景工具的输入与写入结果

### `edit_scene@1`

已获场景权限的调用者通过既有的场景文档 ID 和读取基线提交原子批次。普通写入需要 `based_on_revision`。单独撤回按现有撤回规则执行。

| 字段 | 可省略与含义 |
|---|---|
| `add_objects.objects[].id` | 省略则服务端生成合法物件 ID |
| `parent_id`、`visible`、`locked`、`order` | 新增时分别默认根级、可见、未锁定和在同级末尾追加 |
| `transform` | 新增时缺省为原点、单位旋转、单位缩放，更新时只应用传入的分量。新物件使用 `look_at` 时须提供 `position` |
| `look_at`、`yaw_deg`、`transform.rotation` | 三者至多给一个。目标点是世界坐标，水平旋转是局部 Y 轴的角度 |
| `added_object_ids` | 写入新增物件时按新增输入的顺序返回 ID |

分镜更新只写相关关键帧。缺失、错位和互斥参数返回 `creative_tool_refused`，整批回滚。修订冲突和回执重放沿用既有语义。存量调用保持有效。

### `write_model@1`

目标保持 `add` 或 `replace` 二选一。新任务给非空 `code` 或可复用的 `base_content_revision_id`。冲突重试给 `retry_job_id`，与前两种来源及 `params` 互斥。参数错误在入队前返回具体提示。
