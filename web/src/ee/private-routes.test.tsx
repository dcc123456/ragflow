import { Routes } from '@/routes';
import { PrivateRoutes, privateRoutes } from './private-routes';

it('registers Model Group under the integrations settings layout', () => {
  expect(PrivateRoutes.ModelGroup).toBe('/model-group');

  const appShellRoute = privateRoutes.find(
    (route) => route.path === Routes.Root && route.matchIndex === 2,
  );
  expect(appShellRoute).toBeDefined();

  const settingsLayoutRoute = appShellRoute?.children?.find(
    (route) => route.path === undefined && route.matchIndex === 3,
  );
  expect(settingsLayoutRoute).toBeDefined();

  const modelGroupPath = `${Routes.Integrations}${PrivateRoutes.ModelGroup}`;
  const modelGroupRoute = settingsLayoutRoute?.children?.find(
    (route) => route.path === modelGroupPath,
  );
  expect(modelGroupRoute).toBeDefined();
  expect(modelGroupRoute?.path).toBe(modelGroupPath);
  expect(typeof modelGroupRoute?.Component).toBe('function');
  expect(modelGroupRoute?.errorElement).toBeDefined();

  const userSettingRoute = appShellRoute?.children?.find(
    (route) => route.path === Routes.UserSetting,
  );
  expect(
    userSettingRoute?.children?.some(
      (route) =>
        route.path === `${Routes.UserSetting}${PrivateRoutes.ModelGroup}`,
    ),
  ).toBe(false);
});
