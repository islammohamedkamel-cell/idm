# OpenDownload

A YouTube-first download manager with a browser-based desktop interface. Paste a YouTube link into the main form to queue a video; expand **Download a file instead** for direct HTTP/HTTPS downloads. The interface reports whether yt-dlp and FFmpeg are available. This is an initial implementation, not a complete replacement for Internet Download Manager.

## Windows

Run `dist/OpenDownload-windows-amd64.exe` (Windows x64). It opens your default browser to a loopback-only interface. Keep its console window open; Ctrl+C shuts down the manager. Windows may warn about the unsigned executable. No administrator privileges are needed.

Downloads and queue history are saved in `%USERPROFILE%\Downloads\OpenDownload`. Filenames have unique prefixes to avoid overwrites. Set `IDM_DOWNLOAD_DIR` to use another directory, or `IDM_PORT` to change port 17843. Copy the printed session URL if browser launch fails. Each restart generates a new session token; use the newly opened interface.

## Features

- HTTP and HTTPS file downloads; one active transfer and persistent queue.
- Pause, resume, cancel, retry failed jobs, future start times, per-job speed limits.
- Up to three automatic transfer attempts with backoff.
- Safe restart when a server ignores Range; ETag/If-Range validation before appending.
- Partial-response and truncated-body validation, progress, final SHA-256 checksum.
- Queue restoration after restart (interrupted active jobs become paused).
- Browser right-click connector for links, audio and video URLs.
- YouTube download delegation to **yt-dlp**; install yt-dlp and FFmpeg separately and ensure both are on PATH before launching the manager. Use official installers from https://github.com/yt-dlp/yt-dlp and https://ffmpeg.org/download.html . A missing yt-dlp produces an actionable job error. Video jobs use yt-dlp's own resume behavior; byte progress and speed limits currently apply to direct downloads only. A video job may create multiple output files; find them by the job ID prefix.

Only download content you own or have permission to save. DRM-protected streams are not supported.

## Browser setup

1. Start the manager. In its interface, expand **Connect your browser** and copy the pairing token.
2. Chrome: open `chrome://extensions`, enable developer mode, choose **Load unpacked**, select `extension/`. Edge: use the equivalent controls at `edge://extensions`.
3. Open the extension popup and save the token. Right-click a YouTube page, video link, or downloadable file link and choose **Download with OpenDownload**. A check badge means the job was queued; `!` means connection or authentication failed. Re-pair after restarting the manager. No browser cookies or credentials are forwarded.
4. Firefox: copy `extension/` to a separate directory and replace that copy's `manifest.json` with `manifest.firefox.json`. Open `about:debugging#/runtime/this-firefox`, choose **Load Temporary Add-on**, and select that manifest. Permanent installation requires Mozilla signing.

Safari: this repository does **not** include a signed Safari extension. On macOS with Xcode, Apple's `xcrun safari-web-extension-converter extension/` can begin a Safari wrapper project, which needs adaptation, signing and testing on an Apple device. Safari users can paste direct links into the manager. The supplied executable runs on Windows, not macOS.

Connectors require the default port. Custom ports require updating the connector's host permission and endpoint. This connector sends explicitly selected links; it does not automatically intercept all browser downloads, inspect protected streams, or transfer login sessions. Browser-store publication and native browser testing remain outstanding.

## Development and verification

Requires the official Go toolchain (1.22 or newer), with no external Go modules. Work directly in the existing checkout; cloud tasks already have isolated environments, so no additional Git worktree is needed.

```sh
go test -race -v ./...
go vet ./...
node tests/ui.test.cjs
node tests/extension.test.cjs
go run .
# Cross-compile Windows x64 from Linux/macOS:
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o dist/OpenDownload-windows-amd64.exe .
```

For headless testing set `IDM_NO_BROWSER=1` and use a temporary `IDM_DOWNLOAD_DIR`. The API requires `Authorization: Bearer <session-token>`; the printed startup URL contains that token. The manager rejects unexpected Host headers and listens only on 127.0.0.1. Pairing tokens authorize access to the queue; treat them as local credentials. Do not expose the server through a public proxy.

`main_test.go` exercises actual local HTTP transfers, file integrity, resume, range fallback, invalid ranges, truncated responses, HTTP failures, cancellation, scheduling, persistence, URL validation and API controls/security. See `VALIDATION.md` for this environment's results and limits.

## Remaining work

Segmented parallel downloading, automatic browser interception, native desktop UI/tray, authenticated-cookie downloads, proxy configuration, FTP, batch imports, categories, site grabber, antivirus integration, shutdown-after-download, video progress, signed installers, automatic updates, signed browser extensions and complete Safari support are not implemented. This version has no delete-history UI; cancel retains partial files. Queue history contains source URLs, which may include sensitive query parameters. The Windows executable is cross-compiled and must be exercised on Windows before production distribution.
