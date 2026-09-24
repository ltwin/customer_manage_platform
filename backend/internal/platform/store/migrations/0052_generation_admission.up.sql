-- FND-13 G2: 生成请求的持久准入与非token结算（§19.3/§19.8）。
-- 全部为加法或CHECK放宽：旧行默认chat，不重算旧价格、不自动授权素材。
-- 生成请求不携带Chat token上限，output_limit固定0，禁止伪token上限。
ALTER TABLE llm_requests
 ADD COLUMN request_kind TEXT NOT NULL DEFAULT 'chat' CHECK(request_kind IN ('chat','generation'));
ALTER TABLE llm_requests DROP CONSTRAINT llm_requests_output_limit_check;
ALTER TABLE llm_requests ADD CONSTRAINT llm_requests_output_limit_check
 CHECK(request_kind='generation' OR output_limit>0);

-- 生成扩展表：1:1挂接llm_requests，快照为受信BYTEA（冻结程序+指纹核验），
-- JSONB仅作可重建投影。长度上限对应Go侧maxGenerationJSONBytes+maxGenerationPolicyBytes
-- （6*(1<<20)+(256<<10)+(512<<10)=7077888）；Go常量调整时同步此CHECK。
CREATE TABLE llm_generation_requests (
 account_id TEXT NOT NULL,
 request_id TEXT NOT NULL,
 policy_snapshot BYTEA NOT NULL CHECK(octet_length(policy_snapshot) BETWEEN 1 AND 7077888),
 fingerprint TEXT NOT NULL CHECK(fingerprint ~ '^[a-f0-9]{64}$'),
 -- 首次受理的控制输入，供幂等重放核验；不以组行或consumer的当前状态替代。
 admission_control JSONB NOT NULL CHECK(jsonb_typeof(admission_control)='object'),
 engine_version TEXT NOT NULL CHECK(char_length(engine_version) BETWEEN 1 AND 64),
 policy_version TEXT NOT NULL CHECK(char_length(policy_version) BETWEEN 1 AND 64),
 model_key TEXT NOT NULL CHECK(model_key ~ '^[a-z][a-z0-9_.-]{0,63}$'),
 model_revision TEXT NOT NULL CHECK(char_length(model_revision) BETWEEN 1 AND 256),
 deployment_id TEXT NOT NULL CHECK(char_length(deployment_id) BETWEEN 1 AND 256),
 adapter_version TEXT NOT NULL CHECK(char_length(adapter_version) BETWEEN 1 AND 256),
 mode_key TEXT NOT NULL CHECK(char_length(mode_key) BETWEEN 1 AND 256),
 kind TEXT NOT NULL CHECK(kind IN ('text','image','video','audio')),
 currency CHAR(3) NOT NULL CHECK(currency ~ '^[A-Z]{3}$'),
 reserved_upper_bound_micros BIGINT NOT NULL CHECK(reserved_upper_bound_micros>=0),
 quote_lines JSONB NOT NULL CHECK(jsonb_typeof(quote_lines)='array'),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(account_id,request_id)
);

-- generation_total是生成attempt的唯一成本分量：一份完整用量证据原子登记总额，
-- 组件明细保存在证据的usage_dimensions，禁止跨组件逐个独立落账。
ALTER TABLE llm_usage_measurements DROP CONSTRAINT llm_usage_measurements_cost_component_check;
ALTER TABLE llm_usage_measurements ADD CONSTRAINT llm_usage_measurements_cost_component_check
 CHECK(cost_component IN ('input_cached','input_uncached','output','budget_total_tokens','generation_total'));
ALTER TABLE llm_cost_positions DROP CONSTRAINT llm_cost_positions_cost_component_check;
ALTER TABLE llm_cost_positions ADD CONSTRAINT llm_cost_positions_cost_component_check
 CHECK(cost_component IN ('input_cached','input_uncached','output','generation_total'));

-- 超上界争议的最小部署阻断：该账号对该部署的新准入暂停，要求人工核实解除；
-- 既有operation幂等回读与原请求的核算不受影响（§19.6/GEN-10）。
CREATE TABLE llm_generation_deployment_blocks (
 account_id TEXT NOT NULL,
 deployment_id TEXT NOT NULL CHECK(char_length(deployment_id) BETWEEN 1 AND 256),
 reason TEXT NOT NULL CHECK(char_length(reason) BETWEEN 1 AND 240),
 source_request_id TEXT NOT NULL,
 source_measurement_id TEXT NOT NULL,
 blocked_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(account_id,deployment_id)
);
