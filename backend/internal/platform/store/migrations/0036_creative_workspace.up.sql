-- 0036 · 创作空间先导（creative-workspace-redesign ITEM-2 / ITEM-3）
-- 数据形状依据 Epic「共享语言」与 DEC-17 四项留门：
--   (a) 图片卡记录素材来源分类，默认最严格 unknown_web；
--   (b) 批量搬入保留原始整段文本与批次 ID / 序号；
--   (c) 顺序与分组作为布局信息放在成员关系表，卡片行不含布局字段；
--   (d) 卡片由账号持有，空间归属只经成员关系表达，先导版一卡一空间。

-- ---- 幂等 operation 白名单（沿用 0029 全量重建模式） ----
ALTER TABLE idempotency_records
    DROP CONSTRAINT idempotency_records_operation_check;
ALTER TABLE idempotency_records
    ADD CONSTRAINT idempotency_records_operation_check CHECK (operation IN (
        'order.create.v1',
        'schedule-slot.create.v1',
        'shoot-plan.create.v1',
        'shoot-plan.command.v1',
        'shoot-plan.transition.v1',
        'shoot-plan.run-session.open.v1',
        'shoot-plan.shot.capture.v1',
        'shoot-plan.execution-event.void.v1',
        'shoot-plan.batch-commit.v1',
        'planning-media.upload.v1',
        'planning-media.binding.create.v1',
        'planning-media.binding.release.v1',
        'planning-media.lease.reserve.v1',
        'planning-media.lease.release.v1',
        'plan-ingestion.create.v1',
        'plan-ingestion.preview.v1',
        'plan-ingestion.transition.v1',
        'plan-ingestion.commit.v1',
        'shoot-plan.crm-link.v1',
        'plan-share.issue.v1',
        'plan-share.rotate.v1',
        'plan-share.revoke.v1',
        'plan-share.feedback-disposition.v1',
        'plan-share.feedback-plan-create.v1',
        'plan-share.feedback-shot-create.v1',
        'plan-share.offer-create.v1',
        'plan-share.offer-close.v1',
        'plan-share.assignment-photographer-revoke.v1',
        'plan-share.assignment-claim.v1',
        'plan-share.assignment-self-revoke.v1',
        'shoot-plan.business-facts.v1',
        'shoot-plan.business-drafts.generate.v1',
        'shoot-plan.business-draft.decision.v1',
        'creative-workspace.create.v1',
        'creative-workspace.cards.import.v1',
        'creative-workspace.shoot-items.create.v1',
        'creative-workspace.memos.create.v1',
        'creative-pilot.enroll.v1'
    ));

-- ---- pilot 账号状态（ITEM-2） ----
-- legacy_write → pilot_new_write（preflight 通过后原子切换）；stopped 可恢复旧新建但新空间只读保留。
CREATE TABLE creative_pilot_accounts (
    account_id       TEXT PRIMARY KEY REFERENCES accounts (id),
    state            TEXT NOT NULL CHECK (state IN ('legacy_write', 'pilot_new_write', 'stopped')),
    enrolled_at      TIMESTAMPTZ,
    stopped_at       TIMESTAMPTZ,
    window_id        TEXT,
    revision         BIGINT NOT NULL DEFAULT 1 CHECK (revision >= 1),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CHECK (
        (state = 'legacy_write' AND enrolled_at IS NULL AND stopped_at IS NULL)
        OR (state = 'pilot_new_write' AND enrolled_at IS NOT NULL AND stopped_at IS NULL)
        OR (state = 'stopped' AND enrolled_at IS NOT NULL AND stopped_at IS NOT NULL)
    )
);

