# echo MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build echo v0.1: a Windows x64 Wails 3 desktop app plus Gin/GORM/SQLite business service and self-hosted LiveKit integration for temporary 2-10 person voice rooms.

**Architecture:** Wails 3 desktop owns native Windows shell behavior, local settings, logs, and push-to-talk bridging. React/TypeScript/WebView2 owns UI and LiveKit JS media. Gin business service owns product rooms, invite codes, membership, lifecycle, WebSocket state, SQLite persistence, and short-lived LiveKit token issuance; LiveKit owns only WebRTC SFU media forwarding.

**Tech Stack:** Wails 3, Go, React, TypeScript, Vite, Tailwind CSS, LiveKit JS, Gin, GORM, SQLite, coder/websocket, OpenAPI 3.1, Docker Compose, external Nginx, GitHub Actions.

---

## 0. Execution rules

- Work in order. Do not start full feature implementation before Tasks 2-4 spike results are recorded.
- Keep commits small. Each task below includes a commit checkpoint.
- Run the listed verification command before marking a task done.
- If a command fails, fix the cause in the same task before moving on.
- Do not add account, chat, fixed room, auto-update, TURN, Redis, PostgreSQL, Electron, or server-side audio mixing.
- Keep `prd.md`, `CONTEXT.md`, `design.md`, and `docs/adr/` as source-of-truth context.

## 1. Target file structure

Create this structure during implementation:

```text
/
├── apps/
│   └── desktop/
│       ├── go.mod
│       ├── go.sum
│       ├── main.go
│       ├── internal/
│       │   ├── app/app.go
│       │   ├── config/store.go
│       │   ├── keyboard/hook_windows.go
│       │   ├── keyboard/hook_nonwindows.go
│       │   ├── logging/logger.go
│       │   └── tray/tray.go
│       └── frontend/
│           ├── package.json
│           ├── src/
│           │   ├── api/client.ts
│           │   ├── api/roomSocket.ts
│           │   ├── app/App.tsx
│           │   ├── app/routes.tsx
│           │   ├── components/
│           │   ├── livekit/livekitClient.ts
│           │   ├── state/voiceState.ts
│           │   ├── state/roomReducer.ts
│           │   ├── settings/settings.ts
│           │   └── test/
│           └── vitest.config.ts
├── services/
│   └── api/
│       ├── go.mod
│       ├── go.sum
│       ├── openapi.yaml
│       ├── cmd/api/main.go
│       └── internal/
│           ├── config/config.go
│           ├── domain/types.go
│           ├── http/router.go
│           ├── http/handlers.go
│           ├── invite/service.go
│           ├── livekit/tokens.go
│           ├── logging/logger.go
│           ├── room/service.go
│           ├── session/token.go
│           ├── store/models.go
│           ├── store/sqlite.go
│           └── ws/hub.go
├── deploy/
│   └── server/
│       ├── docker-compose.yml
│       ├── livekit.yaml
│       ├── nginx.example.conf
│       └── env.example
├── docs/
│   ├── api/websocket.md
│   └── adr/
├── .github/workflows/
│   ├── ci.yml
│   └── release-windows.yml
├── go.work
├── CONTEXT.md
├── prd.md
├── design.md
└── implement.md
```

---

## Task 1: Bootstrap repository structure and toolchain

**Files:**
- Create: `go.work`
- Create: `apps/desktop/` via `wails3 init`
- Create: `services/api/go.mod`
- Create: `services/api/cmd/api/main.go`
- Create: `services/api/internal/config/config.go`
- Create: `docs/api/websocket.md`
- Create: `deploy/server/env.example`

- [ ] **Step 1: Verify required local tools**

Run:

```powershell
wails3 version
go version
node --version
npm --version
```

Expected:

```text
wails3 version prints a Wails 3 version
go version prints Go 1.22 or newer
node --version prints Node 20 or newer
npm --version prints a version
```

- [ ] **Step 2: Scaffold desktop app**

Run from repository root:

```powershell
mkdir apps
cd apps
wails3 init -n desktop -t react-ts
cd ..
```

Expected:

```text
apps/desktop exists
apps/desktop/go.mod exists
apps/desktop/frontend/package.json exists
```

- [ ] **Step 3: Scaffold API module**

Run from repository root:

```powershell
mkdir services
mkdir services\api
cd services\api
go mod init echo/services/api
go get github.com/gin-gonic/gin gorm.io/gorm gorm.io/driver/sqlite github.com/coder/websocket github.com/coder/websocket/wsjson github.com/livekit/protocol
cd ..\..
```

Expected:

```text
services/api/go.mod exists
services/api/go.sum exists
```

- [ ] **Step 4: Create root Go workspace**

Create `go.work`:

```go
go 1.22

use (
	./apps/desktop
	./services/api
)
```

Run:

```powershell
go work sync
```

Expected:

```text
go work sync exits 0
```

- [ ] **Step 5: Add minimal API entrypoint**

Create `services/api/cmd/api/main.go`:

```go
package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	router := gin.New()
	router.Use(gin.Recovery())
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	addr := ":8080"
	logger.Info("api starting", "addr", addr)
	if err := router.Run(addr); err != nil {
		logger.Error("api stopped", "error", err.Error())
		os.Exit(1)
	}
}
```

- [ ] **Step 6: Add API config skeleton**

Create `services/api/internal/config/config.go`:

```go
package config

import "time"

type Config struct {
	HTTPAddr              string
	DatabasePath          string
	LiveKitURL            string
	LiveKitAPIKey         string
	LiveKitAPISecret      string
	RoomSessionSecret     string
	LiveKitTokenTTL       time.Duration
	ReconnectWindow       time.Duration
	EmptyRoomRetention    time.Duration
	InviteCodeLength      int
	MaxRoomMembers        int
}

func Default() Config {
	return Config{
		HTTPAddr:           ":8080",
		DatabasePath:       "./echo.sqlite3",
		LiveKitTokenTTL:    10 * time.Minute,
		ReconnectWindow:    30 * time.Second,
		EmptyRoomRetention: 30 * time.Minute,
		InviteCodeLength:   6,
		MaxRoomMembers:     10,
	}
}
```

- [ ] **Step 7: Create WebSocket contract file with concrete message names**

Create `docs/api/websocket.md`:

```markdown
# echo Room WebSocket Contract

Endpoint: `GET /v1/rooms/{room_id}/ws`

Authorization: room session token from `POST /v1/rooms` or `POST /v1/rooms/join`.

## Server to client

- `room.snapshot`
- `member.joined`
- `member.left`
- `member.reconnecting`
- `member.disconnected`
- `member.restored`
- `member.muted_changed`
- `member.speaking_changed`
- `room.expired`
- `room.error`
- `room.resync_required`
- `heartbeat.ping`

## Client to server

- `heartbeat.pong`
- `member.mute_changed`
- `member.speaking_changed`
- `member.voice_mode_changed`
- `member.leave_requested`
- `room.resync_requested`
```

- [ ] **Step 8: Verify bootstrap builds**

Run:

```powershell
go test ./services/api/...
cd apps\desktop
wails3 build
cd ..\..
```

Expected:

```text
go test exits 0
wails3 build exits 0 and creates a desktop binary under apps/desktop/bin or Wails build output directory
```

- [ ] **Step 9: Commit**

```powershell
git add go.work apps services docs/api deploy implement.md
git commit -m "chore: bootstrap echo workspace"
```

Rollback:

```powershell
git reset --hard HEAD~1
```

---

## Task 2: Spike Wails 3 WebView2 + LiveKit JS audio path

**Files:**
- Modify: `apps/desktop/frontend/package.json`
- Create: `apps/desktop/frontend/src/spike/LiveKitAudioSpike.tsx`
- Modify: `apps/desktop/frontend/src/app/App.tsx`
- Create: `docs/spikes/wails-livekit-audio.md`

- [ ] **Step 1: Install LiveKit JS dependencies**

Run:

```powershell
cd apps\desktop\frontend
npm install livekit-client @livekit/components-react
cd ..\..\..
```

Expected:

```text
apps/desktop/frontend/package.json includes livekit-client and @livekit/components-react
```

- [ ] **Step 2: Add spike component**

Create `apps/desktop/frontend/src/spike/LiveKitAudioSpike.tsx`:

