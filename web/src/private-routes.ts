import { Routes } from './routes';

export enum PrivateRoutes {
  Price = '/price',
  Billing = '/billing',
}

export const privateRoutes = [
  {
    path: Routes.ProfileSetting,
    layout: false,
    component: `@/pages${Routes.ProfileSetting}`,
    routes: [
      {
        path: `${Routes.ProfileSetting}${PrivateRoutes.Billing}`,
        component: `@/pages${PrivateRoutes.Billing}`,
      },
      {
        path: `${Routes.ProfileSetting}/profile`,
        component: `@/pages${Routes.ProfileSetting}/profile`,
      },
    ],
  },
  {
    path: PrivateRoutes.Price,
    layout: false,
    component: `@/pages${PrivateRoutes.Price}`,
  },
];
