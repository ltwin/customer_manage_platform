---
issue: asset-video-orientation
type: bug
status: deferred
date: 2026-10-12
---

# 视频显示方向与历史尺寸修正

## 目标

正确识别带 display matrix 或 rotate 标记的视频显示方向，让竖拍视频的画幅筛选和检查器尺寸与实际播放一致。

## 现场

`backend/internal/creativemedia/verify.go` 的 `probeOutput` 只读取编码宽高；上传 `probe` 和媒体事实探测 `probeAV` 均使用流的 width/height，后者还明确写 `DimensionBasis=container`。iPhone 竖拍等视频可能以横向尺寸编码再旋转显示。当前未用真机素材复现，根因是源码确认的缺口。

2026-10-12 资产库第 6 步审计提出两种处置，owner 选择范围较小的方案 2：画幅仅匹配图片；视频检查器标「编码尺寸」且不判断横/竖/方形。普通列表、播放、导入和下载不变。这不是完整视频方向修复。

## 边界

后续实现应统一上传与生成导入的显示方向提取、处理历史原始 blob，并核对版本化媒体事实与依赖其尺寸的消费者。不要只交换新上传的宽高而让新旧数据语义混杂，也不要无版本变更地覆盖既有 container 事实。

优先读取 side_data_list 中的 display matrix rotation，再按明确规则兼容 tags.rotate；处理正负 90/270、180、缺失值和异常角度。是否另存编码尺寸/旋转/显示尺寸、历史回填方式，在实施时明确。完成前不恢复视频画幅筛选。

## 证据

- `TestPaletteShapeExcludesVideoUntilOrientationIsKnown`：真实 MP4 上传仍可在普通视频列表找到，但任何画幅条件下 items、total_count、facets 均不包含视频。
- `step6-audit-inspector-video-and-duplicate-colors`：浏览器断言视频只显示编码尺寸，不附带显示画幅标签。
- 上述证明临时规避有效，不证明真机旋转已被处理。

## 验收

- [ ] 普通横拍、iPhone 竖拍、±90/270 和 180 旋转样例经过上传后尺寸和显示方向正确；记录真机样例来源及 ffprobe 结果。
- [ ] 上传与生成导入的语义一致；已发布历史视频修正可重跑、可中断恢复，并保留账号隔离及读取锁。
- [ ] 媒体事实版本及依赖其尺寸的生成/引用逻辑验证，不混淆 container 和 oriented。
- [ ] 恢复视频画幅筛选后，列表、计数、检查器和真实播放方向一致。

## 状态与未决

待后续推进，触发点为 V6-072 智能分组 design 开始前：届时先处理本待办，或在 V6-072 design 中明确画幅条件仅限图片。本轮不改媒体尺寸、不运行历史修正、不构成后续实现或提交授权。