```tsx
import { useState } from 'react';
import { Room, RoomEvent, Track } from 'livekit-client';

type ConnectionState = 'idle' | 'connecting' | 'connected' | 'failed';

export function LiveKitAudioSpike() {
  const [url, setUrl] = useState('wss://livekit.example.com');
  const [token, setToken] = useState('');
  const [state, setState] = useState<ConnectionState>('idle');
  const [error, setError] = useState('');

  async function connect() {
    setState('connecting');
    setError('');
    const room = new Room({ adaptiveStream: false, dynacast: false });
    room.on(RoomEvent.TrackSubscribed, (track) => {
      if (track.kind === Track.Kind.Audio) {
        const element = track.attach();
        element.autoplay = true;
        document.body.appendChild(element);
      }
    });
    try {
      await room.connect(url, token);
      await room.localParticipant.setMicrophoneEnabled(true);
      setState('connected');
      (window as unknown as { echoLiveKitSpikeRoom?: Room }).echoLiveKitSpikeRoom = room;
    } catch (err) {
      setState('failed');
      setError(err instanceof Error ? err.message : String(err));
    }
  }

  return (
    <main className="p-6 space-y-4">
      <h1 className="text-xl font-semibold">LiveKit Audio Spike</h1>
      <label className="block">
        <span>LiveKit URL</span>
        <input className="block w-full border px-2 py-1" value={url} onChange={(event) => setUrl(event.target.value)} />
      </label>
      <label className="block">
        <span>Token</span>
        <textarea className="block w-full border px-2 py-1" value={token} onChange={(event) => setToken(event.target.value)} />
      </label>
      <button className="rounded bg-blue-600 px-3 py-2 text-white" onClick={connect}>Connect and publish microphone</button>
      <p data-testid="state">State: {state}</p>
      {error && <pre className="text-red-600">{error}</pre>}
    </main>
  );
}
```

- [ ] **Step 3: Temporarily route app to spike**

Modify `apps/desktop/frontend/src/app/App.tsx` to render the spike component while Task 2 runs:

```tsx
import { LiveKitAudioSpike } from '../spike/LiveKitAudioSpike';

export default function App() {
  return <LiveKitAudioSpike />;
}
```

- [ ] **Step 4: Run desktop dev spike**

Run:

```powershell
cd apps\desktop
wails3 dev
```

Manual expected result:

```text
Wails window opens
Spike page renders
A valid LiveKit URL and token can connect
Microphone permission prompt appears if needed
Local participant publishes microphone audio
A second client in the same LiveKit room can hear the desktop client
```

- [ ] **Step 5: Record spike result**

Create `docs/spikes/wails-livekit-audio.md`:

```markdown
# Wails 3 + WebView2 + LiveKit Audio Spike

Result: pass

Validated on Windows x64:

- Wails 3 app launched with WebView2.
- LiveKit JS connected to the configured LiveKit room.
- Local microphone track published.
- Remote audio track subscribed and played.
- App logs captured connection failures without exposing token plaintext.

Follow-up constraints:

- Keep audio capture/playback in WebView2 + LiveKit JS.
- Do not add a Go audio capture/playback pipeline.
```

If the spike fails, set `Result: fail`, include the failing condition and stop implementation before Task 5.

- [ ] **Step 6: Commit passing spike**

```powershell
git add apps/desktop/frontend/package.json apps/desktop/frontend/package-lock.json apps/desktop/frontend/src docs/spikes
git commit -m "spike: validate wails livekit audio"
```

Rollback:

```powershell
git reset --hard HEAD~1
```

---

## Task 3: Spike device selection and tray persistence

**Files:**
- Create: `apps/desktop/frontend/src/spike/DeviceSpike.tsx`
- Modify: `apps/desktop/frontend/src/app/App.tsx`
- Create: `docs/spikes/device-tray.md`

- [ ] **Step 1: Add device spike component**

Create `apps/desktop/frontend/src/spike/DeviceSpike.tsx`:

```tsx
import { useEffect, useState } from 'react';

type DeviceInfo = { deviceId: string; label: string; kind: MediaDeviceKind };

export function DeviceSpike() {
  const [devices, setDevices] = useState<DeviceInfo[]>([]);
  const [error, setError] = useState('');

  async function refresh() {
    setError('');
    try {
      await navigator.mediaDevices.getUserMedia({ audio: true });
      const all = await navigator.mediaDevices.enumerateDevices();
      setDevices(all.filter((device) => device.kind === 'audioinput' || device.kind === 'audiooutput').map((device) => ({
        deviceId: device.deviceId,
        label: device.label || '(unlabeled device)',
        kind: device.kind,
      })));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }

  useEffect(() => {
    void refresh();
  }, []);

  return (
    <main className="p-6 space-y-4">
      <h1 className="text-xl font-semibold">Device Spike</h1>
      <button className="rounded bg-blue-600 px-3 py-2 text-white" onClick={refresh}>Refresh devices</button>
      {error && <pre className="text-red-600">{error}</pre>}
      <ul className="space-y-2">
        {devices.map((device) => (
          <li key={`${device.kind}:${device.deviceId}`} className="rounded border p-2">
            <strong>{device.kind}</strong> — {device.label}
          </li>
        ))}
      </ul>
    </main>
  );
}
```

- [ ] **Step 2: Route app to device spike**

Modify `apps/desktop/frontend/src/app/App.tsx`:

```tsx
import { DeviceSpike } from '../spike/DeviceSpike';

export default function App() {
  return <DeviceSpike />;
}
```

- [ ] **Step 3: Run spike with tray hide**

Run:

```powershell
cd apps\desktop
wails3 dev
```

Manual expected result:

```text
audioinput devices are listed after permission grant
audiooutput devices are listed if WebView2 exposes them
closing the main window hides the app to tray after tray wiring is available in Task 4
LiveKit audio from Task 2 remains active while hidden once tray lifecycle is wired
```

- [ ] **Step 4: Record output device decision**

Create `docs/spikes/device-tray.md`:

```markdown
# Device and Tray Spike

Result: pass

Validated:

- Microphone enumeration works in Wails 3 WebView2.
- Microphone permission can be requested from frontend code.
- Output device enumeration result: supported.
- Tray-hide persistence will be verified again after native tray wiring.

Accepted product behavior:

- If output device switching remains supported, expose it in settings.
- If output device switching fails in final verification, use system default output and keep a visible product note before shipping v0.1.
```

If output devices are not exposed, set `Output device enumeration result: unsupported` and stop before implementing output device selection UI.

- [ ] **Step 5: Commit spike**

```powershell
git add apps/desktop/frontend/src docs/spikes
git commit -m "spike: validate webview audio devices"
```

---

## Task 4: Spike push-to-talk press/release in foreground games

**Files:**
- Create: `apps/desktop/internal/keyboard/hook_windows.go`
- Create: `apps/desktop/internal/keyboard/hook_nonwindows.go`
- Create: `apps/desktop/frontend/src/spike/KeyboardSpike.tsx`
- Modify: `apps/desktop/frontend/src/app/App.tsx`
- Create: `docs/spikes/push-to-talk-keyboard.md`

- [ ] **Step 1: Add keyboard event contract**

Create `apps/desktop/internal/keyboard/hook_nonwindows.go`:

```go
//go:build !windows

package keyboard

type Event struct {
	Key     string `json:"key"`
	Pressed bool   `json:"pressed"`
}

type Hook struct{}

func NewHook(_ func(Event)) *Hook { return &Hook{} }
func (h *Hook) Start() error      { return nil }
func (h *Hook) Stop()             {}
```

- [ ] **Step 2: Add Windows hook shell**

Create `apps/desktop/internal/keyboard/hook_windows.go`:

```go
//go:build windows

package keyboard

type Event struct {
	Key     string `json:"key"`
	Pressed bool   `json:"pressed"`
}

type Hook struct {
	onEvent func(Event)
}

func NewHook(onEvent func(Event)) *Hook {
	return &Hook{onEvent: onEvent}
}

func (h *Hook) Start() error {
	// First implementation may use Wails GlobalShortcut. If it cannot emit release events,
	// replace this body with a Windows WH_KEYBOARD_LL hook in the same package.
	return nil
}

func (h *Hook) Stop() {}

func (h *Hook) emit(key string, pressed bool) {
	if h.onEvent != nil {
		h.onEvent(Event{Key: key, Pressed: pressed})
	}
}
```

- [ ] **Step 3: Add frontend keyboard spike page**

Create `apps/desktop/frontend/src/spike/KeyboardSpike.tsx`:

```tsx
import { useEffect, useState } from 'react';

type KeyEvent = { key: string; pressed: boolean };

export function KeyboardSpike() {
  const [events, setEvents] = useState<KeyEvent[]>([]);

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.key.toLowerCase() === 'v') setEvents((prev) => [{ key: 'V', pressed: true }, ...prev].slice(0, 20));
    }
    function onKeyUp(event: KeyboardEvent) {
      if (event.key.toLowerCase() === 'v') setEvents((prev) => [{ key: 'V', pressed: false }, ...prev].slice(0, 20));
    }
    window.addEventListener('keydown', onKeyDown);
    window.addEventListener('keyup', onKeyUp);
    return () => {
      window.removeEventListener('keydown', onKeyDown);
      window.removeEventListener('keyup', onKeyUp);
    };
  }, []);

  return (
    <main className="p-6 space-y-4">
      <h1 className="text-xl font-semibold">Push-to-talk Keyboard Spike</h1>
      <p>Press and release V while this app is focused, then repeat while a game is foregrounded after native hook wiring.</p>
      <ul className="font-mono">
        {events.map((event, index) => <li key={index}>{event.key} {event.pressed ? 'down' : 'up'}</li>)}
      </ul>
    </main>
  );
}
```

