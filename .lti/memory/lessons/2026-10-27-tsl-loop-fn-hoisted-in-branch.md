---
slug: tsl-loop-fn-hoisted-in-branch
status: observed
scope: 3D 场景编辑器：写含 TSL Loop 的 Fn，并在 If 或其他分支里多次调用（阴影滤波、级联等）
createdAt: 2026-10-27T04:36:54.301Z
source: 9f1feb22-1c9c-4ca5-a2c3-712325a45e8d
---
three r186 里，没有 layout 的 Fn 内含 Loop，又在分支块（If、Loop 体）里被调用时，VarNode.build 的 hasLoop 分支会把调用结果提到着色器开头声明。这个标记记在 Fn 上，不是记在每次调用上：同一个 Fn 被多处调用时，后面的调用也会被提前，读到还没赋值的变量。S0 实测：CSM 四级级联共用一个 PCSS 滤波 Fn，级联 1–3 被提到 shadowPositionWorld 赋值之前，整级全亮，没有报错。做法：每次调用新建一个内层 Fn（外层 Fn 把 inputs 按需要的键拷成普通对象再传给内层；PointShadowNode 会把滤波函数当作 Fn 的输入，不能换成普通 JS 函数）。防回退靠 e2e 检查每一级都有影子。证据：scene-light-size-spike 的 soft-shadow-candidates.md「反应舱」和 prototype/spike/light/soft.ts 的 perShadow。