-- ---- 创作空间 ----
CREATE TABLE creative_workspaces (
    id             TEXT PRIMARY KEY,
    account_id     TEXT NOT NULL REFERENCES accounts (id),
    kind           TEXT NOT NULL CHECK (kind IN ('project', 'inbox')),
    name           TEXT NOT NULL DEFAULT '' CHECK (char_length(name) <= 80),
    link_order_id  TEXT,
    link_customer_id TEXT,
    archived_at    TIMESTAMPTZ,
    revision       BIGINT NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    last_opened_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (account_id, id),
    -- DEC-15：单值可空外键，纯读引用；被关联对象删除时只置空引用列（PG 15+ 列表语法），account_id 与空间自身不受影响。
    FOREIGN KEY (account_id, link_order_id) REFERENCES orders (account_id, id) ON DELETE SET NULL (link_order_id),
    FOREIGN KEY (account_id, link_customer_id) REFERENCES customers (account_id, id) ON DELETE SET NULL (link_customer_id),
    CHECK (link_order_id IS NULL OR link_customer_id IS NULL),
    -- 未归类空间：无名、不可关联、不可归档。
    CHECK (kind = 'project' OR (name = '' AND link_order_id IS NULL AND link_customer_id IS NULL AND archived_at IS NULL))
);
CREATE UNIQUE INDEX creative_workspaces_inbox_idx
    ON creative_workspaces (account_id) WHERE kind = 'inbox';
CREATE INDEX creative_workspaces_list_idx
    ON creative_workspaces (account_id, archived_at, last_opened_at DESC, id DESC);
CREATE INDEX creative_workspaces_link_order_idx
    ON creative_workspaces (account_id, link_order_id) WHERE link_order_id IS NOT NULL;
CREATE INDEX creative_workspaces_link_customer_idx
    ON creative_workspaces (account_id, link_customer_id) WHERE link_customer_id IS NOT NULL;

-- ---- 搬入批次（DEC-17 (b)） ----
CREATE TABLE creative_import_batches (
    id           TEXT PRIMARY KEY,
    account_id   TEXT NOT NULL REFERENCES accounts (id),
    workspace_id TEXT NOT NULL,
    source_kind  TEXT NOT NULL CHECK (source_kind IN ('paste_text', 'drop_images', 'single')),
    raw_text     TEXT,
    card_count   INTEGER NOT NULL CHECK (card_count >= 0),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (account_id, id),
    FOREIGN KEY (account_id, workspace_id) REFERENCES creative_workspaces (account_id, id),
    CHECK ((source_kind = 'paste_text' AND raw_text IS NOT NULL) OR (source_kind <> 'paste_text' AND raw_text IS NULL))
);

-- ---- 创意卡片（账号持有，DEC-17 (d)；行内不含布局字段，DEC-17 (c)） ----
CREATE TABLE creative_cards (
    id            TEXT PRIMARY KEY,
    account_id    TEXT NOT NULL REFERENCES accounts (id),
    card_type     TEXT NOT NULL CHECK (card_type IN ('text', 'image', 'link')),
    text_body     TEXT CHECK (text_body IS NULL OR char_length(text_body) BETWEEN 1 AND 2000),
    link_url      TEXT CHECK (link_url IS NULL OR char_length(link_url) BETWEEN 1 AND 2048),
    asset_id      TEXT,
    caption       TEXT NOT NULL DEFAULT '' CHECK (char_length(caption) <= 200),
    -- DEC-17 (a)：素材来源分类，未声明即视为不可用于生成；判定仍交 planningmedia 权利矩阵。
    source_class  TEXT CHECK (source_class IS NULL OR source_class IN
                    ('official','anime_screenshot','setting_book','fan','unknown_web',
                     'photographer_owned','licensed','customer_supplied')),
    batch_id      TEXT,
    batch_seq     INTEGER CHECK (batch_seq IS NULL OR batch_seq >= 1),
    revision      BIGINT NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    archived_at   TIMESTAMPTZ,
    UNIQUE (account_id, id),
    FOREIGN KEY (account_id, batch_id) REFERENCES creative_import_batches (account_id, id),
    CHECK (
        (card_type = 'text'  AND text_body IS NOT NULL AND link_url IS NULL AND asset_id IS NULL AND source_class IS NULL AND caption = '')
        OR (card_type = 'link'  AND link_url IS NOT NULL AND text_body IS NULL AND asset_id IS NULL AND source_class IS NULL AND caption = '')
        OR (card_type = 'image' AND asset_id IS NOT NULL AND source_class IS NOT NULL AND text_body IS NULL AND link_url IS NULL)
    ),
    CHECK ((batch_id IS NULL AND batch_seq IS NULL) OR (batch_id IS NOT NULL AND batch_seq IS NOT NULL))
);
CREATE INDEX creative_cards_batch_idx ON creative_cards (account_id, batch_id, batch_seq) WHERE batch_id IS NOT NULL;

