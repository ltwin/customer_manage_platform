---
slug: creative-capability-follows-account-status
status: observed
scope: 创意空间：能力 / 权限相关的设计与测试
createdAt: 2026-10-16T18:08:28.371Z
source: d04f6c6d-5e57-41d1-8e19-62777ff4d5fb
---
创意能力（creative_read、manual_write 等）现在只由账号状态决定（store.CreativeCapabilities：status='active' 即全开），creative_account_capabilities 表不再参与判断，所以无法单独关闭 manual_write。设计或测试里写「去掉 manual_write 后 403」时，实际只能用非 active 账号验证，读写都会 403。另：creativeops 执行器的幂等哈希包含 client_created_at，重放测试必须复用同一个创建时间，否则返回 409 idempotency_conflict。
