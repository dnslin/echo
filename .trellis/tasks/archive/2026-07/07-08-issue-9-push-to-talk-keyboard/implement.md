# Implement: Issue #9 Push-to-talk Keyboard Spike

## Phase order

1. Prepare branch and task state.
2. Implement keyboard event contract and native hook.
3. Implement frontend keyboard spike state/UI/tests.
4. Route App to keyboard spike and update smoke tests.
5. Create spike documentation with pending HITL matrix.
6. Run automated validation.
7. Run/record Windows HITL where possible.
8. Run trellis-check and fix until green.

## Checklist

- [ ] Sync latest `master` and create a feature branch, e.g. `issue-9-push-to-talk-keyboard`.
- [ ] Add `apps/desktop/internal/keyboard/hook_nonwindows.go` no-op stub.
- [ ] Add `apps/desktop/internal/keyboard/hook_windows.go` for target-key press/release events.
- [ ] Wire the hook in `apps/desktop/main.go` while preserving existing tray behavior.
- [ ] Add `apps/desktop/frontend/src/spike/keyboardState.ts` with pure event-state reducer/helpers.
- [ ] Add `keyboardState.test.ts` covering:
  - one down/up cycle;
  - 10 consecutive down/up cycles;
  - repeated keydown ignored while pressed;
  - missing release warning;
  - non-target key ignored.
- [ ] Add `KeyboardSpike.tsx` that subscribes to native events and DOM fallback events, shows counts, recent events, target key, and manual instructions.
- [ ] Add `KeyboardSpike.test.tsx` covering visible UI, 10-cycle DOM fallback, and missing release warning.
- [ ] Update `App.tsx` to render `KeyboardSpike`.
- [ ] Update `App.test.tsx` smoke assertions for keyboard spike.
- [ ] Create `docs/spikes/push-to-talk-keyboard.md` with pending HITL result and scenario matrix.
- [ ] Run validation commands and update doc command results.
- [ ] Perform Windows HITL scenarios where available and update doc honestly.
- [ ] Run trellis-check; fix and repeat until all checks pass.

## Validation commands

From repo root unless noted:

```bash
npm --prefix apps/desktop/frontend run test:run
npm --prefix apps/desktop/frontend run build
go -C apps/desktop test ./...
cd apps/desktop && wails3 build
```

Manual Windows HITL:

```powershell
cd apps\desktop
wails3 dev
```

Manual scenarios to record:

- Ordinary desktop: focus echo window; press and release `V` 10 times; expected 10 complete cycles, no missing release.
- Borderless game foreground: switch focus to a borderless/windowed game; press and release `V` 10 times; expected native events still count 10 cycles.
- Fullscreen/exclusive game: try the same if an available game supports exclusive fullscreen; record pass/fail/not tested with reason.
- Administrator game: run a target game as administrator if available; record whether non-admin echo receives events, and whether same-permission launch changes result.
- Anti-cheat restricted game: test only if an already-installed game is safe/available; record limitation if blocked. Do not add bypass behavior.

## Risk files / rollback points

- `apps/desktop/main.go`: must preserve close-to-tray and tray quit behavior.
- `apps/desktop/internal/keyboard/hook_windows.go`: Windows-only native API risk; keep build tags correct.
- `apps/desktop/frontend/src/App.tsx`: temporary spike route; later product tasks may replace it.
- `docs/spikes/push-to-talk-keyboard.md`: must not overstate unverified manual results.

Rollback:

```bash
git checkout -- apps/desktop/main.go apps/desktop/frontend/src/App.tsx apps/desktop/frontend/src/App.test.tsx
git rm -r apps/desktop/internal/keyboard apps/desktop/frontend/src/spike/KeyboardSpike.tsx apps/desktop/frontend/src/spike/KeyboardSpike.test.tsx apps/desktop/frontend/src/spike/keyboardState.ts apps/desktop/frontend/src/spike/keyboardState.test.ts docs/spikes/push-to-talk-keyboard.md
```

Adjust the rollback command if some files already existed before this task.

## Completion gate

- All automated validation commands pass or any environment-only failure is documented with exact output and a retry/fallback.
- HITL records are honest: pass, partial, fail, or not tested per scenario.
- `trellis-check` passes after any fixes.
- No token/secret/audio content is written.
- No product scope expansion beyond Issue #9.
