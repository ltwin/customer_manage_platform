-- Platform skills stop belonging to an account. They used to be filed under
-- whichever account CREATIVE_PLATFORM_ACCOUNT_ID named; from here on the
-- platform catalog has the reserved owner 'platform', which is not an account,
-- cannot sign in and is named in code rather than configuration.
--
-- origin='platform' is exactly the set to move: only the configured account
-- could ever publish it. Whatever else that account owns stays where it is.
-- Imports are matched on their recorded intent rather than on the skill, so an
-- unfinished import that would have created a platform skill moves too and can
-- still be replayed or reclaimed under the new owner.
--
-- Object keys keep the old account in their path. Versions and imports record
-- the exact key they read or delete, so nothing has to be copied.
UPDATE creative_skill_version_resources r SET account_id='platform'
 FROM creative_skills s, creative_skill_versions v
 WHERE s.origin='platform' AND v.account_id=s.account_id AND v.skill_id=s.id
  AND r.account_id=v.account_id AND r.skill_version_id=v.id;
UPDATE creative_skill_versions v SET account_id='platform'
 FROM creative_skills s
 WHERE s.origin='platform' AND v.account_id=s.account_id AND v.skill_id=s.id;
UPDATE creative_skill_imports SET account_id='platform'
 WHERE verified_manifest->>'origin'='platform';
-- The publisher recorded beside a message or run is a value, not a key; it is
-- rewritten so that it keeps naming the owner that actually holds the version.
UPDATE creative_message_skill_refs SET skill_owner_account_id='platform'
 WHERE skill_id IN (SELECT id FROM creative_skills WHERE origin='platform');
UPDATE creative_run_skill_refs SET skill_owner_account_id='platform'
 WHERE skill_id IN (SELECT id FROM creative_skills WHERE origin='platform');
UPDATE creative_skills SET account_id='platform' WHERE origin='platform';
