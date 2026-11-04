# Validation

Validated in the Linux cloud environment with official Go 1.27.3. The compiler archive was verified against the SHA-256 from official go.dev release metadata.

Passed:
- `go test -race -v ./...`: 10 top-level tests, 4 additional subtests, no failures or race reports.
- `go vet ./...`.
- `./build.sh`: tests, vet, Windows x64 cross-compilation.
- Node syntax checks for both extension JavaScript files.
- Compiled Linux executable smoke test: startup, HTML response, authenticated API, queued HTTP download, actual saved contents.
- Windows output identified as a PE32+ x86-64 executable.

Coverage: successful transfers and checksum; ETag-protected resume; servers ignoring Range; invalid partial responses; truncated bodies; HTTP errors; API authentication and Host checks; add/pause/resume/cancel controls; queue persistence; scheduling; cancellation; URL validation; YouTube URL routing; transient retries; bandwidth throttling; queue execution.

The sandbox blocks listening sockets; integration tests passed using the approved execution mode with socket access. Initial sandbox-only attempts failed for that environment restriction.

Not validated: Windows runtime/UI launch, real Chrome/Edge/Firefox installation and connector operation, Safari conversion/signing, live YouTube downloads, yt-dlp/FFmpeg installation, network interruption on Windows, signed installer or browser store distribution. No Windows runtime or Apple signing environment was available. YouTube URL validation is tested; that does not establish live media downloading. See README for missing features; complete IDM parity is not claimed.

## YouTube-first interface update

- YouTube submission is now the primary form; file downloading remains available in a secondary panel.
- Authenticated tool-availability API reports yt-dlp and FFmpeg presence; API authentication and method restrictions are tested.
- `node tests/ui.test.cjs`: four tests passed for primary YouTube submission, unrelated-site rejection, direct file downloads with speed limits, and missing-tool guidance. These tests execute interface JavaScript with a simulated DOM; they are not browser rendering tests.
- `node tests/extension.test.cjs`: page-context YouTube submission and pairing header test passed using simulated browser APIs.
- Browser page context now supports sending the current YouTube page.
- Live YouTube extraction and Windows/browser execution remain unverified; external tools are not bundled.
