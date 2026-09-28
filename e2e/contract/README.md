# Contract tests

Black-box tests that exercise a running Goosar server over plain HTTP and
check every response against `docs/50-api-contract.yaml` (status code
documented, JSON body matches its schema). They import no server code — they
are a client of the API, exactly like any other clean-room implementation
would be.

## Running against a server

```
BASE_URL=http://localhost:8199 \
GOOSAR_DEV_VERIFICATION_CODE=424242 \
go test ./e2e/contract/... -v
```

- `BASE_URL` (required) — base URL of the server under test. If it is not
  set, the whole suite is skipped (`t.Skip`), so it is safe to leave in a
  normal `go test ./...` run of the repo.
- `GOOSAR_DEV_VERIFICATION_CODE` (required to run anything past `auth`) —
  the fixed 6-digit login code the server accepts for any email when
  `APP_ENV` is not `production`. Must match the value the server was started
  with.
- `DATABASE_URL` (optional) — the server's own database, read by this suite
  only for context/diagnostics; the suite talks to the server over HTTP, it
  does not connect to Postgres itself. Not required to run the tests.

Every subtest creates its own workspace/runtime/agent/etc. with unique
names, so the suite can be run repeatedly against the same long-lived
database without cleanup.

### Running one section

Every domain is its own named subtest, so any one of them can be run alone;
it will transparently create whatever it depends on (a workspace, a
registered runtime, an agent, ...) if a fuller run hasn't already done so:

```
go test ./e2e/contract/... -run 'Contract/auth' -v
go test ./e2e/contract/... -run 'Contract/issues' -v
go test ./e2e/contract/... -run 'Contract/daemon' -v
```

Sections: `auth`, `workspaces`, `me`, `issues`, `projects`, `labels`,
`comments`, `squads`, `autopilots`, `agents`, `runtimes`, `skills`, `chat`,
`inbox`, `daemon`, `deployment`, `admin`, `integrations`.

## Standing up a server to test against

From the repository root:

```
# 1. A local Postgres reachable at localhost:5432, user postgres, trust auth
createdb -h localhost -p 5432 -U postgres goosar_contract

# 2. Apply migrations
cd server
DATABASE_URL="postgres://postgres@localhost:5432/goosar_contract?sslmode=disable" \
  go run ./cmd/migrate up

# 3. Start the server with the minimal env this suite needs
DATABASE_URL="postgres://postgres@localhost:5432/goosar_contract?sslmode=disable" \
JWT_SECRET="some-dev-secret" \
ALLOW_SIGNUP=true \
GOOSAR_DEV_VERIFICATION_CODE=424242 \
FRONTEND_ORIGIN="http://localhost:3199" \
PORT=8199 \
  go run ./cmd/server &

# 4. Run the suite
cd ..
BASE_URL=http://localhost:8199 GOOSAR_DEV_VERIFICATION_CODE=424242 \
  go test ./e2e/contract/... -v
```

Everything else (Redis, S3, Composio, VCS, Slack, cloud billing, cloud
runtime fleet, ...) is optional: the suite accepts whichever status code the
contract documents for "this integration is not configured"
(typically 502/503) on those endpoints, rather than requiring 200.

Stop the server when done (`kill %1`, or find it with
`lsof -i :8199` / `pkill -f cmd/server`).

## What "passing" means here

- Every HTTP status code this suite receives must be one `docs/50-api-contract.yaml`
  documents for that operation. An undocumented status is a test failure —
  fix the spec (the server is the source of truth for behavior), not the
  test, unless the test's own expectations were wrong.
- Every JSON response body must satisfy the `application/json` schema
  documented for the status code it came back with.
- At the end of a run, the suite prints how much of the contract it
  exercised, e.g.:

  ```
  === Contract coverage: 121/406 documented operations exercised ===
  ```

  This is a floor, not a ceiling: many operations need infrastructure this
  suite intentionally does not stand up (git-backed VCS providers, Slack,
  Composio, a real cloud-runtime fleet, S3, an actual daemon process holding
  a WebSocket open), or destructive/irreversible actions (deleting a
  workspace, revoking every session) that are deliberately left untouched
  here to keep the suite repeatable.

## Layout

- `spec.go` — loads `docs/50-api-contract.yaml` (via `getkin/kin-openapi`)
  and validates a response's status+body against it; tracks coverage.
- `client.go` — a minimal HTTP client (Bearer token, `X-Workspace-ID`, JSON
  in/out); knows nothing about the server's implementation.
- `harness.go` — the shared fixture and its idempotent `ensureX` setup
  helpers (auth, workspace, daemon runtime registration, agent, issue,
  project, label, squad, chat session, skill).
- `contract_test.go` — `TestContract` and one function per domain section.
- `check_routes.py` — a separate, standalone check (not part of `go test`)
  that parses `server/cmd/server/router.go`'s route table and diffs it
  against `docs/50-api-contract.yaml`; see `python3 e2e/contract/check_routes.py --help`.
