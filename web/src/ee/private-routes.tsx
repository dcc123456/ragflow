import { Routes } from '@/routes';
import FallbackComponent from '@/components/fallback-component';
import type { ComponentType, ReactNode } from 'react';

export enum PrivateRoutes {
  Price = '/price',
  Billing = '/billing',
  Tickets = '/support',
  TicketCreate = '/support/create',
  TicketDetail = '/support/:id',
  AdminRoleConfigModelProviders = '/admin/roles/:roleName/model-providers',
  AdminModelUsageStatistics = '/admin/model-usage-statistics',
  AdminSettings = '/admin/settings',
  AdminSSOProviders = '/admin/sso',
  AdminModelProviders = '/admin/model-providers',
  Team = '/team',
  AdminTenants = '/admin/tenants',
}

export type PrivateRoute = {
  path?: string;
  Component?: () => Promise<{ default: ComponentType<any> }>;
  errorElement?: ReactNode;
  children?: PrivateRoute[];
  /**
   * Remove these upstream paths (searched recursively) before this route is
   * added. Use it to substitute an upstream page with an enterprise one.
   */
  replace?: string[];
  /**
   * Merge into the n-th upstream sibling carrying the same path (1-based,
   * defaults to 1). `/admin` is both the admin login page and the authorized
   * layout, so the latter is addressed as `matchIndex: 2`.
   */
  matchIndex?: number;
};

/**
 * Enterprise-only routes, merged over the upstream tree at runtime by
 * `merge-routes.tsx` (see `app.tsx`). Never add these entries to
 * `routes.tsx` — that file must stay byte-identical to upstream.
 *
 * Entries use the upstream `LazyRouteConfig` shape (`Component` is a dynamic
 * import) plus the `replace` / `matchIndex` merge directives above. A matched
 * entry only contributes `children`; its `Component` is used only when no
 * upstream route matches and the entry is added as a new route. Pathless
 * entries merge into the n-th pathless sibling.
 */
export const privateRoutes: PrivateRoute[] = [
  {
    // Replace upstream's flat `/login` and `/login-next` pages with the
    // enterprise auth layout (flip effect, register, forget password).
    replace: ['/login', '/login-next'],
    Component: () => import('@/ee/layouts/auth-layout'),
    errorElement: <FallbackComponent />,
    children: [
      {
        Component: () => import('@/ee/pages/login-next/flip-effect-container'),
        children: [
          {
            path: '/login',
            Component: () => import('@/ee/pages/login-next/login'),
          },
          {
            path: '/login-next',
            Component: () => import('@/ee/pages/login-next/login'),
          },
          {
            path: '/register',
            Component: () => import('@/ee/pages/login-next/register'),
          },
        ],
      },
      {
        path: '/forget-password',
        Component: () => import('@/ee/pages/login-next/forget-password'),
      },
    ],
  },
  {
    path: `${PrivateRoutes.Evaluation}/:id`,
    Component: () => import('@/ee/pages/evaluation'),
    children: [{}],
    errorElement: <FallbackComponent />,
  },
  {
    path: PrivateRoutes.Tickets,
    Component: () => import('@/ee/pages/tickets'),
  },
  {
    path: PrivateRoutes.TicketCreate,
    Component: () => import('@/ee/pages/tickets/create'),
  },
  {
    path: PrivateRoutes.TicketDetail,
    Component: () => import('@/ee/pages/tickets/detail'),
  },
  {
    path: Routes.Admin,
    children: [
      {
        path: `${Routes.AdminUserManagement}/:id`,
        Component: () => import('@/pages/admin/user-detail'),
      },
      {
        path: PrivateRoutes.AdminRoleConfigModelProviders,
        Component: () => import('@/ee/pages/admin/roles/model-providers'),
      },
      {
        path: Routes.Admin,
        matchIndex: 2,
        children: [
          {
            // The navigation layout is the only pathless child of the
            // authorized layout.
            children: [
              {
                path: PrivateRoutes.AdminModelUsageStatistics,
                Component: () =>
                  import('@/ee/pages/admin/model-usage-statistics'),
              },
              {
                path: PrivateRoutes.AdminSettings,
                Component: () => import('@/ee/pages/admin/settings'),
              },
              {
                path: PrivateRoutes.AdminSSOProviders,
                Component: () => import('@/ee/pages/admin/sso-providers'),
              },
              {
                path: PrivateRoutes.AdminModelProviders,
                Component: () => import('@/ee/pages/admin/model-providers'),
              },
              {
                path: PrivateRoutes.AdminTenants,
                Component: () => import('@/ee/pages/admin/tenants'),
              },
            ],
          },
        ],
      },
    ],
  },
  {
    // The app shell root; the first `/` route is the standalone home page.
    path: Routes.Root,
    matchIndex: 2,
    Component: () => import('@/layouts/root-layout'),
    children: [
      {
        // Enterprise files page: tab wrapper (files / evaluation) above the
        // upstream file manager; see web/src/ee/pages/files/index.tsx.
        replace: [Routes.Files],
        path: Routes.Files,
        Component: () => import('@/ee/pages/files'),
        errorElement: <FallbackComponent />,
      },
      {
        path: Routes.UserSetting,
        Component: () => import('@/pages/user-setting'),
        children: [
          {
            path: `${Routes.UserSetting}${PrivateRoutes.Billing}`,
            Component: () => import('@/ee/pages/billing'),
          },
          {
            // Substitute upstream's `/user-setting/team` page with the
            // enterprise team page; the shared page stays untouched.
            replace: [`${Routes.UserSetting}${PrivateRoutes.Team}`],
            path: `${Routes.UserSetting}${PrivateRoutes.Team}`,
            Component: () => import('@/ee/pages/user-setting/team'),
            errorElement: <FallbackComponent />,
          },
        ],
        errorElement: <FallbackComponent />,
      },
      {
        path: `${Routes.Files}${PrivateRoutes.EvaluationDetail}/:id`,
        Component: () => import('@/ee/pages/files/evaluation/detail-page'),
        errorElement: <FallbackComponent />,
      },
      {
        path: PrivateRoutes.Price,
        Component: () => import('@/ee/pages/price'),
        errorElement: <FallbackComponent />,
      },
    ],
  },
];
