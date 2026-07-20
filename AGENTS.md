- ALWAYS USE PARALLEL TOOLS WHEN APPLICABLE.
- Prefer automation: execute requested actions without confirmation unless blocked by missing info or safety/irreversibility.

## Code Generation

Three auto-generated files are gitignored (`*.gen.ts`). On a fresh clone, run `pnpm dev` or `pnpm build` to generate them:

| File                                | Generator                  | Trigger                               |
| ----------------------------------- | -------------------------- | ------------------------------------- |
| `src/shared/types.gen.ts`           | `plugins/ipc-generator.ts` | Vite buildStart / handler file change |
| `src/shared/ipc-keys.gen.ts`        | `plugins/ipc-generator.ts` | same                                  |
| `src/renderer/src/routeTree.gen.ts` | `@tanstack/router-plugin`  | Vite build                            |

## IPC Pattern

- Handlers: use `rh("channel:name", handlerFn)` from `src/main/ipc/helper.ts` (typed wrapper around `ipcMain.handle`)
- Preload exposes `window.api.invoke(channel, ...args)` (typed handler calls), `window.api.send(channel, ...args)`, and `window.api.on(channel, listener)` (typed events)
- Channel whitelisting is enforced at runtime via the generated `IPC_HANDLER_CHANNELS` / `IPC_SEND_CHANNELS` / `IPC_EVENT_CHANNELS` constant arrays
- To add a new IPC channel: add a handler file in `src/main/ipc/handlers/` using `rh()`, then restart dev server to regenerate types

## Error Logging

- For IPC-backed user actions, log the original error in the main process before rethrowing when the renderer will show a generic fallback message.
- Include enough structured context to diagnose the failure without reproduction: channel/action name, user-facing entity name, relevant domain identifiers, current operation/stage, input paths, resolved paths, external URLs or executable paths when relevant, and rollback/cleanup state.
- For multi-step operations, track and log the current operation/stage and any registered rollback or cleanup state.
- Preserve the original error message/code so renderer error handling can still match known sentinel and domain-specific error codes.

## Style Guide

### General Principles

- Keep things in one function unless composable or reusable
- Do not extract single-use helpers preemptively. Inline the logic at the call site unless the helper is reused, hides a genuinely complex boundary, or has a clear independent name that improves the caller.
- Avoid `try`/`catch` where possible
- Avoid using the `any` type
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

### Complex Logic

When a function has several validation branches or supporting details, make the main function read as the happy path and move supporting details into small helpers below it.

```ts
// Good
export function loadThing(input: unknown) {
  const config = requireConfig(input)
  const metadata = readMetadata(input)
  return createThing({ config, metadata })
}

function requireConfig(input: unknown) {
  ...
}
```

- Keep helpers close to the code they support, below the main export when that improves readability.
- Do not over-abstract simple expressions into many single-use helpers; extract only when it names a real concept like `requireConfig` or `readMetadata`.
- Do not return `Effect` from helpers unless they actually perform effectful work. Synchronous parsing, validation, and option building should stay synchronous.
- Prefer Effect schema helpers such as `Schema.UnknownFromJsonString` and `Schema.decodeUnknownOption` over manual `JSON.parse` wrapped in `Effect.try` when parsing untrusted JSON strings.
- Add comments for non-obvious constraints and surprising behavior, not for obvious assignments or control flow.

## Performance

### High-Frequency Visual Feedback

When only a visual attribute (e.g. vertex colors, heatmap) changes frequently but the underlying data (positions, geometry) stays the same, do not use React state to trigger re-renders. Use refs and imperative method calls instead.

React's declarative model has high overhead for high-frequency updates: state change → memo re-execution → JSX diff → child component reconciliation → Canvas/Three.js commit. On large meshes (100k+ vertices) this can cost 200ms+ per update even when the actual computation is only 5ms.

```tsx
// Good — ref + imperative call, no React re-render
const previewKeyRef = useRef<string | null>(null);

const handleItemHighlighted = (item) => {
  previewKeyRef.current = item?.key ?? null;
  viewportRef.current?.updateColors(buildHighlightRegions(mesh, previewKeyRef.current));
};

// Imperative handle on the child component
useImperativeHandle(ref, () => ({
  updateColors: (regions) => {
    const displayWeights = composeDisplayWeights(vertexCount, regions, { ignoreAmount: true });
    writeWeightColors(displayWeights, colorsRef.current);
    colorAttribute.needsUpdate = true;
  },
}), []);
```

```tsx
// Bad — state change triggers full React re-render pipeline
const [previewKey, setPreviewKey] = useState<string | null>(null);
const [colorVersion, setColorVersion] = useState(0);

const handleItemHighlighted = (item) => {
  setPreviewKey(item?.key ?? null);
  setColorVersion((v) => v + 1); // triggers 4 memos + JSX re-render + Canvas commit
};
```

Guidelines:
- Use refs (`useRef`) for values that only affect imperative visual output, not React-rendered UI
- Expose imperative methods via `useImperativeHandle` on child components (e.g. `updateColors`, `updatePositions`)
- Keep React state for values that affect JSX structure or text content
- When positions change but only rarely, a `positionsChanged` boolean can skip expensive `computeVertexNormals` / `computeBoundingSphere` calls when positions are unchanged

## Type Checking

- Do not run tsc directly; instead, execute pnpm lint -- file/to/path file/to/path2.

## Formatting

- After modifying files, run pnpm fmt -- file/to/path file/to/path2 to apply formatting.

## Git Revert

When reverting multiple commits, revert them one at a time starting from the most recent (newest first) to avoid conflicts. Use `--no-commit` for all but the last, then commit once.

## Commit

Commit messages must follow the Conventional Commits format.

```txt
<type>[optional scope]: <description>
```

Do not use a `body` or `footer`.

### Type

The allowed `type` values are:

```txt
feat
fix
docs
style
refactor
perf
test
build
ci
chore
revert
```

The main `type` values are defined as follows:

| Type       | Description                                 |
| ---------- | ------------------------------------------- |
| `feat`     | Adds a user-facing feature                  |
| `fix`      | Fixes a user-facing bug                     |
| `docs`     | Documentation-only changes                  |
| `refactor` | Code restructuring without behavior changes |
| `test`     | Adds or updates tests                       |
| `chore`    | Other maintenance tasks                     |

### Scope

Add a `scope` when it helps clarify the affected area of the change.

```txt
feat(auth): add login form
fix(api): handle empty response
docs(readme): update setup guide
```

### Description

The `description` should briefly and clearly describe the change.

```txt
feat: add user profile page
fix(auth): prevent expired token login
refactor(store): split user state module
test(login): add invalid password case
chore: update dependencies
```

### Examples

```txt
feat: add dark mode
feat(auth): add OAuth login
fix: handle null user
fix(modal): prevent close button overlap
docs: update README
refactor(api): simplify user service
test: add user service tests
chore: update eslint config
```
