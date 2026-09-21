-- FND-13 G1：生成参考的版本化媒体事实与去重探测任务。事实按精确对象
-- 版本 + 提取器版本作键；NULL 度量列表示「未确立」，绝不是零。这里
-- 不存任何临时 URL、凭证或客户端上报值。
CREATE TABLE creative_media_facts (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL,
 blob_id TEXT NOT NULL, object_version TEXT NOT NULL,
 digest TEXT NOT NULL CHECK(digest ~ '^sha256-[a-f0-9]{64}$'),
 extractor_version TEXT NOT NULL CHECK(char_length(extractor_version) BETWEEN 1 AND 64),
 facts_schema_version INTEGER NOT NULL CHECK(facts_schema_version>0),
 kind TEXT NOT NULL CHECK(kind IN ('image','video','audio')),
 mime TEXT NOT NULL CHECK(char_length(mime) BETWEEN 3 AND 120),
 byte_size BIGINT NOT NULL CHECK(byte_size>0),
 width INTEGER CHECK(width>0), height INTEGER CHECK(height>0),
 dimension_basis TEXT NOT NULL CHECK(dimension_basis IN ('oriented','container','none')),
 duration_ms BIGINT CHECK(duration_ms>=0),
 frame_rate_num BIGINT CHECK(frame_rate_num>0), frame_rate_den BIGINT CHECK(frame_rate_den>0),
 probed_at TIMESTAMPTZ NOT NULL,
 facts_sha256 TEXT NOT NULL CHECK(facts_sha256 ~ '^sha256-[a-f0-9]{64}$'),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(account_id,id), UNIQUE(account_id,blob_id,object_version,extractor_version),
 CHECK((width IS NULL)=(height IS NULL)),
 CHECK((frame_rate_num IS NULL)=(frame_rate_den IS NULL)),
 CHECK(kind<>'image' OR (width IS NOT NULL AND duration_ms IS NULL AND frame_rate_num IS NULL AND dimension_basis='oriented')),
 CHECK(kind<>'video' OR (width IS NOT NULL AND dimension_basis='container')),
 CHECK(kind<>'audio' OR (width IS NULL AND frame_rate_num IS NULL AND dimension_basis='none'))
);
CREATE INDEX creative_media_facts_blob ON creative_media_facts(account_id,blob_id);
-- 每个精确对象版本 + 提取器版本一个探测任务；唯一键是事实回填请求的
-- 去重边界，把提取器版本纳入键后，语义版本升级可以对已成功的对象在
-- 自己的键下重新探测。租约/epoch 提供重启恢复；读 pin 只为探测中的
-- worker 保住对象版本。
CREATE TABLE creative_media_probes (
 id TEXT PRIMARY KEY, account_id TEXT NOT NULL,
 blob_id TEXT NOT NULL, object_version TEXT NOT NULL,
 extractor_version TEXT NOT NULL CHECK(char_length(extractor_version) BETWEEN 1 AND 64),
 state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','running','succeeded','failed','unsupported')),
 attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts>=0),
 -- 显式重试才开启新轮次；同轮的崩溃恢复与队列补投递共用这一身份。
 retry_round BIGINT NOT NULL DEFAULT 0 CHECK(retry_round>=0),
 execution_epoch BIGINT NOT NULL DEFAULT 0 CHECK(execution_epoch>=0),
 lease_until TIMESTAMPTZ, read_pin_id TEXT,
 error_code TEXT NOT NULL DEFAULT '' CHECK(char_length(error_code)<=100),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(), completed_at TIMESTAMPTZ,
 UNIQUE(account_id,id), UNIQUE(account_id,blob_id,object_version,extractor_version)
);
CREATE INDEX creative_media_probe_due ON creative_media_probes(state,updated_at,account_id,id) WHERE state IN ('pending','running');
