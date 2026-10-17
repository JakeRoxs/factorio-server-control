## Highlights

- More reliable start/stop, profile switching and reconnection handling, with regression coverage for delayed and failed operations.
- Remove the previously unresolved `braces` build dependency and update the Go security baseline to 1.26.9.
- Faster initial UI loading and broader browser/real-engine checks, while keeping the existing interface, features and persistent data layout.

## Added

- Automated browser coverage across Chromium, Firefox and WebKit, including mobile layouts, keyboard interaction, permissions and error recovery.
- Disposable real-engine container scenarios for base Factorio 2.0.77 with combined storage and Space Age 2.1.20 with split storage. They exercise world creation, save/mod uploads, startup settings, live checkpoints, autostart and container recreation.
- Contributor and operations guides covering local isolation, permissions, recovery and the limits of automated verification.

## Changed

- Management pages load on demand through content-hashed assets. The navigation shell remains available if a page fails, with an explicit reload action rather than an automatic refresh that discards work.
- Tailwind 4 processes plain CSS with the existing palette and layout. Unused Sass/loaders and other unused frontend dependencies are removed, and production CSS minification is corrected.
- Reviewed Node 24/Ubuntu 24.04 image and GitHub artifact-action updates are included. Go toolchain versions are checked for consistency across build and test paths.
- The manual live-world smoke script requires an explicit disposable-target opt-in and removes only saves that it created successfully.

## Fixed

- A failed startup no longer leaves a queued stop or runtime reservation behind. Stopping an already stopped server leaves it unlocked; manager shutdown also waits for pending startup within the existing timeout.
- Late profile responses cannot revert a completed profile change. Obsolete WebSocket callbacks cannot overwrite a newer connection or restore logged-out state; failed console sends are not replayed.
- Save-upload controls reflect the existing stopped-server requirement. New passwords are checked against bcrypt's 72-byte UTF-8 limit before hashing, without changing existing passwords or hashes.
- Mod ZIP downloads preserve nested paths instead of flattening duplicate filenames. Equivalent file paths share locks, and unused lock entries are released.
- Legacy save creation uses the shared staged world generator and refuses to overwrite an existing save. Replacement version parsing is atomic and clears omitted components.
- Cancelled requests remain quiet; structured API errors become safe notification text. Mobile navigation retains keyboard focus, closes nested profile menus in order and keeps help text on screen.
- Closing a dialog or mobile navigation restores focus to its trigger, including browsers that do not focus buttons when clicked.

## Security and privacy

- Removing the old Tailwind dependency chain eliminates `braces` and its build-only advisory GHSA-vfj7-8cjw-p6xm. The current npm lockfile audit reports no known vulnerabilities; this is a point-in-time result, not a permanent guarantee.
- Go 1.26.9 includes upstream HTTP, MIME and TLS security fixes. `govulncheck` reports no finding in reachable code or imported packages; an advisory in an unused package of a required module remains distinct from that result.
- Existing authentication, administrator restrictions, stopped-server guards, archive/path limits and credential handling remain in place. No new external service, telemetry or AI runtime is introduced.
- Tests and documentation use synthetic data. No private savegames, accounts or credentials are shipped.

## Compatibility and migration

- No data migration is required. Profiles, selected saves, exact Factorio versions, game modes, mod settings, autostart preferences, database formats and portable backup format remain unchanged.
- The UI now requires Chrome 111+, Safari 16.4+ or Firefox 128+ because of Tailwind 4's CSS baseline. Older browsers should be updated before upgrading the manager.
- New passwords must contain 10–72 UTF-8 bytes; multibyte characters can reach the limit before 72 characters. Existing login credentials remain valid.
- Keep `/opt/fsm-data` and either the combined `/opt/factorio` mount or all three split mounts for saves, mods and config. Never mix the two Factorio layouts. No new container ports or environment variables are required.

## Upgrade and rollback

1. Save and stop Factorio cleanly. Back up `/opt/fsm-data` and the selected Factorio data layout.
2. Pull `ghcr.io/tricade/factorio-server-manager:0.20.1` or the updated `latest` alias and recreate the container with its existing mappings and 180-second stop timeout.
3. Reload the browser once, verify 0.20.1 in the UI footer and check the active profile, exact Factorio version and saves. The existing internal autostart preference still applies.

For rollback, stop the container cleanly and use the retained immutable `0.20.0` image with the pre-upgrade full-volume backup. Do not assume a downgrade can safely read every state written by a newer release. Portable profile exports supplement rather than replace a full offline backup.

## Known limitations

- One playable Factorio instance runs at a time; profile switching still requires a stopped server.
- The real-engine matrix covers two representative versions and small generated worlds, not every historical version, large factory or arbitrary third-party mod combination.
- Authenticated Mod Portal downloads need a Factorio account and remain a manual integration check. Automated tests cover rejected unauthenticated imports without data loss and use controlled fixtures for successful import/rollback paths.
- Mobile browser tests emulate devices; they do not certify every physical phone or native file dialog. Existing screenshots remain representative because no menu, branding or feature redesign was made.
- The production image supports `linux/amd64`, matching the official headless server. The map remains a periodic isolated chart snapshot, not a graphical live stream.

## Verification

- `factorio-server-manager-linux-0.20.1.zip`
- `factorio-server-manager-windows-0.20.1.zip`
- `SHA256SUMS`
- `ghcr.io/tricade/factorio-server-manager:0.20.1` (`linux/amd64`, with provenance and SBOM)
- Node 24 UI tests and production build; Go 1.26.9 short tests, `go vet` and `govulncheck`.
- Python repository/Unraid release validators, Windows/Linux CI, targeted race tests, CodeQL, the browser matrix, mounted-directory checks and disposable real-engine persistence scenarios.
- Both platform archives, file permissions, included documentation, checksums and immutable image publication are verified by the guarded release workflow.

Factorio Server Control is a maintained fork of the original Factorio Server Manager project and is not affiliated with or endorsed by Wube Software. Factorio is a trademark of Wube Software.
