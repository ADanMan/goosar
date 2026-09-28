DROP INDEX IF EXISTS spaces_template_key_ix;
ALTER TABLE spaces DROP COLUMN IF EXISTS ws_template_key;
ALTER TABLE accounts DROP COLUMN IF EXISTS acct_anonymized_at;
ALTER TABLE accounts DROP COLUMN IF EXISTS acct_deactivated_at;
