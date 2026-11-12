# 骨骼导入的样例角色

带骨骼人形（scene-skinned-humanoid）用来测试和手动试导入的角色原文件。2026-11-12 owner 确认可以提交进仓库。

| 文件 | 来源 | 授权 | 骨架特点 |
|---|---|---|---|
| `Xbot.glb` | three.js r170 `examples/models/gltf/Xbot.glb` | Mixamo | Mixamo 命名，带手指 |
| `Michelle.glb` | three.js r170 `examples/models/gltf/Michelle.glb` | Mixamo | Mixamo 命名，根节点 Z 轴朝上 |
| `RobotExpressive.glb` | three.js r170 `examples/models/gltf/RobotExpressive/RobotExpressive.glb`（Tomás Laulhé） | CC0 | 非标准骨架，映射不成立（AC-4） |
| `VRM1_Constraint_Twist_Sample.vrm` | pixiv three-vrm `examples/models/VRM1_Constraint_Twist_Sample.vrm` | VRM Public License 1.0 | VRM 1.0，带节点约束（AC-2 的 e2e 用它） |
| `three-vrm-girl.vrm` | pixiv three-vrm v0.6.11 `examples/models/three-vrm-girl.vrm` | VRoid Hub 条款 | VRM 0.x |

- 单测不读这些文件：它们用 `skeletons.json` 里抽出的节点树，加上 `glb.ts` 拼的网格。
- 在编辑器里试导入时，直接从这里选文件即可。
- 两个内置素体（Quaternius UBC）不在这里，见 `docs/dev/builtin-models.md`。
