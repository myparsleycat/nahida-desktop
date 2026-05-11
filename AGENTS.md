- ALWAYS USE PARALLEL TOOLS WHEN APPLICABLE.
- Prefer automation: execute requested actions without confirmation unless blocked by missing info or safety/irreversibility.

## Commands

```bash
pnpm install          # install deps
pnpm dev              # start dev (auto-runs db:generate first, triggers codegen)
pnpm build            # production build (auto-runs db:generate)
pnpm lint             # oxlint (NOT eslint)
pnpm lint:fix         # oxlint --fix
pnpm fmt              # oxfmt (NOT prettier)
pnpm fmt:check        # oxfmt --check
pnpm typecheck        # oxlint --type-aware --type-check (NOT tsc)
pnpm db:generate      # regenerate Drizzle migrations → drizzle/
pnpm build:native     # build all 7 Rust napi-rs native addons

```

> **Note:** There is no `pnpm test` — the `tests/` directory is empty.

## Architecture

- **Electron + React + Vite** (`electron-vite`): main process `src/main/`, preload `src/preload/`, renderer `src/renderer/src/`
- **Windows-first**: mod management, XXMI launcher, and mod tools are only loaded on Windows via `supportsWindowsDesktopFeatures()` — they use dynamic `import()` at runtime (`src/main/index.ts:145`)
- **Database**: SQLite via better-sqlite3 + Drizzle ORM. Schema at `src/main/internal/db/schema.ts`. Migrations in `drizzle/`. DB file is `local.db` (dev) or `{userData}/data.db` (packaged)
- **Native addons**: 7 Rust (napi-rs) packages under `native/`. Their `index.js` CJS glue is auto-patched by `plugins/native-binding.ts` at build time
- **Sibling repo**: references `../backend` (Elysia server) for shared types via tsconfig paths
- **Shadcn/ui** ("new-york" style) with Tailwind v4, components under `src/renderer/src/components/ui/`

## Code Generation

Three auto-generated files are gitignored (`*.gen.ts`). On a fresh clone, run `pnpm dev` or `pnpm build` to generate them:

| File                                | Generator                  | Trigger                               |
| ----------------------------------- | -------------------------- | ------------------------------------- |
| `src/shared/types.gen.ts`           | `plugins/ipc-generator.ts` | Vite buildStart / handler file change |
| `src/shared/ipc-keys.gen.ts`        | `plugins/ipc-generator.ts` | same                                  |
| `src/renderer/src/routeTree.gen.ts` | `@tanstack/router-plugin`  | Vite build                            |

The IPC generator scans `src/main/ipc/handlers/*.ts` for `rh()` and `ipcMain.handle()` calls, infers channel names and types, and writes types + runtime channel whitelists. Never edit `.gen.ts` files directly.

## IPC Pattern

- Handlers: use `rh("channel:name", handlerFn)` from `src/main/ipc/helper.ts` (typed wrapper around `ipcMain.handle`)
- Preload exposes `window.api.invoke(channel, ...args)` (typed handler calls), `window.api.send(channel, ...args)`, and `window.api.on(channel, listener)` (typed events)
- Channel whitelisting is enforced at runtime via the generated `IPC_HANDLER_CHANNELS` / `IPC_SEND_CHANNELS` / `IPC_EVENT_CHANNELS` constant arrays
- To add a new IPC channel: add a handler file in `src/main/ipc/handlers/` using `rh()`, then restart dev server to regenerate types

## Formatter

- `.ts` / `.js` → 4-space indent
- `.tsx` / `.jsx` → 2-space indent
- `.json` / `.md` → 2-space indent
- Print width 100, double quotes, semicolons, trailing commas
- Import sorting: ascending order, no blank lines between import groups

## Style Guide

### General Principles

- Keep things in one function unless composable or reusable
- Do not extract single-use helpers preemptively. Inline the logic at the call site unless the helper is reused, hides a genuinely complex boundary, or has a clear independent name that improves the caller.
- Avoid `try`/`catch` where possible
- Avoid using the `any` type (oxlint warns on it)
- Rely on type inference when possible; avoid explicit type annotations or interfaces unless necessary for exports or clarity
- Prefer functional array methods (flatMap, filter, map) over for loops; use type guards on filter to maintain type inference downstream
- Prioritize using `es-toolkit` where applicable when working with TypeScript.

Reduce total variable count by inlining when a value is only used once.

```ts
// Good
const journal = JSON.parse(await fse.readFile(path.join(dir, "journal.json"), "utf8"));

// Bad
const journalPath = path.join(dir, "journal.json");
const journal = JSON.parse(await fse.readFile(journalPath, "utf8"));
```

### Destructuring

Avoid unnecessary destructuring. Use dot notation to preserve context.

```ts
// Good
obj.a;
obj.b;

// Bad
const { a, b } = obj;
```

### Variables

Prefer `const` over `let`. Use ternaries or early returns instead of reassignment.

```ts
// Good
const foo = condition ? 1 : 2;

// Bad
let foo;
if (condition) foo = 1;
else foo = 2;
```

### Control Flow

Avoid `else` statements. Prefer early returns.

```ts
// Good
function foo() {
  if (condition) return 1;
  return 2;
}

// Bad
function foo() {
  if (condition) return 1;
  else return 2;
}
```

## Type Checking

- Always run `pnpm typecheck`, never `tsc` directly.
- The root `tsconfig.json` uses project references: `tsconfig.node.json` (main/preload) and `tsconfig.web.json` (renderer).
- Generated `.gen.ts` files must exist before typechecking — run `pnpm dev` first on a fresh clone.
