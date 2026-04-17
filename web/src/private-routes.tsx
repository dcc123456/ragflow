import { lazy } from 'react';
import FallbackComponent from './components/fallback-component';
import { Routes } from './routes';

export enum PrivateRoutes {
  Price = '/price',
  Billing = '/billing',
  EvaluationDetail = '/evaluation-detail',
}

export const privateRoutes = [
  {
    layout: false,
    path: Routes.Root,
    Component: () => import('@/layouts/root-layout'),
    children: [
      {
        path: '/user-setting',
        layout: false,
        Component: lazy(() => import('@/pages/user-setting')),
        children: [
          {
            path: `/user-setting/billing`,
            Component: lazy(() => import(`@/pages/billing`)),
          },
        ],
        errorElement: <FallbackComponent />,
      },

      {
        path: `${Routes.Files}${PrivateRoutes.EvaluationDetail}/:id`,
        layout: false,
        Component: lazy(() => import('@/pages/files/evaluation/detail-page')),
        errorElement: <FallbackComponent />,
      },
    ],
  },
  {
    path: PrivateRoutes.Price,
    layout: false,
    Component: lazy(() => import(`@/pages/price`)),
    errorElement: <FallbackComponent />,
  },
];
