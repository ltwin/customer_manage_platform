# Defect routing log（D8）

| 发现 | 类型 | 动作 |
|---|---|---|
| `restore-compose` compose-identity（fixed services/volumes）导致 v1 wipe / v2 mixed 全量 restore 无法完成 | 上游 ops／compose 契约（非本条呈现） | **blocked-on-upstream**；package-validate + dual-key 断言已落盘；记 CMD-002 exit 2 |
| 跨标签页 settings 丢更新 | accepted-residual（system-settings） | **白名单**；不退回、不补协议 |
| 人工断点浏览器截图（375／1440／200%／coarse）本会话未真开浏览器 | 证据形态不足 | STEP-004 **partial**：可复跑清单 + 模型／CSS 断言覆盖双插槽／键盘／nav／toggle |
| openapi／accountprofile／avatarmedia／dataexport 分支上已有 Wave1–3 改动 | 非本条 diff | A9：hardening 本条未对上述路径做非删除改动 |
| `make check` → `golangci-lint`：accountprofile／avatarmedia gofmt×3 + staticcheck×2 | Wave1–3 机械清洁债 | **fixed（goal follow-up）**：gofmt 三文件 + `Cursor(key)` + 删空分支；非协议变更 |

呈现级自修（本条）：redirect、navItems、theme-toggle 清除、AccountMenu 方向键循环、D11 后删旧页。