- [ ] **Step 4: Route app to keyboard spike**

Modify `apps/desktop/frontend/src/app/App.tsx`:

```tsx
import { KeyboardSpike } from '../spike/KeyboardSpike';

export default function App() {
  return <KeyboardSpike />;
}
```

- [ ] **Step 5: Validate foreground game behavior**

Run:

```powershell
cd apps\desktop
wails3 dev
```

Manual expected result:

```text
V key down and up events are visible while app is focused
native hook implementation emits V pressed/released while a game is foregrounded
10 consecutive press/release cycles produce no missing release event
```

- [ ] **Step 6: Record spike result**

Create `docs/spikes/push-to-talk-keyboard.md`:

```markdown
# Push-to-talk Keyboard Spike

Result: pass

Validated:

- Default shortcut V produced press and release events.
- Press/release state worked while a foreground game was active.
- 10 consecutive press/release cycles had no missing release event.

Implementation path:

- Use Wails shortcut events if they provide reliable press/release semantics.
- Use Windows low-level keyboard hook if Wails shortcut events cannot satisfy press/release behavior.
```

- [ ] **Step 7: Commit spike**

```powershell
git add apps/desktop/internal/keyboard apps/desktop/frontend/src docs/spikes
git commit -m "spike: validate push to talk keyboard events"
```

---

## Task 5: Implement API domain types and invite code service with tests

**Files:**
- Create: `services/api/internal/domain/types.go`
- Create: `services/api/internal/invite/service.go`
- Create: `services/api/internal/invite/service_test.go`

- [ ] **Step 1: Write invite service tests**

Create `services/api/internal/invite/service_test.go`:

```go
package invite

import "testing"

func TestNormalize(t *testing.T) {
	got, err := Normalize(" k7-m9 q2 ")
	if err != nil { t.Fatalf("Normalize returned error: %v", err) }
	if got != "K7M9Q2" { t.Fatalf("got %q", got) }
}

func TestNormalizeRejectsInvalidLength(t *testing.T) {
	_, err := Normalize("ABC")
	if err == nil { t.Fatal("expected error") }
}

func TestNormalizeRejectsInvalidCharacters(t *testing.T) {
	_, err := Normalize("ABC!23")
	if err == nil { t.Fatal("expected error") }
}

func TestGenerateUsesAlphabet(t *testing.T) {
	code, err := Generate(6)
	if err != nil { t.Fatalf("Generate returned error: %v", err) }
	if len(code) != 6 { t.Fatalf("len=%d", len(code)) }
	for _, r := range code {
		if !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') {
			t.Fatalf("invalid rune %q in %q", r, code)
		}
	}
}
```

- [ ] **Step 2: Run tests and verify failure**

Run:

```powershell
cd services\api
go test ./internal/invite -run TestNormalize -v
```

Expected:

```text
FAIL because Normalize and Generate are undefined
```

- [ ] **Step 3: Add domain types**

Create `services/api/internal/domain/types.go`:

```go
package domain

import "time"

type RoomState string

const (
	RoomStateActive  RoomState = "active"
	RoomStateExpired RoomState = "expired"
)

type MemberState string

const (
	MemberStateOnline       MemberState = "online"
	MemberStateReconnecting MemberState = "reconnecting"
	MemberStateDisconnected MemberState = "disconnected"
)

type VoiceMode string

const (
	VoiceModePushToTalk VoiceMode = "push_to_talk"
	VoiceModeFreeTalk   VoiceMode = "free_talk"
)

type Room struct {
	ID                string
	Name              string
	InviteCode        string
	LiveKitRoomName   string
	HostAnonymousID   string
	HostNickname      string
	HostAvatarID      string
	State             RoomState
	CreatedAt         time.Time
	LastEmptyAt       *time.Time
	ExpiresAt         *time.Time
}

type Member struct {
	ID              string
	RoomID          string
	AnonymousID     string
	Nickname        string
	AvatarID        string
	IsHost          bool
	State           MemberState
	Muted           bool
	Speaking        bool
	VoiceMode       VoiceMode
	JoinedAt        time.Time
	ReconnectUntil  *time.Time
	LiveKitIdentity string
}
```

- [ ] **Step 4: Implement invite service**

Create `services/api/internal/invite/service.go`:

```go
package invite

import (
	"crypto/rand"
	"errors"
	"math/big"
	"strings"
	"unicode"
)

const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

var ErrInvalidInviteCode = errors.New("invalid invite code")

func Normalize(input string) (string, error) {
	var builder strings.Builder
	for _, r := range input {
		if r == ' ' || r == '-' { continue }
		upper := unicode.ToUpper(r)
		if !((upper >= 'A' && upper <= 'Z') || (upper >= '0' && upper <= '9')) {
			return "", ErrInvalidInviteCode
		}
		builder.WriteRune(upper)
	}
	code := builder.String()
	if len(code) != 6 { return "", ErrInvalidInviteCode }
	return code, nil
}

func Generate(length int) (string, error) {
	if length <= 0 { return "", ErrInvalidInviteCode }
	var builder strings.Builder
	max := big.NewInt(int64(len(alphabet)))
	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil { return "", err }
		builder.WriteByte(alphabet[n.Int64()])
	}
	return builder.String(), nil
}
```

- [ ] **Step 5: Verify invite tests pass**

Run:

```powershell
cd services\api
go test ./internal/invite -v
```

Expected:

```text
PASS
```

- [ ] **Step 6: Commit**

```powershell
git add services/api/internal/domain services/api/internal/invite
git commit -m "feat(api): add invite code domain rules"
```

---

## Task 6: Implement SQLite persistence models and migrations

**Files:**
- Create: `services/api/internal/store/models.go`
- Create: `services/api/internal/store/sqlite.go`
- Create: `services/api/internal/store/sqlite_test.go`

- [ ] **Step 1: Write persistence tests**

Create `services/api/internal/store/sqlite_test.go`:

```go
package store

import (
	"testing"
	"time"
)

func TestOpenMigratesRooms(t *testing.T) {
	db, err := OpenSQLite("file::memory:?cache=shared")
	if err != nil { t.Fatalf("OpenSQLite: %v", err) }
	var count int64
	if err := db.Model(&RoomModel{}).Count(&count).Error; err != nil { t.Fatalf("count rooms: %v", err) }
}

func TestRoomPersistsExpiryFields(t *testing.T) {
	db, err := OpenSQLite("file::memory:?cache=shared")
	if err != nil { t.Fatalf("OpenSQLite: %v", err) }
	now := time.Now().UTC()
	expires := now.Add(30 * time.Minute)
	room := RoomModel{ID: "room_1", Name: "Duo", InviteCode: "ABC123", LiveKitRoomName: "lk_room_1", HostAnonymousID: "anon_1", HostNickname: "A", HostAvatarID: "avatar_1", State: "active", CreatedAt: now, ExpiresAt: &expires}
	if err := db.Create(&room).Error; err != nil { t.Fatalf("create room: %v", err) }
	var got RoomModel
	if err := db.First(&got, "id = ?", "room_1").Error; err != nil { t.Fatalf("load room: %v", err) }
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(expires) { t.Fatalf("expires mismatch: %#v", got.ExpiresAt) }
}
```

- [ ] **Step 2: Run tests and verify failure**

Run:

```powershell
cd services\api
go test ./internal/store -v
```

Expected:

```text
FAIL because OpenSQLite and RoomModel are undefined
```

- [ ] **Step 3: Add GORM models**

Create `services/api/internal/store/models.go`:

```go
package store

import "time"

type RoomModel struct {
	ID              string     `gorm:"primaryKey;size:64"`
	Name            string     `gorm:"size:24;not null"`
	InviteCode      string     `gorm:"size:6;not null;index:idx_rooms_invite_code,unique"`
	LiveKitRoomName string     `gorm:"size:96;not null"`
	HostAnonymousID string     `gorm:"size:96;not null"`
	HostNickname    string     `gorm:"size:16;not null"`
	HostAvatarID    string     `gorm:"size:64;not null"`
	State           string     `gorm:"size:16;not null;index"`
	CreatedAt       time.Time  `gorm:"not null"`
	LastEmptyAt     *time.Time `gorm:"index"`
	ExpiresAt       *time.Time `gorm:"index"`
	UpdatedAt       time.Time
}

func (RoomModel) TableName() string { return "rooms" }
```

