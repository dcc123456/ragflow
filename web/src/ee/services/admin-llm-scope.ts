/*
 *  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
 *
 *  Licensed under the Apache License, Version 2.0 (the "License");
 *  you may not use this file except in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing, software
 *  distributed under the License is distributed on an "AS IS" BASIS,
 *  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  See the License for the specific language governing permissions and
 *  limitations under the License.
 */

import { restAPIv1 } from '@/utils/api';
import request from '@/utils/next-request';

/**
 * Route-scoped rewriting of model-provider requests onto the admin gateway.
 *
 * The admin "model providers" page reuses the tenant page's components
 * wholesale — `Sidebar`, `ProviderHeaderBar`, `ProviderInstanceCard` and
 * everything beneath them. Those components reach the backend through the
 * shared `llmService` / `use-llm-request` hooks, which hardcode tenant
 * paths.
 *
 * Redirecting them the obvious way (thread a swapped service through ~20
 * hooks, or duplicate the page) would put permanent merge conflicts on
 * open-source files that currently track upstream almost verbatim —
 * `use-llm-request.tsx` and `llm-service.ts` differ from upstream by only a
 * couple of lines. So instead we attach one axios request interceptor to the
 * shared instance from this file, which is enterprise-only, and rewrite the
 * URL there. Nothing in `use-llm-request.tsx`, `llm-service.ts`,
 * `next-request.ts` or the `setting-model` components has to change.
 *
 * The scope is taken from the current route: the admin app lives entirely
 * under `/admin` and is a disjoint route tree from the tenant settings page,
 * so "which route am I on" is exactly "which gateway should serve this".
 *
 * `/api/v1/admin/providers/...` is a 1:1 mirror of `/api/v1/providers/...`
 * (see `internal/cli/common_command.go`, whose AdminMode and APIMode
 * branches pair up call for call), which is what makes a pure prefix
 * insertion correct.
 */

/** Gateway prefix the admin routes are served under. */
const ADMIN_API_PREFIX = `${restAPIv1}/admin`;

/** First route segment that marks the admin app. */
const ADMIN_ROUTE = 'admin';

/**
 * Tenant paths that have an admin mirror. The whole `/providers` tree is
 * mirrored, so it matches by prefix. `/models` matches exactly: its only
 * admin counterpart is the list endpoint, while `/models/default` is
 * tenant-only (admin manages no per-tenant default model) and must pass
 * through untouched.
 */
const MIRROR_PREFIXES = [`${restAPIv1}/providers`];
const MIRROR_EXACT_PATHS = [`${restAPIv1}/models`];

/**
 * True while the app is on an admin route. Basename-aware, because
 * `window.location.pathname` still contains `VITE_BASE_URL` whereas the
 * router reports paths without it.
 */
function isAdminScope(): boolean {
  const base = import.meta.env.VITE_BASE_URL || '/';
  const pathname = window.location.pathname;
  const relative =
    base !== '/' && pathname.startsWith(base)
      ? pathname.slice(base.length - 1)
      : pathname;
  const [first] = relative.split('/').filter(Boolean);
  return first === ADMIN_ROUTE;
}

/**
 * Maps a tenant model-provider URL onto the admin gateway when the app is
 * on an admin route. Every other URL is returned unchanged — including
 * `/api/v1/admin/*` calls that are already addressed correctly, and
 * `/api/v1/models/default` — so this is a no-op for all other traffic.
 */
export function rewriteAdminLlmUrl(url?: string): string | undefined {
  if (!url || !isAdminScope()) return url;

  // `registerNextServer` passes query params separately, but a URL template
  // may still carry a query suffix; preserve whatever follows the path.
  const queryIndex = url.indexOf('?');
  const path = queryIndex === -1 ? url : url.slice(0, queryIndex);
  const query = queryIndex === -1 ? '' : url.slice(queryIndex);

  const mirrored =
    MIRROR_EXACT_PATHS.includes(path) ||
    MIRROR_PREFIXES.some(
      (prefix) => path === prefix || path.startsWith(`${prefix}/`),
    );
  if (!mirrored) return url;

  return `${ADMIN_API_PREFIX}${path.slice(restAPIv1.length)}${query}`;
}

// Registered once, at import time, on the axios instance every `llmService`
// call goes through (`utils/register-server` -> `utils/next-request`). It
// stays installed after leaving the admin area; harmless, since
// `rewriteAdminLlmUrl` is gated on the route.
request.interceptors.request.use((config) => {
  config.url = rewriteAdminLlmUrl(config.url);
  return config;
});