-- ---- 简单分组（布局信息） ----
CREATE TABLE creative_card_groups (
    id           TEXT PRIMARY KEY,
    account_id   TEXT NOT NULL REFERENCES accounts (id),
    workspace_id TEXT NOT NULL,
    name         TEXT NOT NULL DEFAULT '' CHECK (char_length(name) <= 40),
    position     INTEGER NOT NULL CHECK (position >= 0),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (account_id, id),
    FOREIGN KEY (account_id, workspace_id) REFERENCES creative_workspaces (account_id, id)
);
CREATE INDEX creative_card_groups_ws_idx ON creative_card_groups (account_id, workspace_id, position);

-- ---- 空间成员关系（归属 + 顺序 + 分组；先导版一卡只有一条） ----
CREATE TABLE creative_card_memberships (
    account_id   TEXT NOT NULL REFERENCES accounts (id),
    card_id      TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    group_id     TEXT,
    position     INTEGER NOT NULL CHECK (position >= 0),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (account_id, card_id),
    FOREIGN KEY (account_id, card_id) REFERENCES creative_cards (account_id, id),
    FOREIGN KEY (account_id, workspace_id) REFERENCES creative_workspaces (account_id, id),
    FOREIGN KEY (account_id, group_id) REFERENCES creative_card_groups (account_id, id) ON DELETE SET NULL (group_id)
);
CREATE INDEX creative_card_memberships_ws_idx ON creative_card_memberships (account_id, workspace_id, position, card_id);

-- ---- 拍摄项（值拷贝 + 来源引用两态 + tombstone） ----
CREATE TABLE creative_shoot_items (
    id                TEXT PRIMARY KEY,
    account_id        TEXT NOT NULL REFERENCES accounts (id),
    workspace_id      TEXT NOT NULL,
    title             TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    description       TEXT NOT NULL DEFAULT '' CHECK (char_length(description) <= 2000),
    ref_asset_id      TEXT,
    source_card_id    TEXT,
    source_card_rev   BIGINT,
    source_card_ids   JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(source_card_ids) = 'array'),
    position          INTEGER NOT NULL CHECK (position >= 0),
    status            TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'tombstone')),
    result            TEXT CHECK (result IS NULL OR result IN ('done', 'skipped')),
    result_note       TEXT NOT NULL DEFAULT '' CHECK (char_length(result_note) <= 500),
    result_at         TIMESTAMPTZ,
    revision          BIGINT NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    tombstoned_at     TIMESTAMPTZ,
    UNIQUE (account_id, id),
    FOREIGN KEY (account_id, workspace_id) REFERENCES creative_workspaces (account_id, id),
    -- 来源引用不设 FK：来源卡被归档/移除时拍摄项完整保留，只显示 unavailable。
    CHECK ((source_card_id IS NULL AND source_card_rev IS NULL) OR (source_card_id IS NOT NULL AND source_card_rev IS NOT NULL)),
    CHECK ((status = 'active' AND tombstoned_at IS NULL) OR (status = 'tombstone' AND tombstoned_at IS NOT NULL)),
    CHECK ((result IS NULL AND result_at IS NULL) OR (result IS NOT NULL AND result_at IS NOT NULL))
);
CREATE INDEX creative_shoot_items_ws_idx ON creative_shoot_items (account_id, workspace_id, status, position);

-- ---- 拍摄备忘（DEC-16：一行一条、可打勾，无 required / 截止 / 负责人 / 提醒） ----
CREATE TABLE creative_shoot_memos (
    id           TEXT PRIMARY KEY,
    account_id   TEXT NOT NULL REFERENCES accounts (id),
    workspace_id TEXT NOT NULL,
    text_body    TEXT NOT NULL CHECK (char_length(text_body) BETWEEN 1 AND 200),
    checked      BOOLEAN NOT NULL DEFAULT FALSE,
    position     INTEGER NOT NULL CHECK (position >= 0),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (account_id, id),
    FOREIGN KEY (account_id, workspace_id) REFERENCES creative_workspaces (account_id, id)
);
CREATE INDEX creative_shoot_memos_ws_idx ON creative_shoot_memos (account_id, workspace_id, position);

