import { privateRoutes } from './private-routes';
import { default as routes } from './routes';

type RouteType =
  | {
      component?: string | undefined;
      layout?: false | undefined;
      path?: string | undefined;
      redirect?: string | undefined;
      routes?: Array<RouteType>;
      wrappers?: Array<string> | undefined;
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
        routes: mergeRoutes(route.routes, currentRoute.routes),
      };
    }
  }

  return nextRoutes;
}

export const nextRoutes = mergeRoutes(privateRoutes, routes);
