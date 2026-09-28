DROP TABLE IF EXISTS dispatch_usage;
DROP TABLE IF EXISTS dispatch_messages;

ALTER TABLE convo_drafts DROP CONSTRAINT IF EXISTS convo_drafts_dispatch_job_fk;
ALTER TABLE convo_messages DROP CONSTRAINT IF EXISTS convo_messages_dispatch_job_fk;
ALTER TABLE sentinel_runs DROP CONSTRAINT IF EXISTS sentinel_runs_dispatch_job_fk;
ALTER TABLE ticket_notes DROP CONSTRAINT IF EXISTS ticket_notes_dispatch_job_fk;

DROP TABLE IF EXISTS dispatch_jobs;
