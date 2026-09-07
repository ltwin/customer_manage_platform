DROP TABLE IF EXISTS creative_observation_events;
DROP TABLE IF EXISTS creative_execution_events;
-- 回退会删除全部创作空间数据；先导版按 forward-fix 原则，不承诺无损回退。对象存储 creative/ 前缀下的文件需另行清理。
DROP TABLE IF EXISTS creative_shoot_memos;
DROP TABLE IF EXISTS creative_shoot_items;
DROP TABLE IF EXISTS creative_card_memberships;
DROP TABLE IF EXISTS creative_card_groups;
DROP TABLE IF EXISTS creative_cards;
DROP TABLE IF EXISTS creative_workspace_assets;
DROP TABLE IF EXISTS creative_import_batches;
DROP TABLE IF EXISTS creative_workspaces;
DROP TABLE IF EXISTS creative_pilot_accounts;

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
        'shoot-plan.business-draft.decision.v1'
    ));
