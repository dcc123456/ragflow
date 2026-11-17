import { render, screen } from '@testing-library/react';
import { matchRoutes } from 'react-router';

jest.mock('react-router', () => ({
  ...jest.requireActual('react-router'),
  createBrowserRouter: (routes: unknown) => ({ routes }),
}));
jest.mock('@/routes', () => jest.requireActual('@/routes.tsx'));
jest.mock('react', () => ({
  ...jest.requireActual('react'),
  lazy: (importer: () => unknown) => () => <div>{String(importer)}</div>,
}));

it('renders enterprise account actions through the matched profile route', () => {
  const { routers } = require('@/ee/merge-routes');
  const matches = matchRoutes(
    routers.routes as import('react-router').RouteObject[],
    '/user-setting/profile',
  );
  const Component = matches?.at(-1)?.route.Component;
  if (!Component) throw new Error('Profile route has no component');
  render(<Component />);
  expect(
    screen.getByText(/@\/ee\/pages\/user-setting\/profile/),
  ).toBeInTheDocument();
});
