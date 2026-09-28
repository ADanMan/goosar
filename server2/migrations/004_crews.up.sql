-- 004_crews: schemas.Squad / SquadMember, renamed to crews/crew_members.
-- A crew member is polymorphic (an operative or a human account), same pattern
-- the contract itself uses (member_type + member_id) — no single FK is possible,
-- so cm_member_id is a soft reference resolved in application code by cm_member_type.

CREATE TABLE crews (
    id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id            uuid NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
    crew_title              text NOT NULL,
    crew_summary            text,
    crew_instructions       text,
    crew_avatar_uri         text,
    crew_leader_type        text NOT NULL CHECK (crew_leader_type IN ('agent','member')),
    crew_leader_id          uuid NOT NULL,
    crew_creator_account_id uuid NOT NULL REFERENCES accounts(id),
    crew_archived_at        timestamptz,
    crew_archived_by        uuid REFERENCES accounts(id),
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_at              timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX crews_workspace_ix ON crews (workspace_id) WHERE crew_archived_at IS NULL;

CREATE TABLE crew_members (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    crew_id        uuid NOT NULL REFERENCES crews(id) ON DELETE CASCADE,
    cm_member_type text NOT NULL CHECK (cm_member_type IN ('agent','member')),
    cm_member_id   uuid NOT NULL,
    cm_role        text NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT crew_members_uk UNIQUE (crew_id, cm_member_type, cm_member_id)
);
CREATE INDEX crew_members_lookup_ix ON crew_members (cm_member_type, cm_member_id);