- [ ] **Step 4: Add SQLite opener and migration**

Create `services/api/internal/store/sqlite.go`:

```go
package store

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func OpenSQLite(path string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil { return nil, err }
	if err := db.AutoMigrate(&RoomModel{}); err != nil { return nil, err }
	return db, nil
}
```

- [ ] **Step 5: Verify persistence tests pass**

Run:

```powershell
cd services\api
go test ./internal/store -v
```

Expected:

```text
PASS
```

- [ ] **Step 6: Commit**

```powershell
git add services/api/internal/store
git commit -m "feat(api): add sqlite room persistence"
```

---

## Task 7: Implement room lifecycle service with tests

**Files:**
- Create: `services/api/internal/room/service.go`
- Create: `services/api/internal/room/service_test.go`

- [ ] **Step 1: Write lifecycle tests**

Create `services/api/internal/room/service_test.go`:

```go
package room

import (
	"testing"
	"time"

	"echo/services/api/internal/config"
	"echo/services/api/internal/store"
)

func TestCreateJoinCapacityAndExpiry(t *testing.T) {
	db, err := store.OpenSQLite("file::memory:?cache=shared")
	if err != nil { t.Fatalf("db: %v", err) }
	cfg := config.Default()
	cfg.MaxRoomMembers = 2
	service := NewService(db, cfg, func() time.Time { return time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC) })

	created, err := service.Create(CreateInput{AnonymousID: "anon_a", Nickname: "A", AvatarID: "avatar_a", RoomName: "Duo"})
	if err != nil { t.Fatalf("create: %v", err) }
	if created.Member.IsHost != true { t.Fatal("creator should be host") }

	joined, err := service.Join(JoinInput{InviteCode: created.Room.InviteCode, AnonymousID: "anon_b", Nickname: "B", AvatarID: "avatar_b"})
	if err != nil { t.Fatalf("join: %v", err) }
	if joined.Member.ID == created.Member.ID { t.Fatal("members should differ") }

	_, err = service.Join(JoinInput{InviteCode: created.Room.InviteCode, AnonymousID: "anon_c", Nickname: "C", AvatarID: "avatar_c"})
	if err != ErrRoomFull { t.Fatalf("expected ErrRoomFull, got %v", err) }
}

func TestLeaveStartsEmptyRoomRetention(t *testing.T) {
	db, err := store.OpenSQLite("file::memory:?cache=shared")
	if err != nil { t.Fatalf("db: %v", err) }
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	service := NewService(db, config.Default(), func() time.Time { return now })
	created, err := service.Create(CreateInput{AnonymousID: "anon_a", Nickname: "A", AvatarID: "avatar_a", RoomName: "Duo"})
	if err != nil { t.Fatalf("create: %v", err) }
	if err := service.Leave(created.Room.ID, created.Member.ID); err != nil { t.Fatalf("leave: %v", err) }
	room, err := service.Get(created.Room.ID)
	if err != nil { t.Fatalf("get: %v", err) }
	if room.ExpiresAt == nil || !room.ExpiresAt.Equal(now.Add(30*time.Minute)) { t.Fatalf("expires_at not set correctly: %#v", room.ExpiresAt) }
}

func TestReconnectWindowRestoresOriginalMember(t *testing.T) {
	db, err := store.OpenSQLite("file::memory:?cache=shared")
	if err != nil { t.Fatalf("db: %v", err) }
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	service := NewService(db, config.Default(), func() time.Time { return now })
	created, err := service.Create(CreateInput{AnonymousID: "anon_a", Nickname: "A", AvatarID: "avatar_a", RoomName: "Duo"})
	if err != nil { t.Fatalf("create: %v", err) }
	if err := service.MarkReconnecting(created.Room.ID, created.Member.ID); err != nil { t.Fatalf("reconnecting: %v", err) }
	restored, err := service.Join(JoinInput{InviteCode: created.Room.InviteCode, AnonymousID: "anon_a", Nickname: "A", AvatarID: "avatar_a"})
	if err != nil { t.Fatalf("rejoin: %v", err) }
	if restored.Member.ID != created.Member.ID { t.Fatalf("member id changed: %s != %s", restored.Member.ID, created.Member.ID) }
}
```

- [ ] **Step 2: Run test and verify failure**

Run:

```powershell
cd services\api
go test ./internal/room -run TestCreateJoinCapacityAndExpiry -v
```

Expected:

```text
FAIL because NewService and input types are undefined
```

- [ ] **Step 3: Implement room service contracts**

Create `services/api/internal/room/service.go` with these public contracts and implement every method exercised by `service_test.go`:

```go
package room

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"echo/services/api/internal/config"
	"echo/services/api/internal/domain"
	"echo/services/api/internal/invite"
	"echo/services/api/internal/store"
	"gorm.io/gorm"
)

var ErrRoomFull = errors.New("room full")
var ErrRoomNotFound = errors.New("room not found")
var ErrRoomExpired = errors.New("room expired")

type CreateInput struct { AnonymousID, Nickname, AvatarID, RoomName string }
type JoinInput struct { InviteCode, AnonymousID, Nickname, AvatarID string }
type Result struct { Room domain.Room; Member domain.Member }

type Service struct { db *gorm.DB; cfg config.Config; now func() time.Time; online map[string]map[string]domain.Member }

func NewService(db *gorm.DB, cfg config.Config, now func() time.Time) *Service {
	if now == nil { now = func() time.Time { return time.Now().UTC() } }
	return &Service{db: db, cfg: cfg, now: now, online: map[string]map[string]domain.Member{}}
}

func (s *Service) Create(input CreateInput) (Result, error) {
	roomID := "room_" + randomHex(8)
	memberID := "mem_" + randomHex(8)
	code, err := invite.Generate(s.cfg.InviteCodeLength)
	if err != nil { return Result{}, err }
	now := s.now()
	model := store.RoomModel{ID: roomID, Name: input.RoomName, InviteCode: code, LiveKitRoomName: "lk_" + roomID, HostAnonymousID: input.AnonymousID, HostNickname: input.Nickname, HostAvatarID: input.AvatarID, State: string(domain.RoomStateActive), CreatedAt: now}
	if err := s.db.Create(&model).Error; err != nil { return Result{}, err }
	member := domain.Member{ID: memberID, RoomID: roomID, AnonymousID: input.AnonymousID, Nickname: input.Nickname, AvatarID: input.AvatarID, IsHost: true, State: domain.MemberStateOnline, VoiceMode: domain.VoiceModePushToTalk, JoinedAt: now, LiveKitIdentity: memberID}
	s.online[roomID] = map[string]domain.Member{memberID: member}
	return Result{Room: toDomainRoom(model), Member: member}, nil
}

func (s *Service) Join(input JoinInput) (Result, error) {
	code, err := invite.Normalize(input.InviteCode)
	if err != nil { return Result{}, err }
	var model store.RoomModel
	if err := s.db.First(&model, "invite_code = ? AND state = ?", code, string(domain.RoomStateActive)).Error; err != nil { return Result{}, ErrRoomNotFound }
	if model.ExpiresAt != nil && !s.now().Before(*model.ExpiresAt) { model.State = string(domain.RoomStateExpired); _ = s.db.Save(&model).Error; return Result{}, ErrRoomExpired }
	members := s.online[model.ID]
	if members == nil { members = map[string]domain.Member{}; s.online[model.ID] = members }
	if len(members) >= s.cfg.MaxRoomMembers { return Result{}, ErrRoomFull }
	memberID := "mem_" + randomHex(8)
	member := domain.Member{ID: memberID, RoomID: model.ID, AnonymousID: input.AnonymousID, Nickname: input.Nickname, AvatarID: input.AvatarID, State: domain.MemberStateOnline, VoiceMode: domain.VoiceModePushToTalk, JoinedAt: s.now(), LiveKitIdentity: memberID}
	members[memberID] = member
	return Result{Room: toDomainRoom(model), Member: member}, nil
}

func toDomainRoom(model store.RoomModel) domain.Room {
	return domain.Room{ID: model.ID, Name: model.Name, InviteCode: model.InviteCode, LiveKitRoomName: model.LiveKitRoomName, HostAnonymousID: model.HostAnonymousID, HostNickname: model.HostNickname, HostAvatarID: model.HostAvatarID, State: domain.RoomState(model.State), CreatedAt: model.CreatedAt, LastEmptyAt: model.LastEmptyAt, ExpiresAt: model.ExpiresAt}
}

func randomHex(bytes int) string { b := make([]byte, bytes); _, _ = rand.Read(b); return hex.EncodeToString(b) }
```

