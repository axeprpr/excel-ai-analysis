# Release Checklist

## Product Boundary

- Backend-only service.
- No bundled frontend.
- Primary integration targets are workflow engines and API clients.

## Required Endpoints

- `POST /api/chat/upload`
- `POST /api/chat/upload-url`
- `POST /api/chat/query`
- `GET /healthz`
- `GET /readyz`

## Data and Session

- Session isolation is enabled (one SQLite database per session).
- Session metadata and import task metadata are persisted.
- URL uploads are downloaded into session uploads directory and imported.

## Model and Embedding Config

- Global settings endpoint works:
  - `GET /api/settings/model`
  - `PUT /api/settings/model`
- Request-scoped `model_config` override works on:
  - `/api/chat/upload`
  - `/api/chat/upload-url`
  - `/api/chat/query`

## Query Reliability

- SQL safety guard blocks non-read-only SQL.
- Detail mode row cap is enforced.
- Multi-pass repair flow is active.
- `repair_trace` appears in query response.

## Build and Tests

- `go test ./...` passes.
- `go build ./...` passes.
- Docker build succeeds for backend image.

## Deployment Checks

1. `DATA_DIR` points to persistent storage.
2. `sqlite3` is present in runtime image.
3. `GET /readyz` returns `200`.
4. `POST /api/chat/upload-url` can import from a reachable object URL.
5. `POST /api/chat/query` returns structured response with `summary`, `sql`, `rows`, and `repair_trace`.

## Known Limits

- `.xls` remains placeholder-only.
- `.xlsx` supports structured sheets; complex workbook layouts are not fully handled.
