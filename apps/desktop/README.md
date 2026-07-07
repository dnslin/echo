# echo desktop

Wails 3 desktop scaffold for the echo MVP Windows client.

## Scope

This directory is the engineering skeleton for later desktop issues. It currently provides:

- a Wails 3 Go module;
- a React + TypeScript frontend;
- a focused non-product device/tray spike screen for Issue #7 risk validation;
- frontend smoke tests via Vitest;
- Windows build metadata for the MVP desktop target.

It intentionally does not implement rooms, push-to-talk, formal room voice, or LiveKit integration. The Issue #7 spike is not the formal settings page or room voice implementation.

## Commands

```bash
wails3 build
```

```bash
cd frontend
npm run test:run
```

echo v0.1 targets Windows 10 / Windows 11 x64 unless later specs expand the platform scope.