The same file must also implement these methods:

```go
func (s *Service) Get(roomID string) (domain.Room, error)
func (s *Service) Leave(roomID string, memberID string) error
func (s *Service) MarkReconnecting(roomID string, memberID string) error
func (s *Service) ExpireReconnects() error
func (s *Service) ExpireEmptyRooms() error
```

Required behavior:

- `Leave` removes the member from in-memory online state and starts `last_empty_at` / `expires_at` when the room becomes empty.
- `MarkReconnecting` keeps the member slot occupied, sets state to `reconnecting`, clears speaking state, and records `ReconnectUntil = now + 30 seconds`.
- `Join` restores the original member when the same `anonymous_id` rejoins during the reconnect window.
- `ExpireReconnects` removes members whose reconnect window has elapsed, then starts empty-room retention if the room becomes empty.
- `ExpireEmptyRooms` marks rooms expired when `expires_at <= now`.
- Create retries invite code generation on SQLite invite-code uniqueness conflicts up to 5 attempts.

- [ ] **Step 4: Verify room tests pass**

Run:

```powershell
cd services\api
go test ./internal/room -v
```

Expected:

```text
PASS
```

- [ ] **Step 5: Commit**

```powershell
git add services/api/internal/room
git commit -m "feat(api): add room lifecycle service"
```

---

## Task 8: Implement session tokens and LiveKit token service

**Files:**
- Create: `services/api/internal/session/token.go`
- Create: `services/api/internal/session/token_test.go`
- Create: `services/api/internal/livekit/tokens.go`
- Create: `services/api/internal/livekit/tokens_test.go`

- [ ] **Step 1: Write session token tests**

Create `services/api/internal/session/token_test.go`:

```go
package session

import "testing"

func TestSignAndVerify(t *testing.T) {
	token, err := Sign("secret", Claims{RoomID: "room_1", MemberID: "mem_1"})
	if err != nil { t.Fatalf("Sign: %v", err) }
	claims, err := Verify("secret", token)
	if err != nil { t.Fatalf("Verify: %v", err) }
	if claims.RoomID != "room_1" || claims.MemberID != "mem_1" { t.Fatalf("claims mismatch: %#v", claims) }
}

func TestVerifyRejectsWrongSecret(t *testing.T) {
	token, err := Sign("secret_a", Claims{RoomID: "room_1", MemberID: "mem_1"})
	if err != nil { t.Fatalf("Sign: %v", err) }
	_, err = Verify("secret_b", token)
	if err == nil { t.Fatal("expected error") }
}
```

- [ ] **Step 2: Implement HMAC room session token**

Create `services/api/internal/session/token.go`:

```go
package session

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

type Claims struct { RoomID string `json:"room_id"`; MemberID string `json:"member_id"` }

func Sign(secret string, claims Claims) (string, error) {
	body, err := json.Marshal(claims)
	if err != nil { return "", err }
	payload := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + sig, nil
}

func Verify(secret, token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 { return Claims{}, errors.New("invalid token") }
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0]))
	expected := mac.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil { return Claims{}, err }
	if !hmac.Equal(got, expected) { return Claims{}, errors.New("invalid signature") }
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil { return Claims{}, err }
	var claims Claims
	if err := json.Unmarshal(body, &claims); err != nil { return Claims{}, err }
	return claims, nil
}
```

- [ ] **Step 3: Implement LiveKit token wrapper**

Create `services/api/internal/livekit/tokens.go`:

```go
package livekit

import (
	"time"

	"github.com/livekit/protocol/auth"
)

type TokenInput struct { APIKey, APISecret, RoomName, Identity, Name string; ValidFor time.Duration }

func JoinToken(input TokenInput) (string, error) {
	at := auth.NewAccessToken(input.APIKey, input.APISecret)
	at.SetIdentity(input.Identity)
	at.SetName(input.Name)
	at.SetValidFor(input.ValidFor)
	at.AddGrant(&auth.VideoGrant{RoomJoin: true, Room: input.RoomName})
	return at.ToJWT()
}
```

- [ ] **Step 4: Verify token tests pass**

Run:

```powershell
cd services\api
go test ./internal/session ./internal/livekit -v
```

Expected:

```text
PASS
```

- [ ] **Step 5: Commit**

```powershell
git add services/api/internal/session services/api/internal/livekit
git commit -m "feat(api): add room and livekit tokens"
```

---

## Task 9: Implement HTTP API and OpenAPI contract

**Files:**
- Create: `services/api/openapi.yaml`
- Create: `services/api/internal/http/router.go`
- Create: `services/api/internal/http/handlers.go`
- Create: `services/api/internal/http/handlers_test.go`
- Modify: `services/api/cmd/api/main.go`

- [ ] **Step 1: Write OpenAPI contract**

Create `services/api/openapi.yaml` with these paths:

```yaml
openapi: 3.1.0
info:
  title: echo API
  version: 0.1.0
paths:
  /healthz:
    get:
      responses:
        '200':
          description: healthy
  /v1/rooms:
    post:
      summary: Create temporary room
      responses:
        '201':
          description: room created
  /v1/rooms/join:
    post:
      summary: Join temporary room by invite code
      responses:
        '200':
          description: joined room
        '404':
          description: invite invalid or expired
        '409':
          description: room full
  /v1/rooms/{room_id}:
    get:
      summary: Get room snapshot
      parameters:
        - name: room_id
          in: path
          required: true
          schema: { type: string }
      responses:
        '200': { description: room snapshot }
  /v1/rooms/{room_id}/leave:
    post:
      summary: Leave room
      parameters:
        - name: room_id
          in: path
          required: true
          schema: { type: string }
      responses:
        '204': { description: left room }
  /v1/rooms/{room_id}/livekit-token:
    post:
      summary: Issue fresh LiveKit token
      parameters:
        - name: room_id
          in: path
          required: true
          schema: { type: string }
      responses:
        '200': { description: token issued }
```

- [ ] **Step 2: Write handler tests**

Create `services/api/internal/http/handlers_test.go`:

```go
package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"echo/services/api/internal/config"
	"echo/services/api/internal/room"
	"echo/services/api/internal/store"
)

func TestCreateRoomHTTP(t *testing.T) {
	db, err := store.OpenSQLite("file::memory:?cache=shared")
	if err != nil { t.Fatalf("db: %v", err) }
	cfg := config.Default(); cfg.RoomSessionSecret = "test-secret"; cfg.LiveKitAPIKey = "devkey"; cfg.LiveKitAPISecret = "secret"
	router := NewRouter(NewHandlers(room.NewService(db, cfg, nil), cfg))
	body := bytes.NewBufferString(`{"anonymous_id":"anon_1","nickname":"Alice","avatar_id":"avatar_1","room_name":"Duo"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/rooms", body)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusCreated { t.Fatalf("status=%d body=%s", res.Code, res.Body.String()) }
	var payload map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil { t.Fatalf("json: %v", err) }
	if payload["invite_code"] == "" { t.Fatalf("missing invite_code: %#v", payload) }
}
```

- [ ] **Step 3: Implement router and handlers**

Create `services/api/internal/http/router.go`:

```go
package httpapi

import "github.com/gin-gonic/gin"

func NewRouter(handlers *Handlers) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/healthz", handlers.Health)
	v1 := r.Group("/v1")
	v1.POST("/rooms", handlers.CreateRoom)
	v1.POST("/rooms/join", handlers.JoinRoom)
	v1.GET("/rooms/:room_id", handlers.GetRoom)
	v1.POST("/rooms/:room_id/leave", handlers.LeaveRoom)
	v1.POST("/rooms/:room_id/livekit-token", handlers.LiveKitToken)
	return r
}
```

Create `services/api/internal/http/handlers.go` with request/response structs and handler methods. Required response fields for create/join:

```go
type RoomResponse struct {
	RoomID           string `json:"room_id"`
	InviteCode       string `json:"invite_code"`
	MemberID         string `json:"member_id"`
	RoomSessionToken string `json:"room_session_token"`
	LiveKitURL       string `json:"livekit_url"`
	LiveKitToken     string `json:"livekit_token"`
}
```

- [ ] **Step 4: Wire main to real router**

Modify `services/api/cmd/api/main.go` to:

- load `config.Default()` plus environment overrides
- open SQLite with `store.OpenSQLite`
- create `room.Service`
- create `httpapi.NewRouter`
- run on configured address

- [ ] **Step 5: Verify API tests pass**

Run:

```powershell
cd services\api
go test ./internal/http -v
go test ./...
```

Expected:

```text
PASS
```

- [ ] **Step 6: Commit**

```powershell
git add services/api/openapi.yaml services/api/internal/http services/api/cmd/api/main.go
git commit -m "feat(api): add room HTTP API"
```

---

## Task 10: Implement WebSocket room hub

**Files:**
- Create: `services/api/internal/ws/hub.go`
- Create: `services/api/internal/ws/hub_test.go`
- Modify: `services/api/internal/http/router.go`
- Modify: `services/api/internal/http/handlers.go`
- Modify: `docs/api/websocket.md`

- [ ] **Step 1: Write hub tests**

Create `services/api/internal/ws/hub_test.go`:

```go
package ws

