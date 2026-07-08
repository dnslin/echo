# Implement：验证 Wails 3 + WebView2 + LiveKit 音频路径

## Pre-flight

- Base branch: `master`.
- Required new branch before Phase 2 implementation: `issue-8-wails-livekit-audio` from latest `origin/master`.
- Do not run `task.py start` until planning artifacts are reviewed and approved.
- Do not commit secrets, LiveKit tokens, or audio data.

## Ordered checklist

### 1. Branch and dependency setup

- [ ] Ensure working tree is clean except the new Trellis task planning files.
- [ ] Fetch latest remote and create `issue-8-wails-livekit-audio` from `origin/master` after planning activation.
- [ ] From `apps/desktop/frontend`, install `livekit-client`.
- [ ] Confirm `package.json` and lockfile update only expected dependency metadata.

### 2. Add LiveKit audio spike page

- [ ] Create `apps/desktop/frontend/src/spike/LiveKitAudioSpike.tsx`.
- [ ] Use `Room`, `RoomEvent`, and `Track` from `livekit-client`.
- [ ] Implement states: `idle`, `connecting`, `connected`, `failed`, `disconnected`.
- [ ] Add URL and token controls.
- [ ] Add connect/publish button.
- [ ] Add disconnect/cleanup button.
- [ ] On connect:
  - [ ] disconnect any existing room first;
  - [ ] register `Connected`, `Disconnected`, `Reconnecting`, `Reconnected`, `TrackSubscribed`, `TrackUnsubscribed`, and `AudioPlaybackStatusChanged` handlers where available;
  - [ ] call `room.connect(url, token, { autoSubscribe: true })` or equivalent;
  - [ ] call `room.localParticipant.setMicrophoneEnabled(true)`.
- [ ] On audio track subscription, call `track.attach()` and append the returned element to a dedicated container.
- [ ] On unsubscribe/disconnect/unmount, detach/remove audio elements and disconnect the room.
- [ ] Never print or persist the token.

### 3. Route app to the spike

- [ ] Modify `apps/desktop/frontend/src/App.tsx` to render `<LiveKitAudioSpike />`.
- [ ] Keep this as explicit spike routing, not a formal app shell.

### 4. Add/adjust automated coverage

- [ ] Add a lightweight smoke test if needed so `App` renders the LiveKit spike without requiring browser media or a real token.
- [ ] If LiveKit imports make jsdom tests unstable, mock only the external LiveKit module at the test boundary; do not weaken product behavior.

### 5. Record spike evidence

- [ ] Create `docs/spikes/wails-livekit-audio.md`.
- [ ] Include:
  - [ ] `Result: pass` or `Result: fail`;
  - [ ] Windows version;
  - [ ] Wails version;
  - [ ] WebView2 environment notes;
  - [ ] public LiveKit service/address category without secrets;
  - [ ] second client type, preferably official/self-hosted test page or another machine;
  - [ ] token generation source summary without token;
  - [ ] exact manual steps;
  - [ ] observed bidirectional audio result;
  - [ ] failures/limitations;
  - [ ] follow-up constraints.

### 6. Automated verification

Run from `apps/desktop/frontend`:

```bash
npm run test:run
npm run build
```

Run from `apps/desktop`:

```bash
go test ./...
wails3 build
```

If `wails3 build` fails due to environment/tooling, capture the error, retry once if transient, then document the fallback and do not claim build success.

### 7. HITL verification

Run from `apps/desktop`:

```bash
wails3 dev
```

Manual acceptance scenarios:

- [ ] Wails window opens and shows LiveKit audio spike.
- [ ] A valid LiveKit URL/token connects successfully.
- [ ] Microphone permission prompt appears if needed.
- [ ] Local microphone publishes after user action.
- [ ] Two clients join the same test LiveKit room.
- [ ] A speaks and B hears audio.
- [ ] B speaks and A hears audio.
- [ ] Disconnect cleans up the room and microphone capture.
- [ ] No token is written to docs or console output committed to repo.

### 8. Stop/fail handling

If bidirectional audio fails:

- [ ] Mark `docs/spikes/wails-livekit-audio.md` as `Result: fail`.
- [ ] Record the exact failing stage: connect, permission, publish, subscribe, playback, or other.
- [ ] Record environment and second-client type.
- [ ] State that formal media implementation must pause until design is revised.
- [ ] Do not implement a Go audio pipeline or Electron fallback.

## Risk points

- WebView2 autoplay policy may block remote audio until user gesture; connect button should count as gesture, but record if manual playback is needed.
- Token generation is outside this task; stale or room-mismatched token can cause false failure.
- Running two Wails instances on one machine may share audio devices or create feedback; the planned HITL path avoids this by preferring a public LiveKit room plus an external second client.
- Public LiveKit without TURN can fail under restrictive NAT/firewall; this task should distinguish media-path failure from network deployment failure.

## Completion gate before Phase 3

- [ ] Requirements A1-A10 in `prd.md` are either passed or explicitly marked failed with stop rule.
- [ ] `docs/spikes/wails-livekit-audio.md` is complete and non-secret.
- [ ] `trellis-check` passes.
- [ ] User has supplied or confirmed HITL bidirectional audio result.
