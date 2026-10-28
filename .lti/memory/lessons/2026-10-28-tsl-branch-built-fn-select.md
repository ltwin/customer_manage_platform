---
slug: tsl-branch-built-fn-select
status: observed
scope: 3D 场景编辑器：写 three r186 的 TSL 着色（尤其灯光循环里按类型、形状分支）
createdAt: 2026-10-28T07:36:28.486Z
source: 847866d9-bbe9-452c-84a7-c84bac399f2c
---
three r186 的 TSL：在 Loop 里的 If 分支内再套一层 If，并在内层分支里第一次构建返回 float 的函数（three 的 LTC_Evaluate、自写的 Fn 如 ltcEllipse），生成着色器时报「Invalid generated code, expected a float」，连不含这类灯的场景也报（着色器里有这条分支）。给角点加 toVar、把 P/n/v 提成局部变量都没用；在外层 If 里直接调用没问题。做法：两边都算，用 select 取一边（代价是多算一次）。同类现象：表达式第一次在某个分支里构建，就只在该分支作用域有效，分支外复用会出错，需要在分支外先建好或 toVar。出处：scene-area-light 4.1，render/trace/hit.ts 面光源高光。
