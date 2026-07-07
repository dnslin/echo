# Implement — Fix PR 28 code review findings

## Ordered checklist

### 1. Fix clean-checkout desktop embed failure

- [ ] Create tracked fallback static assets under `apps/desktop/frontend/embed/`.
- [ ] Keep `apps/desktop/main.go` focused on app setup and call an `assetHandler()` helper.
- [ ] Add build-tagged asset helper files: default builds embed fallback assets; production builds embed generated `frontend/dist` assets.
- [ ] Keep `frontend/dist` ignored and do not commit generated bundle output.

Validation for this step:

```bash
backup="$CLAUDE_JOB_DIR/tmp/echo-frontend-dist-backup"
rm -rf "$backup"
if [ -d apps/desktop/frontend/dist ]; then mv apps/desktop/frontend/dist "$backup"; fi
go test ./apps/desktop/...
if [ -d "$backup" ]; then mv "$backup" apps/desktop/frontend/dist; fi
```

### 2. Add minimal OpenAPI health contract

- [ ] Create `services/api/openapi.yaml`.
- [ ] Document only `GET /healthz` with a JSON 200 response schema containing required `status`.
- [ ] Keep WebSocket docs unchanged except if a validation issue proves otherwise.

Validation for this step:

```bash
go test ./services/api/...
```

### 3. Repair installer signing path

- [ ] Update `apps/desktop/build/windows/Taskfile.yml` `sign:installer` to sign `{{.BIN_DIR}}/{{.APP_NAME}}-{{.ARCH}}-installer.exe`.
- [ ] Pass `ARCH` through the `sign:installer` dependency and vars consistently.
- [ ] Do not add new release/signing behavior.

Static validation for this step:

```bash
rg "sign --input|OutFile" apps/desktop/build/windows
```

### 4. Fix visible UI/installer convention issues

- [ ] Change `apps/desktop/frontend/src/App.tsx` bootstrap copy to short Simplified Chinese.
- [ ] Update `apps/desktop/frontend/src/App.test.tsx` assertions.
- [ ] Replace `MUI_LANGUAGE "English"` with Simplified Chinese in `apps/desktop/build/windows/nsis/project.nsi`.
- [ ] Remove `radial-gradient` from `apps/desktop/frontend/public/style.css`.

Validation for this step:

```bash
cd apps/desktop/frontend && npm run test:run
cd apps/desktop/frontend && npm run build
```

### 5. Full validation pass

Run from repository root unless a command says otherwise:

```bash
go work sync
go test ./services/api/...
go test ./apps/desktop/... ./services/api/...
cd apps/desktop/frontend && npm run test:run
cd apps/desktop/frontend && npm run build
cd apps/desktop && wails3 build
rg "echo desktop bootstrap|Engineering skeleton ready|MUI_LANGUAGE \"English\"|radial-gradient" apps/desktop services/api docs .trellis/spec
```

Also run the clean-checkout embed simulation from step 1 after any command that recreates `frontend/dist`.

### 6. Trellis finish preparation

- [ ] Run Trellis check or equivalent code review after implementation.
- [ ] Update spec if the source-of-truth behavior changes beyond this bug fix.
- [ ] Leave generated `frontend/dist` and `bin` output untracked.
- [ ] Commit only after validation passes and the user asks/approves finalization.

## Risky files and rollback points

- `apps/desktop/main.go`: if Wails asset serving breaks, rollback only the embed selection helper and retest `wails3 build`.
- `apps/desktop/build/windows/Taskfile.yml`: if signing path templating fails, compare directly against `project.nsi:OutFile` and reduce to a single computed var.
- `apps/desktop/build/windows/nsis/project.nsi`: if NSIS rejects the language token, stop and document the exact local NSIS error before selecting an alternate.
- `services/api/openapi.yaml`: keep scope minimal; remove any unimplemented future endpoints if they appear.

## Review gates

- Do not start implementation until this task is activated with `task.py start` after artifact review.
- Do not fix unaccepted simplification/performance candidates unless one is required for an accepted finding.
- Do not introduce product feature behavior.
