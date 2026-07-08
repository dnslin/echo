# Implementation plan: push-to-talk keyboard review fixes

## Order

1. Add regression tests first.
   - Frontend state tests for native out-of-order sequence replay.
   - Frontend state/UI tests for source-isolated native vs DOM counters.
   - Frontend UI test for hook disabled status and reset flow.
   - Go tests for `keyboard.Event` sequence assignment / dispatcher seam.
2. Update Go keyboard contract.
   - Add `Sequence` to `keyboard.Event`.
   - Add hook status constants/type.
   - Add sequence assignment before Wails emit.
   - Add current hook status storage and frontend request handler in `main.go`.
3. Harden Windows hook stop path.
   - Ensure hook thread message queue exists before `Start` returns success.
   - Retry `WM_QUIT` after unhook if initial post fails.
   - Keep timeout behavior explicit.
4. Refactor frontend keyboard state.
   - Split source counters.
   - Add native pending sequence replay.
   - Add hook status reducer action and reset action.
5. Update `KeyboardSpike.tsx`.
   - Subscribe to transition and status events.
   - Emit status request on mount.
   - Render separate native/DOM cards, reset button, and unambiguous HITL steps.
6. Fix P3 documentation/manifest drift.
   - Update archived Trellis `implement.jsonl` and `check.jsonl` paths from active task path to archive path.
   - Update frontend directory-structure spec examples to use `PushToTalkEventName` and `SourceNative`.
7. Run verification and clean up.

## Validation commands

```bash
cd apps/desktop && go test ./...
cd apps/desktop/frontend && npm run test:run
cd apps/desktop/frontend && npm run build
python ./.trellis/scripts/task.py validate .trellis/tasks/archive/2026-07/07-08-issue-9-push-to-talk-keyboard
```

## Review gates

- Do not report P1 fixed until the release-before-down regression test fails on old logic and passes on new logic.
- Do not report hook status fixed unless UI test proves disabled status is visible without relying on Go logs.
- Do not report HITL wording fixed unless the page has either a reset button or explicit baseline subtraction instructions; prefer reset button for KISS.

## Rollback

- Revert source-code changes if tests fail after one focused retry and no small fix is available.
- Documentation/manifest fixes are independent and can remain if source-code rollback is needed.
