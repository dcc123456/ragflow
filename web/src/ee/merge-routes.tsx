import { lazy, Suspense } from 'react';
import type { ComponentType, FC } from 'react';
import { createBrowserRouter } from 'react-router';
import type { NonIndexRouteObject } from 'react-router';
import { privateRoutes } from '@/ee/private-routes';
import type { PrivateRoute } from '@/ee/private-routes';
import { routers as upstreamRouters } from '@/routes';

/**
 * Enterprise route seam.
 *
 * Upstream `routes.tsx` must stay byte-identical, so it neither exports its
 * route config nor knows about enterprise routes. This module takes the tree
 * upstream already built (`routers.routes`), merges the enterprise tree from
 * `private-routes.tsx` into it, and builds the only router the app renders
 * (see `app.tsx`). Match semantics live in `private-routes.tsx`.
 */

type RouteNode = Omit<NonIndexRouteObject, 'children'> & {
  id?: string;
  children?: RouteNode[];
};

const defaultRouteFallback = (
  <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 backdrop-blur-[1px]">
    <div className="h-8 w-8 animate-spin rounded-full border-2 border-white/70 border-t-transparent" />
  </div>
);

/** Same wrapping upstream applies to `Component` imports in `routes.tsx`. */
function withLazyRoute(
  importer: () => Promise<{ default: ComponentType<any> }>,
): FC<any> {
  const LazyComponent = lazy(importer);
  const Wrapped: FC<any> = (props) => (
    <Suspense fallback={defaultRouteFallback}>
      <LazyComponent {...props} />
    </Suspense>
  );
  Wrapped.displayName = 'LazyRoute';
  return Wrapped;
}

/**
 * Drop the route ids the upstream router generated. They are positional, so
 * reusing them after inserting enterprise routes shifts positions and trips
 * react-router's global id uniqueness check; `createBrowserRouter` derives
 * fresh ids for the final tree.
 */
function stripRouteIds(routes: RouteNode[]): RouteNode[] {
  return routes.map((route) => {
    const node: RouteNode = { ...route };
    delete node.id;
    delete node.children;
    if (route.children) node.children = stripRouteIds(route.children);
    return node;
  });
}

/** Remove every route whose `path` is listed, searching the whole tree. */
function removeRoutes(routes: RouteNode[], paths: string[]): RouteNode[] {
  return routes
    .filter((route) => !paths.includes(route.path as string))
    .map((route) =>
      route.children
        ? { ...route, children: removeRoutes(route.children, paths) }
        : route,
    );
}

/** Index of the `matchIndex`-th sibling whose path equals `path`. */
function findMatchIndex(
  routes: RouteNode[],
  path: string | undefined,
  matchIndex: number,
) {
  let seen = 0;
  for (let index = 0; index < routes.length; index++) {
    if (routes[index].path !== path) continue;
    seen += 1;
    if (seen === matchIndex) return index;
  }
  return -1;
}

/** Convert enterprise routes into real route objects (wrapping components). */
function toRouteObjects(routes: PrivateRoute[]): RouteNode[] {
  return routes.map((route) => {
    const node: RouteNode = {};
    if (route.path !== undefined) node.path = route.path;
    if (route.errorElement) node.errorElement = route.errorElement;
    if (route.Component) node.Component = withLazyRoute(route.Component);
    if (route.children) node.children = toRouteObjects(route.children);
    return node;
  });
}

export function mergeRoutes(
  privateRouteList: PrivateRoute[],
  routeList: RouteNode[],
): RouteNode[] {
  const nextRoutes = [...routeList];
  const inserted: RouteNode[] = [];

  for (const route of privateRouteList) {
    if (route.replace?.length) {
      const remaining = removeRoutes(nextRoutes, route.replace);
      nextRoutes.length = 0;
      nextRoutes.push(...remaining);
    }

    const matchIdx = findMatchIndex(
      nextRoutes,
      route.path,
      route.matchIndex ?? 1,
    );
    if (matchIdx === -1) {
      inserted.push(...toRouteObjects([route]));
      continue;
    }
    if (!route.children?.length) continue;

    const baseRoute = nextRoutes[matchIdx];
    nextRoutes[matchIdx] = {
      ...baseRoute,
      children: mergeRoutes(route.children, baseRoute.children ?? []),
    };
  }

  // Enterprise routes come first so they win rank ties against upstream
  // duplicates of the same path (e.g. `/admin/users/:id`).
  return [...inserted, ...nextRoutes];
}

const nextRoutes = mergeRoutes(
  privateRoutes,
  stripRouteIds(upstreamRouters.routes as RouteNode[]),
);

const routers = createBrowserRouter(nextRoutes, {
  basename: import.meta.env.VITE_BASE_URL || '/',
});

export { routers };
