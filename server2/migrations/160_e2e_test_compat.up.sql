-- 160_e2e_test_compat: T-027 доводка. НЕ часть контракта (docs/50-api-contract.yaml)
-- и не описано в docs/51-data-model.md — это узкая, явно обособленная
-- совместимость с e2e/fixtures.ts (репозитория верхнего уровня), который
-- обращается к БД напрямую SQL-запросами по именам таблиц/колонок исходного
-- сервера ("user", workspace.issue_counter, issue.*, verification_code) для
-- быстрого посева/логина тестовых данных. Модифицировать e2e/fixtures.ts вне
-- рамок этой сессии (см. server2/docs/decisions.md, "T-027 доводка"), поэтому
-- вместо этого здесь заведены представления (views), транслирующие эти имена
-- в реальную схему server2 (accounts/spaces/tickets/login_codes). Ничего из
-- этого не участвует в HTTP API контракта — только в e2e-тестовом прогоне.

-- verification_code: код подтверждения входа по email (см. login_codes,
-- 001_identity.up.sql). login_codes хранит только дайджест кода
-- (lc_code_digest), не открытый текст, поэтому колонка "code" здесь —
-- фиктивное значение: e2e-фикстура использует его только как запасной
-- вариант, когда GOOSAR_DEV_VERIFICATION_CODE не задан (в тестовом прогоне
-- этой сессии он всегда задан, см. server2/README.md).
CREATE VIEW verification_code AS
SELECT
    id,
    lc_email AS email,
    '000000'::text AS code,
    (lc_consumed_at IS NOT NULL) AS used,
    lc_valid_until AS expires_at,
    created_at
FROM login_codes;

-- "user": участник (accounts, 001_identity.up.sql). Простые переименования
-- колонок — представление автоматически обновляемо для SELECT/UPDATE/DELETE
-- по этим полям (см. PostgreSQL "Updatable Views").
CREATE VIEW "user" AS
SELECT
    id,
    acct_email AS email,
    acct_onboarded_at AS onboarded_at,
    acct_onboarding_survey AS onboarding_questionnaire
FROM accounts;

-- workspace: воркспейс (spaces, 002_workspace.up.sql), только счётчик номеров
-- задач — e2e-фикстура резервирует пачку номеров той же колонкой
-- (ws_next_ticket_seq), которой пользуется workspace.Store.IncrementTicketSeq
-- при обычном создании задачи через API, так что номера не пересекаются.
CREATE VIEW workspace AS
SELECT
    id,
    ws_next_ticket_seq AS issue_counter
FROM spaces;

-- issue: задача (tickets, 005_tasks.up.sql). tk_display_key (человекочитаемый
-- идентификатор "PREFIX-N") — NOT NULL в базовой таблице и не входит в
-- набор колонок, которые сеет e2e-фикстура (она знает только "number"), поэтому
-- для INSERT нужен INSTEAD OF-триггер, который вычисляет его из
-- spaces.ws_ticket_prefix; SELECT/DELETE остаются автоматически обновляемыми.
CREATE VIEW issue AS
SELECT
    id,
    workspace_id,
    tk_headline AS title,
    tk_status AS status,
    tk_priority AS priority,
    tk_creator_type AS creator_type,
    tk_creator_id AS creator_id,
    tk_parent_ticket_id AS parent_issue_id,
    tk_position AS position,
    tk_seq_number AS number
FROM tickets;

CREATE FUNCTION e2e_compat_issue_insert() RETURNS trigger AS $$
DECLARE
    prefix text;
    new_id uuid;
BEGIN
    SELECT ws_ticket_prefix INTO prefix FROM spaces WHERE id = NEW.workspace_id;
    IF prefix IS NULL THEN
        RAISE EXCEPTION 'e2e_compat_issue_insert: unknown workspace_id %', NEW.workspace_id;
    END IF;
    new_id := COALESCE(NEW.id, gen_random_uuid());

    INSERT INTO tickets (
        id, workspace_id, tk_seq_number, tk_display_key, tk_headline, tk_status, tk_priority,
        tk_creator_type, tk_creator_id, tk_parent_ticket_id, tk_position
    ) VALUES (
        new_id, NEW.workspace_id, NEW.number, prefix || '-' || NEW.number::text, NEW.title,
        COALESCE(NEW.status, 'backlog'), COALESCE(NEW.priority, 'none'),
        NEW.creator_type, NEW.creator_id, NEW.parent_issue_id, NEW.position
    );

    NEW.id := new_id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER e2e_compat_issue_insert_trg
INSTEAD OF INSERT ON issue
FOR EACH ROW EXECUTE FUNCTION e2e_compat_issue_insert();