-- ---- 创作空间媒体资产（独立生命周期；遗留风险 19） ----
-- 只复用 planningmedia 的图像管线、来源×权利矩阵纯函数与不可变对象存储；不接入旧 ShootPlan holder 路径。
-- 对象键前缀 creative/，与 planning/ 的存量对账互不干扰。
CREATE TABLE creative_workspace_assets (
    id                TEXT PRIMARY KEY,
    account_id        TEXT NOT NULL REFERENCES accounts (id),
    workspace_id      TEXT NOT NULL,
    source_class      TEXT NOT NULL CHECK (source_class IN
                        ('official','anime_screenshot','setting_book','fan','unknown_web',
                         'photographer_owned','licensed','customer_supplied')),
    rights_basis      TEXT NOT NULL CHECK (rights_basis IN
                        ('citation_or_display','ownership_attested','license_recorded','display_consent')),
    generation_reference_granted BOOLEAN NOT NULL DEFAULT FALSE,
    original_media_type TEXT NOT NULL,
    original_size     BIGINT NOT NULL CHECK (original_size > 0),
    original_width    INTEGER NOT NULL CHECK (original_width > 0),
    original_height   INTEGER NOT NULL CHECK (original_height > 0),
    original_checksum TEXT NOT NULL CHECK (original_checksum LIKE 'sha256-%' AND char_length(original_checksum) = 71),
    display_media_type TEXT NOT NULL,
    display_size      BIGINT NOT NULL CHECK (display_size > 0),
    display_width     INTEGER NOT NULL CHECK (display_width > 0),
    display_height    INTEGER NOT NULL CHECK (display_height > 0),
    display_checksum  TEXT NOT NULL CHECK (display_checksum LIKE 'sha256-%' AND char_length(display_checksum) = 71),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (account_id, id),
    FOREIGN KEY (account_id, workspace_id) REFERENCES creative_workspaces (account_id, id),
    -- 与 planning_media_rights_declarations 同一矩阵：来源类决定唯一合法的权利依据；生成许可只对 licensed 有意义。
    CHECK (
        (source_class IN ('official','anime_screenshot','setting_book','fan','unknown_web') AND rights_basis = 'citation_or_display')
        OR (source_class = 'photographer_owned' AND rights_basis = 'ownership_attested')
        OR (source_class = 'licensed' AND rights_basis = 'license_recorded')
        OR (source_class = 'customer_supplied' AND rights_basis = 'display_consent')
    ),
    CHECK (source_class = 'licensed' OR generation_reference_granted = FALSE)
);
CREATE INDEX creative_workspace_assets_ws_idx ON creative_workspace_assets (account_id, workspace_id, created_at DESC, id DESC);

-- 卡片与拍摄项的图片引用指向本表（在 creative_cards / creative_shoot_items 建表时 asset 表尚不存在，故此处补 FK）。
ALTER TABLE creative_cards
    ADD CONSTRAINT creative_cards_asset_fk FOREIGN KEY (account_id, asset_id) REFERENCES creative_workspace_assets (account_id, id);
ALTER TABLE creative_shoot_items
    ADD CONSTRAINT creative_shoot_items_ref_asset_fk FOREIGN KEY (account_id, ref_asset_id) REFERENCES creative_workspace_assets (account_id, id);

-- 执行事实追加保存；撤销不删除之前的拍摄记录。
CREATE TABLE creative_execution_events (
    account_id TEXT NOT NULL REFERENCES accounts(id),
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    shoot_item_id TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('result','undo','note','tombstone')),
    previous_result TEXT,
    result TEXT,
    result_note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (account_id,workspace_id) REFERENCES creative_workspaces(account_id,id),
    FOREIGN KEY (account_id,shoot_item_id) REFERENCES creative_shoot_items(account_id,id)
);
CREATE INDEX creative_execution_events_item_idx ON creative_execution_events(account_id,shoot_item_id,created_at,id);

-- 原始使用观察。客户端只能提交打开事实，不能提交 live=true；真实现场归类由
-- 后续 evidence 报告按冻结的 eligible ledger 时间窗派生。
CREATE TABLE creative_observation_events (
    account_id TEXT NOT NULL REFERENCES accounts(id),
    event_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    kind TEXT NOT NULL CHECK(kind IN ('space_open','live_open','live_unverified')),
    client_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY(account_id,event_id),
    FOREIGN KEY(account_id,workspace_id) REFERENCES creative_workspaces(account_id,id)
);
CREATE INDEX creative_observation_events_ws_idx ON creative_observation_events(account_id,workspace_id,received_at);
