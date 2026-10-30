import {
  ForwardRefExoticComponent,
  LazyExoticComponent,
  RefAttributes,
} from 'react';
import { createBrowserRouter } from 'react-router';
import { privateRoutes } from './private-routes';
import { routeConfig } from './routes';

type RouteType =
  | {
      path: string;
      Component: LazyExoticComponent<
        ForwardRefExoticComponent<RefAttributes<unknown>>
      >;
      layout: boolean;
      errorElement: JSX.Element;
      wrappers?: undefined;
      children?: undefined;
    }
  | { [x: string]: any };

export function mergeRoutes(
  privateRouteList: RouteType[] = [],
  routeList: RouteType[] = [],
) {
  const nextRoutes = [...routeList];

  for (const route of privateRouteList) {
    const currentRouteIdx = nextRoutes.findIndex((x) => x.path === route.path);
    if (currentRouteIdx === -1) {
      nextRoutes.push(route);
    } else {
      const currentRoute = nextRoutes[currentRouteIdx];

      nextRoutes[currentRouteIdx] = {
        ...currentRoute,
        children: mergeRoutes(route.children, currentRoute.children),
      };
    }
  }

  return nextRoutes;
}
export const nextRoutes = mergeRoutes(privateRoutes, routeConfig);

const routers = createBrowserRouter(nextRoutes, {
  basename: import.meta.env.VITE_BASE_URL || '/',
});

export { routers };
