# Drive URL Import Design

## Architecture

The renderer owns form state and folder-picker navigation. It invokes a single typed IPC operation, `drive:fn:copyFromUrl`, with the source URL, optional password, optional collection/item selectors, and a destination folder id. The main process validates and normalizes the URL, performs the server requests, and logs/rethrows a stable `DriveApiError` through the existing `rh()` handler wrapper.

```text
Drive toolbar/deep link
        |
        v
Drive import route (renderer)
        |  URL/password/destination
        v
drive:fn:copyFromUrl (IPC)
        |
        v
DriveService.copyFromUrl
  - parseDriveSourceUrl
  - request shared-link access OR mod overview
  - copyRemoteItems(destinationId)
        |
        v
     Akasha API
```

## Data Model

```ts
type DriveCopyFromUrlParams = {
  url: string;
  destinationId: string;
  password?: string;
  collectionId?: string;
  itemId?: string;
};

type DriveCopyFromUrlResult = {
  source: "link" | "mod";
  copied: number;
  destinationId: string;
};
```

The source URL is treated as untrusted input. Response bodies are decoded as `unknown` and narrowed by explicit type guards before ids or tokens are used.

## API Flow

### Shared Link

1. Parse `/akasha/link/:id`.
2. `POST /akasha/link/:id` with `{ password, cftoken: "" }`.
3. Validate the returned token and parent id.
4. Copy either the selected `itemId` or the link parent id with `nhd-link-token`.

### Collection

1. Parse `/akasha/mod/:id`.
2. Fetch the collection overview.
3. Filter private collections and apply an optional `collectionId`.
4. Copy the selected collection roots, or the explicit `itemId`.

## Error Contract

- `DRIVE_INVALID_SOURCE_URL`
- `DRIVE_LINK_PASSWORD_REQUIRED`
- `DRIVE_LINK_INVALID_PASSWORD`
- `DRIVE_LINK_INVALID_RESPONSE`
- `DRIVE_COLLECTION_NOT_FOUND`
- `DRIVE_COLLECTION_EMPTY`
- `DRIVE_COPY_EMPTY`
- `DRIVE_*_FAILED` fallback codes

The main process logs the channel operation, URL, destination id, and failure stage before rethrowing. The renderer maps known password/authentication errors to actionable UI states and displays a safe fallback message for other errors.

## Deep-Link Contract

- Accepted: `nahida://drive/copy?url=<encoded-source>` and `nahida://drive/import?url=<encoded-source>`.
- Optional selectors: `collection`/`collectionId`, `item`/`itemId`.
- Output route: `/drive/import?url=<source>&auto=1` plus normalized selector keys.
- Unsupported hosts, schemes, paths, and source URLs return `null`.

## Test Plan

- Unit-test source URL parsing for link, mod, malformed, foreign-host, and unsupported protocol cases.
- Unit-test deep-link conversion and selector normalization.
- Unit-test error-code normalization for nested API errors, HTTP errors, and unknown values.
- Exercise the renderer route with missing URL, unauthenticated session, password-required response, and successful destination selection where UI test infrastructure permits.
- Run full Vitest, focused type-aware lint, and an alternate-output Electron build while the active app remains running.
