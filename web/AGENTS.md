# AGENTS.md

This file provides guidance to Claude Code (claude.ai/code) when working with the RAGFlow frontend (`web/`).

## Project Overview

RAGFlow frontend is a React/TypeScript application built with Vite:

- **Components**: shadcn/ui
- **Styling**: Tailwind CSS
- **State**: Zustand
- **Data Fetching**: TanStack Query (React Query)
- **i18n**: react-i18next

## Enterprise Extension Seam (Mandatory)

RAGFlow upstream evolves weekly and is merged in regularly. Every edit to a **shared file** (anything under `src/` that upstream also carries) becomes a merge conflict at the next merge-oss. Therefore:

### Rule 1 — Shared files stay byte-identical to upstream

**Never add enterprise-only behavior, translation keys, endpoints, components, or routes directly to a shared file.** After any change under `src/` outside `src/ee/`, verify: `git diff` against the corresponding upstream blob must not show enterprise-only content. Upstreamable bug fixes are the only legitimate shared-file edits.

### Rule 2 — Enterprise code lives in `src/ee/`, mirroring the shared path

Each extension point is a **same-path overlay file**: upstream ships it under `web/src/ee/` as an empty no-op, and this repository fills it with real content. Shared code imports it by the natural specifier (resolving through the generic `@/*` alias to each tree's own file):

| Shared import | Upstream (empty no-op) | Enterprise content |
|---|---|---|
| `@/ee/locales/en-private` | `web/src/ee/locales/en-private.ts` = `{}` | `web/src/ee/locales/en-private.ts` (487 keys) |
| `@/ee/locales/zh-private` | same-path empty object | Chinese enterprise translations |
| `@/ee/locales/zh-traditional-private` | same-path empty object | Traditional Chinese enterprise translations |
| `@/ee/utils/api-private` | same-path empty object | enterprise-only endpoints + path overrides |
| `@/ee/merge-routes` | `export { routers } from '@/routes'` | merges the enterprise route tree over the upstream router |
| `@/ee/components/star-modal` | component returning `null` | GitHub star prompt rendered by the app root |
| `@/ee/services/billingStatus` | `fetchBillingStatus` resolving `false` | runtime `/api/v1/billing/status` lookup |
| `@/ee/utils/private-util` | `redirectToLogin` re-exported from `@/utils/authorization-util` | login redirect that preserves the current path as `?redirect=` |
| `@/ee/utils/response-private` | `handleEnterpriseResponseError` resolving `false` | billing upgrade modal + license-related login silence; `true` suppresses the shared toast |
| `@/ee/hooks/auth-hooks-private` | `useEnterpriseAuthRedirect` returning `undefined` | consumes `?auth=`/`?error=`/`?redirect_url=` on the login callback |
| `@/ee/private-routes` | path enum + empty `privateRoutes` | enterprise route tree merged by `@/ee/merge-routes` |
| `@/ee/constants/team` | permission/role enums and helpers | collaborator permission bit set |
| `@/ee/constants/evaluation` | evaluation enums | evaluation page constants |
| `@/ee/utils/permission-util` | every resource editable by its tenant | `operator_permission` checks (preview/edit/manage/owner) |
| `@/ee/hooks/use-evaluation-navigate` | no-op navigators | evaluation page navigation |
| `@/ee/hooks/use-private-llm-request` | every flag `false`, setter no-op | private-deployment admin/expose requests |
| `@/ee/components/privilege/privilege-dropdown` | component returning `null` | "permission management" dropdown entry |
| `@/ee/components/privilege-management/interface` | shared collaborator types | same types, enterprise call sites |
| `@/ee/components/privilege-management/privilege-management-dialog` | component returning `null` | collaborator management dialog |
| `@/ee/components/duplicate-dialog` | component returning `null` | duplicate-resource dialog |
| `@/ee/pages/*/use-show-privilege-dialog` | closed modal + empty record | per-resource permission dialog state |
| `@/ee/pages/datasets/use-duplicate-dataset` | closed modal | dataset duplicate action |
| `@/ee/layouts/components/notification-banner` | component returning `null` | notification banner above the shell |
| `@/ee/layouts/components/team-invitation-reminder-dialog` | children passthrough | team invitation dialog around the bell |
| `@/ee/pages/agent/online-users` | component returning `null` | agent online-presence list |
| `@/ee/pages/admin/404` | component returning `null` | "feature not enabled" admin page |
| `@/ee/pages/admin/hooks/useAdminVariables` | empty variables | system-variable reads/writes |
| `@/ee/pages/admin/hooks/useRole` | empty, inert results | admin role CRUD requests |
| `@/ee/pages/login-next/forget-password` | components returning `null` | multi-step password reset |
| `@/ee/pages/price/constant` | plan-name enum | pricing catalogue |
| `@/ee/pages/price/global` | provider rendering children | upgrade modal providers |
| `@/ee/pages/price/global/hook` | no-op upgrade prompts | price/upgrade events and modals |
| `@/ee/pages/price/hook/use-price-hooks` | empty plan, no-op billing actions | billing plan and checkout requests |
| `@/ee/pages/user-setting/setting-team/team-management` | component returning `null` | team/department/group management |
| `@/ee/pages/chunk/parsed-result/add-knowledge/components/knowledge-chunk/components/chunk-creating-modal/chunk-version-entry` | component returning `null` | chunk edit history sheet (version list, preview, restore) mounted by the shared chunk editor |
| `@/ee/pages/chunk/components/chunk-card/chunk-version-badge` | component returning `null` | `v{n}` badge on the shared chunk card, naming the version the text shown is at |
| `@/ee/hooks/use-chunk-history-cache` | invalidation callbacks that do nothing | refreshes the chunk version history and the version badge after an edit or a switch |
| `@/ee/hooks/use-dataset-sidebar-extra-items` | `[]` | version history entry in the dataset sidebar |

Locale and API overlays are merged via `lodash/merge` in the shared file (`src/locales/config.ts`, `src/utils/api.ts`); overlay keys override same-named base keys, new keys are additive.

Routing is the one exception: upstream exposes no same-path hook for `src/routes.tsx`, so enterprise routes are declared in `src/ee/private-routes.tsx` and merged at runtime by `src/ee/merge-routes.tsx` (see Rule 3). `src/app.tsx` imports its router from `@/ee/merge-routes` in both trees; upstream's copy of that file only re-exports `routers` from `@/routes`.

### Rule 3 — Where new enterprise content goes

- **New enterprise-only API endpoint** → add the key to `src/ee/utils/api-private.ts` (function values use the `const webAPI = '/v1'; const restAPIv1 = '/api/v1';` consts already declared in that file). Do **not** add it to `src/utils/api.ts`.
- **New enterprise-only translation key** → add it to the matching `src/ee/locales/<lang>-private.ts` (deep-merge: nest it under the same section path as `en.ts`/`zh.ts` would use). Do **not** add it to `src/locales/en.ts` or `src/locales/zh.ts`.
- **Enterprise-only component, hook, service** → place the whole implementation under `src/ee/<same-path>/` and, when a shared file must reach it, extend the same-path overlay pattern: add an empty no-op file under `web/src/ee/` **upstream** (or propose it), then fill it here. Never fork the shared file.
- **Enterprise-only type module** → keep it under `src/ee/` and ship an upstream counterpart only when a **shared** file imports it. When a shared file needs the prop shape of an enterprise overlay component, derive it (`ComponentProps<typeof Component>['prop']`) instead of importing the enterprise type, so the open-source tree carries no enterprise type definitions. Do not add overlay stubs that no shared file imports.
- **Enterprise content that must be inserted into a shared component** (nav entries, header badges, extra toolbar buttons) → do not fork the component. Give the shared file **one inert extension point** importing a same-path `src/ee/` overlay, then fill the overlay:
  - render slot → overlay exports a component that `return null` upstream (e.g. `@/ee/layouts/components/header-enterprise` → `EnterpriseHeaderUpgradeBadge` / `EnterpriseHeaderTicketsLink` / `EnterpriseHeaderPlanBadge`);
  - list extension → overlay exports a hook returning `{ items, hiddenPaths }` with `{ items: [], hiddenPaths: [] }` upstream (e.g. `@/ee/pages/admin/layouts/navigation-private` → `useEnterpriseAdminNav`).
  The overlay owns all enterprise hooks/queries/navigation so the shared file needs no enterprise state; keep overlay identities stable (`useMemo`) because the shared file lists them in `useMemo` deps.
- **Enterprise-only helper inside a shared hook** (e.g. `useNavigatePage`) → leave the shared hook byte-identical and put the helper in its own hook under `src/ee/hooks/`, updating enterprise call sites. Do not add the helper to the shared hook "for convenience".
- **Generic utility that enterprise code happens to use** (date/number/string formatting, parsing, validation, URL helpers) → add it to the shared module under `src/utils/` and ship the same text upstream in the same change; enterprise files import it from `@/utils/...`. Do **not** park it in `src/ee/`: a generic helper is not enterprise behavior, so hiding it in the overlay forks the shared module for no reason and leaves the open-source build without it. `src/ee/` only holds logic that exists solely in the enterprise edition.
- **Enterprise-only route** → declare the entry in `src/ee/private-routes.tsx`: add a `PrivateRoutes` enum member for any path enterprise code navigates to, then add the route tree entry. `src/ee/merge-routes.tsx` merges this tree over the router upstream already built, so the entry must reach its target with `matchIndex` (n-th sibling with the same path, 1-based) and/or `replace` (upstream paths to substitute) instead of editing `src/routes.tsx`. Path constants come from `PrivateRoutes`, never from upstream `Routes`. Never edit `src/routes.tsx`: it must stay byte-identical to upstream.

### Rule 4 — No per-file build aliases

Do **not** add per-file entries to `vite.config.ts` / `jest.config.ts` / `tsconfig.json` to swap overlay implementations. The generic `@/*` mapping already resolves `@/ee/...` to each tree's own `src/ee/` file — that is the whole point. If an `@/ee/...` import fails to resolve, the fix is the file's location or name, not new build config.

### Rule 5 — No compatibility shims

When upstream changes something an overlay covers, update the overlay to match; do not keep both old and new keys, dead branches, or "move later" notes inside `src/ee/`. Delete overlay keys that upstream no longer uses.

### Rule 6 — Every shared-file change ships to the open-source tree in the same change

A shared file only stays conflict-free while both trees carry the same text. Every task that adds or reshapes a seam must therefore produce, in the **same change set**, the open-source counterpart on its own branch (`feat/...`, never a release branch):

- the empty no-op overlay at the same path under `web/src/ee/` (or the inert extension point inside the shared file, e.g. a render slot's `return null`), and
- the identical shared-file text, including the import switched to the `@/ee/...` specifier.

**Why:** a seam that exists only in this repository is not a seam — the next merge-oss reintroduces exactly the conflict it was built to remove, and the OSS build has no no-op to resolve the new `@/ee/...` import.

**How to apply:** when a task touches a shared file or creates an overlay, pair it with the OSS change before reporting the task done. Verify each pair with `diff --strip-trailing-cr <oss-file> <ee-file>` (must be byte-identical) and record the OSS branch in the commit message. If the texts cannot match — the enterprise content is still inline in a shared file — do not ship half a seam: move the content into an overlay first, or leave the pair unchanged and report the remaining fork.

## Common Commands

```bash
npm install
npm run dev        # Development server
npm run build      # Production build
npm run lint       # oxlint
npm run format     # oxfmt
npm run test       # Jest tests
```

## Development Conventions

### Pre-commit Format Validation (Mandatory)

Frontend code changes **must pass the lefthook format checks before every commit**. Do not rely on CI (`web-oxfmt` / `web-oxlint` jobs) to catch formatting after pushing.

- Before staging or committing changed `.ts`/`.tsx`/`.js`/etc. files, run the formatter and linter locally:
  - `npx oxfmt <changed files>` (or `npm run format`) to auto-fix formatting
  - `npx oxlint` (or `npm run lint`) for lint issues
- Verify with `npx oxfmt --check <changed files>` before `git commit`.
- Let the lefthook pre-commit hook run to completion and fix everything it reports; never bypass hooks with `--no-verify`.

### Test File Placement

When tests are grouped in a dedicated directory, that directory is always named `__tests__/` — never `tests/`. Example: `src/pages/agent/utils/__tests__/extractor-transform.test.ts` for `src/pages/agent/utils/extractor-transform.ts`. Co-located `foo.test.ts(x)` files next to the source file are also fine. When you find an existing test under a `tests/` directory, move it into `__tests__/` (keep git history via `git mv`) rather than adding new tests there.

### Backend Conventions

The frontend serves only the **Go** backend. There is no runtime backend detection and no per-backend branching in app code; the dev server proxies `/api` and `/v1` to the Go API (`vite.config.ts`).

Do not reintroduce backend-variant primitives or detection helpers (e.g. a `useIsGoBackend` hook, `pickByBackend`, `<BackendVariant>`, or an `/api/v1/language` probe) and do not create `go/`-vs-`python/` variant directories or files.

### Enterprise Overlays

The frontend mirrors the backend's `stub.go` / `*_ee.go` naming. `src/ee-stub/` holds the no-op customization points for shared modules; the enterprise repository carries real implementations at the same relative paths under `src/ee/`. Shared code always imports the logical `@/ee/...` specifier, and the OSS directory-level aliases resolve it to `src/ee-stub/`.

- `src/ee-stub/locales/<lang>-private.ts` and `src/ee-stub/utils/api-private.ts` stay empty objects here; their enterprise counterparts under `src/ee/` carry private locale strings and API endpoints.
- The shared file only imports the overlay and merges it — `merge(base, private)` in `src/locales/config.ts`, `merge(apiEndpoints, apiPrivate)` in `src/utils/api.ts`. Never inline enterprise strings or endpoints into `src/locales/*.ts` or `src/utils/api.ts`, and never delete the overlay import.
- The edition is chosen by directory aliases and file content, never at runtime: no `import.meta.env` switch that selects enterprise code and no `isEnterprise` branch in shared modules.
- Keep the `@/ee` mappings in `vite.config.ts`, `tsconfig.json`, `jest.config.ts`, and `.storybook/main.ts` ahead of the generic `@` / `@/*` mapping. Do not add per-file aliases or rewrite shared imports to `@/ee-stub`.
- Adding a new overlay means adding the empty file under `src/ee-stub/` here and the implementation at the same relative path under `src/ee/` downstream in the same change.

### CSS and Layout Debugging

When fixing CSS/layout issues (especially flex truncation, ellipsis, or element sizing), **always inspect the full parent hierarchy** for `flex-shrink`, `min-width`, and `overflow` constraints before applying fixes like `min-w-0`. Do not repeatedly apply the same fix without verifying the root cause.

- Before editing, explain: (1) the full flex/container hierarchy from the target element up to the nearest non-flex ancestor, (2) what constraint is actually causing the bug, and (3) how the proposed fix addresses that root cause.

### Text Overflow

Any element rendering **user-generated or server-returned text** (names, titles, descriptions, file names, messages) inside a bounded container (card, table cell, dialog title, list item, badge, select trigger, button) must have an explicit overflow strategy. Never assume real data is short.

- Choose deliberately: single-line `truncate`, multi-line `line-clamp-N`, or intentional wrapping with `break-words`. Rendering with no strategy is a defect — long real-world data always finds it.
- `truncate` needs a width constraint to bite: add `min-w-0` on every flex ancestor between the text and the element that actually constrains the width (flex children default to `min-width: auto` and won't shrink below content size).
- Keep truncated text revealable: prefer the shared `TruncatedText` component (`src/components/truncated-text.tsx`), which shows a tooltip only when truncation actually occurs.
- Long i18n strings count as long text — a label that fits in Chinese may overflow in English.
- When overflow is suspected or the user asks, verify with extreme input (100+ characters, an unbroken no-space string, a CJK/Latin mix) using the `overflow-check` skill (`.claude/skills/overflow-check`), which scans the rendered page in a real browser. jsdom and lint cannot measure layout.

### Color Tokens

When writing or modifying styles, **use the project-defined color tokens from `src/tailwind.css`** (e.g., `bg-bg-base`, `text-text-primary`, `text-text-secondary`, `text-text-disabled`, `border-border-button`, `bg-bg-card`). Do not use arbitrary hex/RGB values or Tailwind's default palette colors (e.g., `emerald-500`, `blue-400`) directly in component class names. These tokens are defined for both light and dark modes and keep the UI consistent with the design system.

### Scope and Boundaries

Respect explicit boundaries from the user. If the user says **"only fix the selected line"** or **"do not touch shared types/files"**, follow that instruction exactly. Do not investigate unrelated errors, modify shared schemas (e.g., `LlmSettingFieldSchema`), or refactor other files without confirmation. If a change outside the described scope seems necessary, ask for permission first.

### Internationalization (i18n)

For translation tasks, add keys **only to the explicitly requested language files** (commonly `src/locales/zh.ts` and `src/locales/en.ts`). Do not auto-propagate changes to all language files unless the user explicitly asks. Strings that exist only in the enterprise edition belong in `src/ee/locales/<lang>-private.ts`, never in the shared files (see Enterprise Overlays).

**Enterprise-only keys are the exception**: they go to the matching `src/ee/locales/<lang>-private.ts` overlay (see *Enterprise Extension Seam* above), never to `src/locales/en.ts` / `zh.ts` — keys in the shared locale files break the byte-identical-to-upstream guarantee and conflict on every merge-oss.

- **Style for `en.ts`**: Sentence case — first word capitalized, rest lowercase (e.g., `referenceAnswer: 'Reference answer'`). Proper nouns remain as-is.

### React Component Refactoring

When refactoring or extracting components, **verify layout behavior after each structural change** (especially `flex-1`, conditional rendering, or flex direction changes). Check that existing buttons, alignment, and responsive behavior remain intact. After extraction, verify: (1) all original props and behavior are preserved, (2) layout in parent contexts is identical, and (3) no syntax or type errors were introduced.

### State Management and Data Fetching

#### Query Key Factory (Mandatory)

**Never write raw `queryKey` arrays inline.** Always use a query key factory object that returns `as const` tuples. Raw arrays duplicated across `useQuery` and `invalidateQueries` are brittle, unreadable, and cause stale-cache bugs when key structures drift.

```ts
// ❌ Bad — raw array, hard to match with useQuery
queryClient.invalidateQueries({
  queryKey: [
    LLMApiAction.AddedProviders,
    params.provider_name,
    params.instance_name,
    'models',
  ],
});

// ✅ Good — factory reference, self-documenting
queryClient.invalidateQueries({
  queryKey: LlmKeys.instanceModels(params.provider_name, params.instance_name),
});
```

- Place the factory in the same file as the hooks, named `{Domain}Keys` (e.g., `LlmKeys`, `DatasetKeys`).
- Every `useQuery` and every `invalidateQueries` must reference the same factory function.
- Use `as const` on each factory return value for type-safe readonly tuples.

#### Cache Debugging

For React Query / cache invalidation bugs, **carefully compare query keys across all consuming components and mutation hooks**. Mismatched keys (e.g., with/without `refreshCount`) are a common root cause of stale data or duplicate requests.

- Systematically: (1) list every component/hook that calls `useQuery` for this data, (2) compare their query keys character-for-character, (3) check every mutation's `onSuccess` for cache invalidation, and (4) verify no parent re-renders are remounting the observer.

#### Colocate Queries with the Consuming View

**Fire a query in the component that renders its data — not in a parent page.** When a page switches between mutually exclusive views (tabs, view modes), extract each view into its own component that issues its own requests on mount. Conditional rendering then provides lazy loading for free.

- Do not hoist child-view queries into the page component — it fires requests the user may never need (e.g., fetching the skill tree on page entry while the default view is the LLM wiki).
- Do not thread `enabled` flags or view-mode props through hooks to gate a hoisted query; that is a sign the query lives at the wrong level. Split the view instead.
- Remember the trade-off: with `gcTime: 0`, unmounting a view drops its cache, so switching back refetches. That is usually desirable for always-fresh data — do not reintroduce eager hoisting just to avoid the refetch.

### Network Request Layering

HTTP requests are organized in three layers. **Never import `@/utils/request`, `@/utils/next-request`, or `@/utils/api` directly inside a hook**:

1. `src/hooks/use-xx-request.ts(x)` — React Query hooks; only call the service layer.
2. `src/services/xx-service.ts` — Register endpoints via `registerNextServer`, all going through `@/utils/next-request`.
3. `src/utils/next-request.ts` — The single axios instance; handles token, 401 redirects, and error notifications.

Interface types are split between two folders:

- Response/data shape → `src/interfaces/database/xx.ts`
- Request params/body → `src/interfaces/request/xx.ts`

Model-related endpoints (LLM provider / factory / my LLM, etc.) are consolidated in `src/services/llm-service.ts` rather than scattered across hooks. For GET endpoints, register with `method: 'get'` in the service, and on the call site pass `true` as the second argument to use the native axios config (e.g., `service.listProviders({ params: { available: true } }, true)`).

### Module Dependency Direction

`src/components/`, `src/hooks/`, `src/services/`, and `src/utils/` are shared layers: **they must not import from `src/pages/`**. A page may import from shared layers, never the reverse. (Legacy exceptions exist, e.g. `src/components/pipeline-operator-tabs/`; do not add new ones, and prefer lifting when you touch such code.)

When page code gains a second consumer (e.g. a page form reused inside a dialog), lift the **whole self-contained subtree** — its `components/`, `hooks/`, `schema.ts`, `constant.ts`, `utils.ts` — into a feature folder under `src/components/` (precedent: `compilation-template-group/`, `pipeline-operator-tabs/`) using `git mv` to preserve history. Keep the subtree's internal structure and relative imports unchanged, and slim the route page down to a thin wrapper (route params, navigation, header). Never lift a single file out of a subtree — that multiplies cross-layer imports instead of removing them. Page-only logic (route params, return-navigation helpers) stays in the page directory.

`src/utils/` is for pure functions only — shared React components and hooks live in `src/components/` and `src/hooks/`, never in `src/utils/`.

### Shared UI Component Lock

The folder `src/components/ui/` is the project's **shared UI library** — it contains both official shadcn/ui primitives and project-authored common components built on top of shadcn. Both kinds are intended to be reused across the app and **must not be modified casually**.

- **Do not modify, refactor, restyle, or "improve"** any file under `src/components/ui/` (including subfolders), even if it seems like the most direct fix.
- If a component does not meet requirements, **wrap or compose it** in a new component **outside** `src/components/ui/` (e.g., under `src/components/` or a feature folder), and customize via `className`, `props`, or composition.
- Exceptions require **explicit user approval** in the same conversation. When in doubt, ask first and propose a wrapper-based alternative.
- Adding a new shared component to `src/components/ui/`, or upgrading a shadcn primitive via the official `shadcn` CLI, is allowed only when the user explicitly requests it.

### Prefer Shared Building Blocks

**Do not hand-roll ad-hoc elements when a shared component, hook, util, or constant already covers the need.** Before writing a raw styled element or a local helper, check the shared layers first:

- Interactive elements (buttons, inputs, tooltips, dialogs, pagination, …) → compose `src/components/ui/` primitives via `variant`/`size`/`className` (e.g. an icon-only action is `<Button variant="ghost" size="icon">` or `variant="static" size="auto"`, not a raw `<button className="...">`).
- Reusable logic → existing hooks in `src/hooks/` (or `ahooks`).
- Pure helpers → existing utils in `src/utils/` (or `lodash`/`dayjs`).
- Domain constant lists → the page's `constant/` directory.

Writing your own one-off version of something the shared layer already provides is a defect, not a style choice. Beyond the `ui/` primitives, `src/components/` already covers most recurring needs — empty states, truncation tooltips, confirm/rename dialogs, list-page scaffolding (filter bar, pagination, skeletons), icons, avatars, and layout wrappers — so skim `src/components/`, `src/hooks/`, and `src/utils/` before writing anything new.

### React Patterns and Conventions

- **Avoid inline event handlers.** Do not define arrow functions directly on JSX event props like `onClick={() => ...}` or `onChange={(e) => ...}`. Instead, extract the handler and reference it by name (e.g., `onClick={handleClick}`). Inline handlers are recreated on every render, which can break `React.memo` optimizations and make stack traces harder to read.
- **Prefer `requestAnimationFrame` or `useLayoutEffect`** over `setTimeout(..., 0)` for focus or DOM measurement operations.
- **Prefer `useTranslation` from `react-i18next`** over project-wrapped utilities like `useTranslate`.
- Extract complex logic into hooks or utils; keep components lean.
- Use **PascalCase** for constants and component names.
  - Components: `EditableTextarea`, `RAGFlowFormItem`
  - Constants: `InitialMockData`, `DefaultPlaceholder`
- Avoid camelCase or SCREAMING_SNAKE_CASE for components and top-level constants.
- **Place domain constant lists in the page's `constant/` directory** (e.g. `pages/agent/constant/pipeline.tsx`) when they belong to an existing family of constants there (like `ChunkerOperators`, `SingleOperators`), instead of declaring them locally next to their consumer. Keeping related lists in one file makes their overlaps and differences visible and gives new entries a single obvious home.
- **Name things semantically, not generically.** A name should say what the value holds or what the callback does, so call sites read naturally. Don't leak a generic underlying-primitive API into your own component's public props — rename at the wrapper boundary (e.g., a tabs wrapper exposes `activeTab` / `onTabChange`, mapped internally to Radix's `value` / `onValueChange`), and align with what callers already name the state. Same for variables and functions: `hiddenNodeIds` over `nodeIdSet`, `pickByBackend` over `resolveVariant`.
- **Use enums for repeated finite string values.** When the same string value is referenced in multiple places (state initialization, comparisons, JSX values, or similar logic), define a string enum and use its members instead of repeating raw string literals.
- Avoid duplicating component structures in JSX; favor render props or reusable components.

### Utility Libraries and Reuse

- **Time/date handling**: Use `dayjs` for all date/time formatting, parsing, and manipulation.
- **Utility hooks**: Prefer `ahooks` for common reusable hooks (e.g., `useDebounce`, `useSetState`).
- **General utilities**: Lodash is available for utility functions when needed.
- **Project utilities first**: Before reaching for a third-party library, check if the project already has an existing utility or hook that covers the need.
- **Extract and share**: If repeated logic cannot be satisfied by an existing project utility or a third-party library, extract it into an appropriate shared hook (`src/hooks/`) or utility file (`src/utils/`).
- **Check for duplicate patterns before adding**: When asked to add logic (validation, existence checks, API calls, etc.), first search for existing hooks/functions that do the same or similar thing — especially in the same file or sibling hooks. If a hook already does X, call it instead of re-implementing X inline. This applies to mutations calling mutations, utility wrappers, and boilerplate around API calls.
