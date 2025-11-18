export enum PrivateRoutes {
  Price = '/price',
  Billing = '/billing',
}

export const privateRoutes = [
  {
    path: '/user-setting',
    layout: false,
    component: '@/pages/user-setting',
    routes: [
      {
        path: `/user-setting${PrivateRoutes.Billing}`,
        component: `@/pages${PrivateRoutes.Billing}`,
      },
    ],
  },
  {
    path: PrivateRoutes.Price,
    layout: false,
    component: `@/pages${PrivateRoutes.Price}`,
  },
];
