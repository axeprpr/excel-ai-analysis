# Release Checklist

## Current V1 State

The repository currently provides a runnable single-container service with:

- Session-scoped local workspaces
- One SQLite database per session
- Upload APIs for `.csv`, `.xlsx`, and `.xls`
- Real `.csv` import into SQLite
- Minimal `.xlsx` import into SQLite for the first sheet
- Placeholder import flow for `.xls`
- Import task tracking in JSON and SQLite
- Schema catalog persistence in JSON and SQLite
- Query planning and execution against local SQLite when possible
- Health and readiness probes
- Dockerfile, Compose file, and Make targets

## Verified Before Release

- `go test ./...`
- `go build ./...`
- `bash ./scripts/smoke.sh`

## API Areas Already Implemented

- Session lifecycle endpoints
- File upload and file listing endpoints
- Import task detail and list endpoints
- Schema inspection endpoint
- Database inspection endpoint
- Query endpoint with chart-oriented metadata
- Root, health, and readiness endpoints
- Global API status summary endpoint
- Browser console and local model settings endpoint

## Known Functional Limits

- `.xls` does not have real parsing yet and still uses placeholder schema/table scaffolding
- `.xlsx` import currently reads only the first sheet in the minimal real-import path
- AI text-to-SQL generation is still heuristic and local-rule based
- Query safety is still based on narrow server-side SQL generation, not a full SQL policy engine
- No authentication or authorization layer exists yet
- No background cleanup worker exists yet for expired sessions

## Minimum Production Requirements Still Missing

- Auth in front of all session and query APIs
- Configurable retention cleanup for expired sessions
- Structured request logging
- Better import error classification and retry policy
- Resource limits and observability for large concurrent imports
- Real multi-sheet `.xlsx` import and true `.xls` parsing

## Suggested Deployment Checks

1. Confirm `sqlite3` is available in the runtime image or VM.
2. Confirm `DATA_DIR` points to persistent disk.
3. Confirm `GET /readyz` returns `200`.
4. Confirm session creation and upload work on the target machine.
5. Confirm disk usage alarms exist for the session data directory.

## Suggested First Post-Release Priorities

1. Replace placeholder `.xls` handling with real parsing.
2. Extend `.xlsx` import from first-sheet-only to multi-sheet import.
3. Add auth and per-session access control.
4. Add cleanup of expired sessions.
5. Add structured metrics and logs for import and query latency.
