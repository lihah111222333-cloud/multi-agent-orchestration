# Agent Terminal Collaboration Rules

## Bridge Boundary (Must Follow)

- Frontend JavaScript is **UI-only**.
- Any desktop/system capability (file dialogs, OS access, process/system integration, clipboard persistence, etc.) must be implemented via **Wails v3 bridge** and Go services.
- Do **not** add browser-native/system-like fallbacks in frontend (for example: `showOpenFilePicker`, hidden file input tricks for local FS browsing, custom OS shell calls from JS).

## File Picking Policy

- Project path selection and attachment file selection must go through Go methods exposed by Wails.
- Frontend should call Wails runtime bridge (`/wails/runtime.js` + `Call.ByID`) only.
- If bridge is unavailable, fail fast and report the bridge issue; do not silently switch to web-mode picker behavior.

## Why

- This project is a desktop bridge application; keeping a strict boundary avoids platform inconsistency and regression in macOS app behavior.