import "testing"

func TestHubBroadcastsToRoomMembers(t *testing.T) {
	hub := NewHub()
	chA := make(chan Message, 1)
	chB := make(chan Message, 1)
	hub.Add("room_1", "mem_a", chA)
	hub.Add("room_1", "mem_b", chB)
	hub.Broadcast("room_1", Message{Type: "member.speaking_changed", Payload: map[string]any{"member_id":"mem_a","speaking":true}})
	if got := <-chA; got.Type != "member.speaking_changed" { t.Fatalf("A got %#v", got) }
	if got := <-chB; got.Type != "member.speaking_changed" { t.Fatalf("B got %#v", got) }
}
```

- [ ] **Step 2: Implement hub**

Create `services/api/internal/ws/hub.go`:

```go
package ws

import "sync"

type Message struct { Type string `json:"type"`; Payload any `json:"payload"` }

type Hub struct { mu sync.RWMutex; rooms map[string]map[string]chan Message }

func NewHub() *Hub { return &Hub{rooms: map[string]map[string]chan Message{}} }

func (h *Hub) Add(roomID, memberID string, ch chan Message) { h.mu.Lock(); defer h.mu.Unlock(); if h.rooms[roomID] == nil { h.rooms[roomID] = map[string]chan Message{} }; h.rooms[roomID][memberID] = ch }
func (h *Hub) Remove(roomID, memberID string) { h.mu.Lock(); defer h.mu.Unlock(); delete(h.rooms[roomID], memberID) }
func (h *Hub) Broadcast(roomID string, msg Message) { h.mu.RLock(); defer h.mu.RUnlock(); for _, ch := range h.rooms[roomID] { select { case ch <- msg: default: } } }
```

- [ ] **Step 3: Add WebSocket route**

Add route:

```go
v1.GET("/rooms/:room_id/ws", handlers.RoomWebSocket)
```

Implement `RoomWebSocket` using `coder/websocket` and `wsjson.Read/Write`. It must:

- verify room session token
- add connection to hub
- send `room.snapshot`
- accept mute/speaking/voice mode messages
- broadcast validated state changes
- close on invalid token

- [ ] **Step 4: Update WebSocket docs with payload schemas**

Modify `docs/api/websocket.md` to include one concrete example:

```json
{
  "type": "member.speaking_changed",
  "payload": {
    "member_id": "mem_abc",
    "speaking": true
  }
}
```

- [ ] **Step 5: Verify hub tests pass**

Run:

```powershell
cd services\api
go test ./internal/ws ./internal/http -v
```

Expected:

```text
PASS
```

- [ ] **Step 6: Commit**

```powershell
git add services/api/internal/ws services/api/internal/http docs/api/websocket.md
git commit -m "feat(api): add room websocket state stream"
```

---

## Task 11: Implement client local settings and voice state tests

**Files:**
- Create: `apps/desktop/internal/config/store.go`
- Create: `apps/desktop/internal/config/store_test.go`
- Create: `apps/desktop/frontend/src/state/voiceState.ts`
- Create: `apps/desktop/frontend/src/state/voiceState.test.ts`
- Modify: `apps/desktop/frontend/package.json`
- Create: `apps/desktop/frontend/vitest.config.ts`
- Create: `apps/desktop/frontend/src/settings/settings.ts`

- [ ] **Step 1: Add frontend test tooling**

Run:

```powershell
cd apps\desktop\frontend
npm install -D vitest @testing-library/react @testing-library/jest-dom jsdom
cd ..\..\..
```

Add scripts to `apps/desktop/frontend/package.json`:

```json
{
  "scripts": {
    "test": "vitest",
    "test:run": "vitest --run"
  }
}
```

Create `apps/desktop/frontend/vitest.config.ts`:

```ts
import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    environment: 'jsdom',
  },
});
```

- [ ] **Step 2: Write voice state tests**

Create `apps/desktop/frontend/src/state/voiceState.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { canSendAudio } from './voiceState';

describe('canSendAudio', () => {
  it('requires push-to-talk pressed in push-to-talk mode', () => {
    expect(canSendAudio({ connected: true, micAvailable: true, muted: false, voiceMode: 'push_to_talk', pttPressed: true, freeTalkEnabledInRoom: false })).toBe(true);
    expect(canSendAudio({ connected: true, micAvailable: true, muted: false, voiceMode: 'push_to_talk', pttPressed: false, freeTalkEnabledInRoom: false })).toBe(false);
  });

  it('lets mute override every mode', () => {
    expect(canSendAudio({ connected: true, micAvailable: true, muted: true, voiceMode: 'free_talk', pttPressed: false, freeTalkEnabledInRoom: true })).toBe(false);
  });

  it('requires explicit in-room free-talk enablement', () => {
    expect(canSendAudio({ connected: true, micAvailable: true, muted: false, voiceMode: 'free_talk', pttPressed: false, freeTalkEnabledInRoom: false })).toBe(false);
  });
});
```

- [ ] **Step 3: Implement voice state function**

Create `apps/desktop/frontend/src/state/voiceState.ts`:

```ts
export type VoiceMode = 'push_to_talk' | 'free_talk';

export type VoiceStateInput = {
  connected: boolean;
  micAvailable: boolean;
  muted: boolean;
  voiceMode: VoiceMode;
  pttPressed: boolean;
  freeTalkEnabledInRoom: boolean;
};

export function canSendAudio(input: VoiceStateInput): boolean {
  if (!input.connected || !input.micAvailable || input.muted) return false;
  if (input.voiceMode === 'push_to_talk') return input.pttPressed;
  return input.freeTalkEnabledInRoom;
}
```

- [ ] **Step 4: Add Go settings store**

Create `apps/desktop/internal/config/store.go` with a JSON file store for:

```go
type Settings struct {
	AnonymousID       string `json:"anonymous_id"`
	Nickname          string `json:"nickname"`
	AvatarID          string `json:"avatar_id"`
	PushToTalkKey     string `json:"push_to_talk_key"`
	MicrophoneDevice  string `json:"microphone_device"`
	OutputDevice      string `json:"output_device"`
	VoiceMode         string `json:"voice_mode"`
	OutputVolume      int    `json:"output_volume"`
}
```

Default values:

```go
PushToTalkKey: "V"
VoiceMode: "push_to_talk"
OutputVolume: 100
```

- [ ] **Step 5: Add frontend settings wrapper**

Create `apps/desktop/frontend/src/settings/settings.ts`:

```ts
export type LocalSettings = {
  anonymousId: string;
  nickname: string;
  avatarId: string;
  pushToTalkKey: string;
  microphoneDevice: string;
  outputDevice: string;
  voiceMode: 'push_to_talk' | 'free_talk';
  outputVolume: number;
};

export const defaultSettings: LocalSettings = {
  anonymousId: '',
  nickname: '',
  avatarId: '',
  pushToTalkKey: 'V',
  microphoneDevice: '',
  outputDevice: '',
  voiceMode: 'push_to_talk',
  outputVolume: 100,
};
```

- [ ] **Step 6: Verify local state tests**

Run:

```powershell
cd apps\desktop\frontend
npm run test:run -- voiceState.test.ts
cd ..
go test ./internal/config -v
cd ..\..
```

Expected:

```text
frontend tests PASS
Go settings tests PASS
```

- [ ] **Step 7: Commit**

```powershell
git add apps/desktop/internal/config apps/desktop/frontend/package.json apps/desktop/frontend/package-lock.json apps/desktop/frontend/vitest.config.ts apps/desktop/frontend/src/state apps/desktop/frontend/src/settings
git commit -m "feat(desktop): add local settings and voice state"
```

---

## Task 12: Implement desktop API client, WebSocket reducer, and room UI shell

**Files:**
- Create: `apps/desktop/frontend/src/api/client.ts`
- Create: `apps/desktop/frontend/src/api/roomSocket.ts`
- Create: `apps/desktop/frontend/src/state/roomReducer.ts`
- Create: `apps/desktop/frontend/src/state/roomReducer.test.ts`
- Create: `apps/desktop/frontend/src/app/App.tsx`
- Create: `apps/desktop/frontend/src/app/routes.tsx`

- [ ] **Step 1: Write room reducer tests**

Create `apps/desktop/frontend/src/state/roomReducer.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { roomReducer, type RoomState } from './roomReducer';

