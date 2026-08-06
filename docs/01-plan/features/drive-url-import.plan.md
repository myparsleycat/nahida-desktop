# Drive URL Import

## Overview

Provide a local desktop workflow for copying content from Nahida shared-link and collection URLs into a user-selected Drive folder. The workflow must also support `nahida://` deep links, optional shared-link passwords, and safe error handling at the IPC boundary.

## Scope

### In Scope

- A form that accepts a Nahida shared-link or collection URL.
- Optional password entry when a shared link requires authentication.
- Destination-folder selection instead of always copying to Drive root.
- Deep-link routing for `nahida://drive/copy` and `nahida://drive/import`.
- Copy operations for shared links and public collections.
- Stable, diagnosable API errors across Drive IPC handlers.
- Renderer feedback for validation, password, authentication, and copy failures.

### Out of Scope

- Tampermonkey or browser-extension automation.
- GitHub PR, commit, or release operations.
- Copying private collections without an authenticated API contract.
- Changing the server API.

## Functional Requirements

1. Parse only HTTPS Nahida source URLs with supported `/akasha/link/:id` or `/akasha/mod/:id` paths.
2. Reject malformed, foreign-host, and unsupported deep links before network calls.
3. Resolve the Drive root as the default destination and allow browsing into child folders and back to the parent.
4. Send a password only when supplied and surface password-required/invalid-password states distinctly.
5. Copy selected link or collection roots to the selected destination and report the copied count.
6. Preserve original error codes and include operation context in main-process logs.

## Non-Functional Requirements

- No unhandled IPC rejection from Drive operations.
- No process termination as part of development verification.
- Existing download, GameBanana, and mod-manager behavior remains unchanged.
- Existing localization conventions are followed for new renderer strings.

## Success Criteria

- URL form, password retry, and destination picker are reachable from the Drive UI.
- Valid deep links navigate to the import form with the source and optional collection/item selectors.
- Invalid source URLs fail locally with a user-facing validation error.
- Drive API failures retain stable `DRIVE_*` codes and are logged with operation context.
- Focused tests cover URL/deep-link parsing and Drive error normalization.
- Full test suite and production build complete without stopping running processes (use an alternate output directory when the active `out` directory is locked).

## Risks and Mitigations

- **Backend response shape drift:** validate unknown JSON responses before using them.
- **Password errors vary by transport:** normalize known sentinel text while preserving the original cause.
- **Destination picker navigation races:** keep the selected folder id independent from query loading state.
- **Existing worktree changes:** keep edits local and do not commit or push until explicitly authorized.

## Schedule

1. Plan: document scope and acceptance criteria.
2. Design: verify API/data-flow and test seams.
3. Do: complete implementation and focused tests.
4. Check: run gap analysis, type-aware lint, tests, and alternate-output build.
5. Act: fix gaps and repeat Check until the acceptance criteria are met.
