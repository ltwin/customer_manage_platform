-- V6-076：图片编辑候选。点击应用时创建，image_edit 队列编码出结果文件（不计额度、
-- 不绑定修订），前端再调用同步采用（后端设计 §6）。未终结且未过期的候选持有源修订与
-- 结果 blob，登记在媒体 GC 的持有方清单里。
CREATE TABLE creative_image_edit_renders (
 account_id TEXT NOT NULL, id TEXT NOT NULL,
 operation_id TEXT NOT NULL, request_hash TEXT NOT NULL,
 canvas_id TEXT NOT NULL, node_id TEXT NOT NULL, source_revision_id TEXT NOT NULL,
 action TEXT NOT NULL CHECK(action IN ('compress','crop')),
 parameters JSONB NOT NULL, encoder_policy TEXT NOT NULL,
 caller_kind TEXT NOT NULL CHECK(caller_kind IN ('photographer','agent')),
 state TEXT NOT NULL CHECK(state IN ('queued','running','ready','not_reduced','failed','discarded','expired','applied')),
 revision BIGINT NOT NULL DEFAULT 1,
 -- queued / running 时是处理期限，ready 时是可采用期限。
 expires_at TIMESTAMPTZ NOT NULL,
 error_code TEXT NOT NULL DEFAULT '',
 -- 某次尝试认领后即写入，写对象前就能找到它们；ready 之前它们是 verifying。
 original_blob_id TEXT, display_blob_id TEXT,
 -- 实际输出：mime、宽高、字节数、摘要，以及源文件字节数。
 output JSONB NOT NULL DEFAULT '{}',
 applied_operation_id TEXT, result_revision_id TEXT, applied_version_id TEXT, change_id TEXT,
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(account_id,id),
 UNIQUE(account_id,operation_id),
 CHECK(state<>'applied' OR result_revision_id IS NOT NULL),
 CHECK(original_blob_id IS NOT NULL OR display_blob_id IS NULL)
);
-- 每个节点同时最多一个未终结候选；过期但尚未清扫的行在受理前先转为 expired。
CREATE UNIQUE INDEX creative_image_edit_renders_active_node ON creative_image_edit_renders(account_id,canvas_id,node_id) WHERE state IN ('queued','running','ready');
CREATE INDEX creative_image_edit_renders_account_state ON creative_image_edit_renders(account_id,state,expires_at);
-- 每账号创建频率上限按最近 10 分钟计数。
CREATE INDEX creative_image_edit_renders_created ON creative_image_edit_renders(account_id,created_at);
CREATE INDEX creative_image_edit_renders_source ON creative_image_edit_renders(account_id,source_revision_id) WHERE state IN ('queued','running','ready');