const initial: RoomState = { roomId: 'room_1', members: [] };

describe('roomReducer', () => {
  it('adds joined member', () => {
    const next = roomReducer(initial, { type: 'member.joined', payload: { member_id: 'mem_1', nickname: 'A', muted: false, speaking: false, state: 'online' } });
    expect(next.members.map((m) => m.memberId)).toEqual(['mem_1']);
  });

  it('keeps member position when speaking changes', () => {
    const withMembers = { roomId: 'room_1', members: [
      { memberId: 'mem_1', nickname: 'A', muted: false, speaking: false, state: 'online' },
      { memberId: 'mem_2', nickname: 'B', muted: false, speaking: false, state: 'online' },
    ] } satisfies RoomState;
    const next = roomReducer(withMembers, { type: 'member.speaking_changed', payload: { member_id: 'mem_2', speaking: true } });
    expect(next.members.map((m) => m.memberId)).toEqual(['mem_1', 'mem_2']);
    expect(next.members[1].speaking).toBe(true);
  });
});
```

- [ ] **Step 2: Implement reducer**

Create `apps/desktop/frontend/src/state/roomReducer.ts` with:

```ts
export type Member = { memberId: string; nickname: string; muted: boolean; speaking: boolean; state: 'online' | 'reconnecting' | 'disconnected' };
export type RoomState = { roomId: string; members: Member[] };
export type RoomEvent = { type: string; payload: Record<string, unknown> };

export function roomReducer(state: RoomState, event: RoomEvent): RoomState {
  switch (event.type) {
    case 'member.joined':
      return { ...state, members: [...state.members, toMember(event.payload)] };
    case 'member.speaking_changed':
      return { ...state, members: state.members.map((member) => member.memberId === event.payload.member_id ? { ...member, speaking: Boolean(event.payload.speaking) } : member) };
    case 'member.muted_changed':
      return { ...state, members: state.members.map((member) => member.memberId === event.payload.member_id ? { ...member, muted: Boolean(event.payload.muted) } : member) };
    default:
      return state;
  }
}

function toMember(payload: Record<string, unknown>): Member {
  return { memberId: String(payload.member_id), nickname: String(payload.nickname), muted: Boolean(payload.muted), speaking: Boolean(payload.speaking), state: String(payload.state) as Member['state'] };
}
```

- [ ] **Step 3: Implement API client contracts**

Create `apps/desktop/frontend/src/api/client.ts` exporting:

- `createRoom(input)`
- `joinRoom(input)`
- `leaveRoom(input)`
- `freshLiveKitToken(input)`

Each function uses `fetch`, checks `response.ok`, and throws `Error` with server error text when not OK.

- [ ] **Step 4: Implement WebSocket wrapper**

Create `apps/desktop/frontend/src/api/roomSocket.ts` exporting `connectRoomSocket(url, token, onMessage)`.

Rules:

- append token as `?token=` query parameter
- parse JSON messages
- call `onMessage`
- expose `send(message)` and `close()`

- [ ] **Step 5: Replace spike route with app shell**

Replace `apps/desktop/frontend/src/app/App.tsx` with a simple route shell:

```tsx
export default function App() {
  return <div className="min-h-screen bg-slate-950 text-slate-50">echo</div>;
}
```

- [ ] **Step 6: Verify frontend tests pass**

Run:

```powershell
cd apps\desktop\frontend
npm run test:run
```

Expected:

```text
PASS
```

- [ ] **Step 7: Commit**

```powershell
git add apps/desktop/frontend/src
git commit -m "feat(desktop): add room client state"
```

---

## Task 13: Implement LiveKit client integration and voice controls

**Files:**
- Create: `apps/desktop/frontend/src/livekit/livekitClient.ts`
- Create: `apps/desktop/frontend/src/components/VoiceControls.tsx`
- Create: `apps/desktop/frontend/src/components/MemberList.tsx`
- Modify: `apps/desktop/frontend/src/app/App.tsx`

- [ ] **Step 1: Implement LiveKit client wrapper**

Create `apps/desktop/frontend/src/livekit/livekitClient.ts`:

```ts
import { Room, RoomEvent, Track } from 'livekit-client';

export type EchoLiveKit = { room: Room; disconnect: () => Promise<void>; setMicrophoneEnabled: (enabled: boolean) => Promise<void> };

export async function connectLiveKit(url: string, token: string, onRemoteAudio: (element: HTMLMediaElement) => void): Promise<EchoLiveKit> {
  const room = new Room({ adaptiveStream: false, dynacast: false });
  room.on(RoomEvent.TrackSubscribed, (track) => {
    if (track.kind === Track.Kind.Audio) onRemoteAudio(track.attach());
  });
  await room.connect(url, token);
  return {
    room,
    disconnect: async () => { await room.disconnect(); },
    setMicrophoneEnabled: async (enabled: boolean) => { await room.localParticipant.setMicrophoneEnabled(enabled); },
  };
}
```

- [ ] **Step 2: Implement voice controls component**

Create `apps/desktop/frontend/src/components/VoiceControls.tsx` with props:

```ts
type Props = { muted: boolean; voiceMode: 'push_to_talk' | 'free_talk'; canSend: boolean; onMuteChange: (muted: boolean) => void; onVoiceModeChange: (mode: 'push_to_talk' | 'free_talk') => void };
```

It must render:

- current mode
- mute/unmute button
- push-to-talk/free-talk toggle
- clear text for `canSend`

- [ ] **Step 3: Implement member list component**

Create `apps/desktop/frontend/src/components/MemberList.tsx` with stable ordering from reducer state. Speaking members get a visual highlight but do not move.

- [ ] **Step 4: Wire room page happy path**

Modify `App.tsx` to support minimal flow:

- nickname input
- create room
- join room by invite code
- connect WebSocket
- connect LiveKit
- render member list and voice controls

- [ ] **Step 5: Manual verify two-client happy path**

Run API, LiveKit, and desktop dev app. Expected:

```text
Client A creates room
Client B joins by invite code
Both clients join LiveKit room
Both clients can hear each other on normal network
Member list shows both clients
Mute state updates member list
Speaking state highlights without reordering list
```

- [ ] **Step 6: Commit**

```powershell
git add apps/desktop/frontend/src
git commit -m "feat(desktop): add livekit room voice controls"
```

---

## Task 14: Implement Wails tray, window lifecycle, local logs, and app quit behavior

**Files:**
- Create: `apps/desktop/internal/logging/logger.go`
- Create: `apps/desktop/internal/tray/tray.go`
- Create: `apps/desktop/internal/app/app.go`
- Modify: `apps/desktop/main.go`

- [ ] **Step 1: Implement client logger**

Create `apps/desktop/internal/logging/logger.go`:

```go
package logging

import (
	"log/slog"
	"os"
	"path/filepath"
)

