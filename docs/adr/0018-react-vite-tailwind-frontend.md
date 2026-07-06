# Use React, TypeScript, Vite, and Tailwind for the Wails frontend

Echo's Wails 3 frontend will use React with TypeScript, built by Vite and styled with Tailwind CSS. This stack fits the WebView-based desktop UI, gives typed room/member/WebSocket/settings contracts, and keeps interface work fast without committing to a heavier desktop UI toolkit. MVP state should start with React state, context, and focused hooks; Redux, Zustand, or similar global stores are deferred until implementation proves the real-time room state cannot stay simple.
