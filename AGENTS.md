# Repository Guidelines

## Project Structure & Module Organization
- `cmd/ds2api`: main backend entrypoint.
- `cmd/ds2api-tests`: end-to-end testsuite runner.
- `internal/`: core Go modules (`adapter/`, `server/`, `config/`, `auth/`, `stream/`, etc.).
- `internal/adapter/openai/tool_sieve_*.go`: streaming tool-call sieve for mixed text + DSML/XML payloads.
- `internal/util/toolcalls_*.go`: tool-call parse/repair/scan pipeline used across adapters.
- `api/`: Vercel-facing entrypoints (`index.go`, `chat-stream.js`).
- `webui/`: React + Vite admin UI source; build artifacts go to `static/admin/` (generated output).
- `tests/node/`: Node.js unit tests for stream and compatibility logic.
- `tests/compat/fixtures/` and `tests/compat/expected/`: parser fixtures and expected outputs.
- `tests/scripts/`: canonical test and gate scripts used locally and in CI.

## Build, Test, and Development Commands
- `go run ./cmd/ds2api`: start backend locally (default `http://localhost:5001`).
- `npm install --prefix webui && npm run dev --prefix webui`: run WebUI dev server (default `http://localhost:5173`).
- `./scripts/build-webui.sh`: build WebUI into `static/admin/`.
- `./tests/scripts/run-unit-all.sh`: run Go + Node unit suites.
- `./tests/scripts/run-live.sh --no-preflight`: run live end-to-end flow (real test accounts).
- `./tests/scripts/check-refactor-line-gate.sh`: enforce JS/entry file line-count gates.
- `./tests/scripts/check-stage6-manual-smoke.sh`: verify release smoke-check metadata.

## Coding Style & Naming Conventions
- Go: run `gofmt` before commit; keep packages lowercase; keep tests in `*_test.go`.
- JavaScript/React: follow existing functional-component style in `webui/src/`.
- Prefer descriptive, concern-based filenames (example: `responses_stream_runtime_toolcalls.go`, `useSettingsForm.js`).
- Keep changes aligned with existing module boundaries rather than introducing cross-cutting utility files.
- For tool-call compatibility work, prefer extending `internal/util/toolcalls_*` and `tool_sieve_*` modules instead of adding ad-hoc parser logic in handlers.

## Testing Guidelines
- Minimum pre-PR baseline: `go test ./...` and `./tests/scripts/run-unit-node.sh`.
- For parser/stream changes, add new fixtures under `tests/compat/fixtures/` and matching outputs under `tests/compat/expected/`.
- Use targeted loops while developing, for example: `go test -v ./internal/adapter/openai/...`.
- For tool-call parsing/sieve changes, run `go test ./internal/util ./internal/adapter/openai -count=1`.
- Add regressions for malformed/partial DSML/XML payloads and history replay markers (`[TOOL_CALL_HISTORY]` / `[TOOL_RESULT_HISTORY]`).

## Commit & Pull Request Guidelines
- Use semantic prefixes seen in history: `feat:`, `fix:`, `docs:`, `refactor:`, `chore:`.
- Branch naming convention: `feature/<topic>` or `fix/<topic>`.
- PRs should include: change type, concise description, and test evidence (commands run/results).
- If `webui/` is modified, ensure `npm run build --prefix webui` passes before requesting review.

## Security & Configuration Tips
- Never commit real credentials or account tokens.
- Start from `config.example.json` and `.env.example` for local setup.
- Treat `config.json` and generated test artifacts as sensitive and sanitize before sharing.