func NewClientLogger(dir string) (*slog.Logger, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil { return nil, err }
	file, err := os.OpenFile(filepath.Join(dir, "echo-client.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil { return nil, err }
	return slog.New(slog.NewJSONHandler(file, &slog.HandlerOptions{Level: slog.LevelInfo})), nil
}
```

- [ ] **Step 2: Implement tray service**

Create `apps/desktop/internal/tray/tray.go` with Wails 3 system tray setup:

- Show main window
- Leave room event to frontend
- Quit app after frontend confirms leave

- [ ] **Step 3: Implement window close hook**

Create `apps/desktop/internal/app/app.go` with a Wails window closing hook that cancels close and hides the window.

- [ ] **Step 4: Manual verify tray behavior**

Run:

```powershell
cd apps\desktop
wails3 dev
```

Expected:

```text
Clicking X hides window to tray
Tray Show restores window
Tray Quit exits app
When in a LiveKit room, hiding window does not disconnect audio
```

- [ ] **Step 5: Commit**

```powershell
git add apps/desktop/internal apps/desktop/main.go
git commit -m "feat(desktop): add tray lifecycle and local logging"
```

---

## Task 15: Implement API server logging and deployment files

**Files:**
- Create: `services/api/internal/logging/logger.go`
- Modify: `services/api/cmd/api/main.go`
- Create: `services/api/Dockerfile`
- Create: `deploy/server/docker-compose.yml`
- Create: `deploy/server/livekit.yaml`
- Create: `deploy/server/nginx.example.conf`
- Create: `deploy/server/env.example`

- [ ] **Step 1: Create API server logger**

Create `services/api/internal/logging/logger.go`:

```go
package logging

import (
	"log/slog"
	"os"
	"path/filepath"
)

func NewServerLogger(dir string) (*slog.Logger, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil { return nil, err }
	file, err := os.OpenFile(filepath.Join(dir, "echo-api.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil { return nil, err }
	return slog.New(slog.NewJSONHandler(file, &slog.HandlerOptions{Level: slog.LevelInfo})), nil
}
```

- [ ] **Step 2: Wire server logger into API main**

Modify `services/api/cmd/api/main.go` so startup uses `logging.NewServerLogger` with `ECHO_LOG_DIR`, defaults to `/logs`, and logs startup, shutdown errors, database open failure, LiveKit token configuration presence, and HTTP bind address. Do not log LiveKit token plaintext, room session secret, request bodies, or audio data.

- [ ] **Step 3: Create API Dockerfile**

Create `services/api/Dockerfile`:

```dockerfile
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY services/api/go.mod services/api/go.sum ./
RUN go mod download
COPY services/api ./
RUN go build -o /out/echo-api ./cmd/api

FROM alpine:3.20
RUN adduser -D -H echo
USER echo
WORKDIR /app
COPY --from=build /out/echo-api /app/echo-api
EXPOSE 8080
ENTRYPOINT ["/app/echo-api"]
```

- [ ] **Step 4: Create Compose stack**

Create `deploy/server/docker-compose.yml`:

```yaml
services:
  api:
    build:
      context: ../..
      dockerfile: services/api/Dockerfile
    environment:
      ECHO_HTTP_ADDR: ":8080"
      ECHO_DATABASE_PATH: "/data/echo.sqlite3"
      ECHO_LIVEKIT_URL: "wss://livekit.example.com"
      ECHO_LIVEKIT_API_KEY: "devkey"
      ECHO_LIVEKIT_API_SECRET: "secret"
      ECHO_ROOM_SESSION_SECRET: "replace-with-32-byte-secret"
      ECHO_LOG_DIR: "/logs"
    volumes:
      - echo-api-data:/data
      - echo-api-logs:/logs
    ports:
      - "127.0.0.1:8080:8080"

  livekit:
    image: livekit/livekit-server:latest
    command: --config /etc/livekit.yaml
    volumes:
      - ./livekit.yaml:/etc/livekit.yaml:ro
    ports:
      - "127.0.0.1:7880:7880"
      - "7881:7881"
      - "50000-50100:50000-50100/udp"

volumes:
  echo-api-data:
  echo-api-logs:
```

- [ ] **Step 5: Create LiveKit config example**

Create `deploy/server/livekit.yaml`:

```yaml
port: 7880
rtc:
  use_external_ip: true
  tcp_port: 7881
  port_range_start: 50000
  port_range_end: 50100
keys:
  devkey: secret
logging:
  level: info
```

- [ ] **Step 6: Create external Nginx example**

Create `deploy/server/nginx.example.conf`:

```nginx
map $http_upgrade $connection_upgrade {
    default upgrade;
    '' close;
}

server {
    listen 443 ssl http2;
    server_name api.example.com;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
    }
}

server {
    listen 443 ssl http2;
    server_name livekit.example.com;

    location / {
        proxy_pass http://127.0.0.1:7880;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
    }
}
```

- [ ] **Step 7: Create server environment example**

Create `deploy/server/env.example`:

```dotenv
ECHO_HTTP_ADDR=:8080
ECHO_DATABASE_PATH=/data/echo.sqlite3
ECHO_LOG_DIR=/logs
ECHO_LIVEKIT_URL=wss://livekit.example.com
ECHO_LIVEKIT_API_KEY=devkey
ECHO_LIVEKIT_API_SECRET=replace-with-livekit-secret
ECHO_ROOM_SESSION_SECRET=replace-with-32-byte-secret
```

- [ ] **Step 8: Verify Compose config**

Run:

```powershell
docker compose -f deploy/server/docker-compose.yml config
```

Expected:

```text
Compose renders normalized config without errors
```

- [ ] **Step 9: Commit**

```powershell
git add services/api/Dockerfile services/api/internal/logging services/api/cmd/api/main.go deploy/server
git commit -m "chore(deploy): add single server compose stack"
```

---

## Task 16: Implement CI and release workflows

**Files:**
- Create: `.github/workflows/ci.yml`
- Create: `.github/workflows/release-windows.yml`

- [ ] **Step 1: Create CI workflow**

Create `.github/workflows/ci.yml`:

```yaml
name: CI

on:
  push:
  pull_request:

jobs:
  api:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - run: go test ./services/api/...

  desktop-frontend:
    runs-on: windows-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: '20'
      - run: npm ci
        working-directory: apps/desktop/frontend
      - run: npm run test:run
        working-directory: apps/desktop/frontend
```

- [ ] **Step 2: Create release workflow**

Create `.github/workflows/release-windows.yml`:

```yaml
name: Release Windows

on:
  push:
    tags:
      - 'v*.*.*'

jobs:
  windows-installer:
    runs-on: windows-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - uses: actions/setup-node@v4
        with:
          node-version: '20'
      - name: Install Wails
        run: go install github.com/wailsapp/wails/v3/cmd/wails3@latest
      - name: Install frontend dependencies
        run: npm ci
        working-directory: apps/desktop/frontend
      - name: Build desktop app
        run: wails3 build
        working-directory: apps/desktop
      - name: Package desktop app
        run: wails3 package
        working-directory: apps/desktop
      - name: Upload release assets
        uses: softprops/action-gh-release@v2
        with:
          files: apps/desktop/bin/**
```

- [ ] **Step 3: Verify workflow syntax locally**

Run:

```powershell
Get-Content .github\workflows\ci.yml
Get-Content .github\workflows\release-windows.yml
```

Expected:

```text
Both files exist and include CI and Release Windows workflow names
```

- [ ] **Step 4: Commit**

```powershell
git add .github/workflows
git commit -m "ci: add tests and windows release workflow"
```

---

## Task 17: Final integration verification

**Files:**
- Modify: `docs/spikes/wails-livekit-audio.md`
- Modify: `docs/spikes/device-tray.md`
- Modify: `docs/spikes/push-to-talk-keyboard.md`

- [ ] **Step 1: Run backend test suite**

```powershell
cd services\api
go test ./...
```

Expected:

```text
PASS
```

- [ ] **Step 2: Run desktop frontend tests**

```powershell
cd apps\desktop\frontend
npm run test:run
```

Expected:

```text
PASS
```

- [ ] **Step 3: Build desktop app**

```powershell
cd apps\desktop
wails3 build
```

Expected:

```text
Build exits 0
```

- [ ] **Step 4: Validate server compose config**

```powershell
docker compose -f deploy/server/docker-compose.yml config
```

Expected:

```text
Compose exits 0
```

- [ ] **Step 5: Manual acceptance checklist**

Run a Windows manual test with two clients and record results in `docs/spikes/wails-livekit-audio.md`:

```text
[ ] Two users join the same room and hear each other
[ ] 3-10 user room works
[ ] 11th user is rejected with clear message
[ ] Push-to-talk works with game foregrounded for 10 press/release cycles
[ ] Free-talk requires explicit in-room action
[ ] Mute prevents audio from being sent
[ ] Speaking highlight appears without reordering members
[ ] Close-to-tray keeps audio alive
[ ] Reconnect within 30 seconds restores same member
[ ] Reconnect after 30 seconds removes member
[ ] Empty room expires after 30 minutes
[ ] Installer from GitHub Releases installs and launches echo
```

- [ ] **Step 6: Commit verification notes**

```powershell
git add docs/spikes
git commit -m "test: record mvp integration verification"
```

---

## Coverage matrix

| Design requirement | Implementation task |
| --- | --- |
| Wails 3 desktop shell | Tasks 1, 2, 14 |
| LiveKit/WebRTC audio | Tasks 2, 13, 17 |
| Device selection | Tasks 3, 13, 17 |
| Push-to-talk press/release | Tasks 4, 11, 17 |
| Gin/GORM/SQLite API | Tasks 1, 5, 6, 7, 9 |
| Room lifecycle and expiry | Tasks 7, 17 |
| Invite code rules | Task 5 |
| LiveKit token issuance | Task 8 |
| HTTP/OpenAPI | Task 9 |
| WebSocket state stream | Task 10 |
| Local settings | Task 11 |
| Voice state and mute rules | Tasks 11, 13 |
| Tray lifecycle | Task 14 |
| Detailed logs without upload | Tasks 14, 15 |
| Docker Compose public server | Task 15 |
| GitHub Actions and Releases | Task 16 |
| Manual Windows/game acceptance | Task 17 |

## Execution handoff

Plan complete at `implement.md`.

Recommended execution mode: subagent-driven development with one fresh worker per task group:

1. Backend domain/API worker: Tasks 5-10.
2. Desktop shell/media worker: Tasks 2-4 and 11-14.
3. Deployment/release worker: Tasks 15-16.
4. Integration verifier: Task 17.

Inline execution is also possible, but it should still preserve the task order and stop after any failed spike.
