# Design — Fix PR 28 code review findings

## First-principles analysis

### Challenge assumptions

- Assumption: A Wails app can always embed `frontend/dist` because `wails3 build` builds frontend first. This is incomplete: Go package compilation and tests can run before Wails orchestration.
- Assumption: A health route is too small to need OpenAPI. This conflicts with the repository contract that HTTP APIs live in `services/api/openapi.yaml`.
- Assumption: Generated Wails/NSIS signing tasks are safe to leave unchanged. This is false when the wrapper points at a different file than the generator emits.
- Assumption: Bootstrap UI copy and visuals can be throwaway English/template style. This conflicts with explicit project rules and smoke tests can freeze the wrong baseline.
- Assumption: Fixing review output should include all subagent simplification suggestions. This is unverified and would expand scope beyond accepted findings.

### Bedrock truths

- Go `embed` patterns are evaluated by the Go compiler before runtime; a missing matched path is a compile-time error.
- `.gitignore` prevents generated `frontend/dist` from being present in a clean tracked checkout.
- Wails build orchestration can create `frontend/dist`, but plain `go test ./apps/desktop/...` does not run frontend build steps.
- Public HTTP routes are observable contracts; repository docs state OpenAPI is the HTTP contract source of truth.
- A signing command can only sign the exact file path that exists after packaging.
- Visible installer and app copy are user-visible UI and therefore governed by project UI copy rules.
- CSS gradients render visible visual treatment and violate the current no-gradient rule.

### Rebuild from truths

The minimum safe fix is to remove compile-time dependence on an ignored generated directory while preserving production embedding after Wails builds, add only the currently exposed HTTP route to OpenAPI, align the signing wrapper with the generator output, and replace only the violating user-visible copy/style. No broader template cleanup is required by these truths.

### Contrast with convention

A conventional scaffold cleanup might commit generated `dist`, ignore plain Go tests, or delete broad Wails template branches. Those moves are either brittle or broader than the verified bug. The fundamental constraint is not scaffold aesthetics; it is preserving executable contracts from a clean checkout.

### Conclusion

Repair the six accepted findings with small local changes: embed a tracked placeholder plus runtime-generated dist, add a minimal health OpenAPI contract, sign the actual NSIS output, and make bootstrap/installer UI comply with Chinese/no-gradient rules.

## Technical design

### D1. Desktop embed fallback

- Add a small tracked placeholder asset directory under `apps/desktop/frontend/embed/`.
- Keep `apps/desktop/main.go` focused on application setup and call an `assetHandler()` helper.
- Add build-tagged asset helpers:
  - non-production builds embed tracked fallback assets from `frontend/embed` so clean-checkout Go compile/test succeeds;
  - production builds embed generated `frontend/dist` assets after the Wails frontend build task creates them.
- Keep `apps/desktop/frontend/dist` ignored and untracked.

Rationale: Go embed patterns are compile-time and cannot be optional. Build tags match the existing Wails build contract: default `go test` uses fallback assets, while `wails3 build` already compiles with the `production` tag after generating `frontend/dist`.

Compatibility notes:

- The fallback page is only for direct Go compile/test paths before frontend build.
- The served Wails app after `wails3 build` must still use the Vite output.

### D2. Minimal OpenAPI contract

- Create `services/api/openapi.yaml` with OpenAPI 3.1 metadata and `/healthz` only.
- Define the response body as JSON object with required `status` string and example `ok`.
- Do not add future room endpoints from `implement.md` in this task.

Rationale: This repairs the actual route drift without inventing product endpoints not implemented by the PR.

### D3. Installer signing path alignment

- In `apps/desktop/build/windows/Taskfile.yml`, compute the installer path using the same shape as `project.nsi`:
  - `{{.BIN_DIR}}/{{.APP_NAME}}-{{.ARCH}}-installer.exe`
- Ensure the `sign:installer` dependency passes through `ARCH` consistently.
- Preserve certificate variables and Wails signing command shape.

Rationale: The wrapper should sign the artifact produced by its dependency, not a hard-coded non-existent location.

### D4. Chinese UI copy and installer language

- Change `App.tsx` visible copy to concise Simplified Chinese while keeping `echo` as product-name exception.
- Update `App.test.tsx` to assert the new public text.
- Change `project.nsi` MUI language to `SimpChinese`.

Rationale: These are user-visible surfaces and should not preserve English scaffold copy.

### D5. No-gradient bootstrap style

- Replace the body `radial-gradient` background with a flat dark color or simple solid treatment.
- Keep the rest of the bootstrap styling minimal.

Rationale: This satisfies the explicit no-gradient rule without redesigning the future product UI.

## Validation strategy

- Simulate clean checkout by temporarily moving/removing `apps/desktop/frontend/dist`, then run `go test ./apps/desktop/...`, and restore any build output.
- Run `go test ./services/api/...`.
- Run frontend `npm run test:run` and `npm run build`.
- Run `wails3 build` from `apps/desktop`.
- Run targeted searches for the old violating strings.
- Verify git status to ensure only intended source/planning files changed and generated outputs remain ignored.

## Rollback considerations

- If the embed fallback causes Wails runtime asset issues, revert `main.go` and use a tracked `.gitkeep`-style directory only after confirming Go embed accepts it and Wails still serves generated dist.
- If NSIS `SimpChinese` is unavailable in the local toolchain, stop and document the exact error before choosing an alternate NSIS language identifier.
- If OpenAPI scope expands accidentally, reduce it back to `/healthz` only.
