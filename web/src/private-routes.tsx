import { lazy } from 'react';
import FallbackComponent from './components/fallback-component';

export enum PrivateRoutes {
  Price = '/price',
  Billing = '/billing',
}

export const privateRoutes = [
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
    path: PrivateRoutes.Price,
    layout: false,
    Component: lazy(() => import(`@/pages/price`)),
    errorElement: <FallbackComponent />,
  },
];
