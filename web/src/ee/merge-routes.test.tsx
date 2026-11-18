jest.doMock('@/routes', () => jest.requireActual('../routes'));

class TestRequest {
  url: string;
  signal?: AbortSignal;

  constructor(input: string | URL, init?: { signal?: AbortSignal }) {
    this.url = String(input);
    this.signal = init?.signal;
  }
}

Object.assign(globalThis, { Request: TestRequest });

const { routers } = jest.requireActual(
  '@/ee/merge-routes',
) as typeof import('@/ee/merge-routes');
const { PrivateRoutes } = jest.requireActual(
  '@/ee/private-routes',
) as typeof import('@/ee/private-routes');
const { IntegrationChildren } = jest.requireActual(
  '@/layouts/components/app-sidebar/navigation',
) as typeof import('@/layouts/components/app-sidebar/navigation');
const { Routes } = jest.requireActual('@/routes') as typeof import('@/routes');
const { matchRoutes } = jest.requireActual(
  'react-router',
) as typeof import('react-router');

const modelGroupPath = `${Routes.Integrations}${PrivateRoutes.ModelGroup}`;

describe('enterprise bootstrap integration', () => {
  it('keeps shared integration navigation unchanged during application bootstrap', () => {
    expect(IntegrationChildren.map((child) => child.key)).toEqual([
      'model',
      'data-source',
      'chat-channel',
      'mcp',
    ]);
  });

  it('reaches Model Group through the merged integrations settings layout', () => {
    const appShellRoute = routers.routes.filter(
      (route) => route.path === Routes.Root,
    )[1];
    const settingsLayoutRoute = appShellRoute?.children?.filter(
      (route) => route.path === undefined,
    )[2];

    expect(
      settingsLayoutRoute?.children?.some(
        (route) => route.path === `${Routes.Integrations}${Routes.Model}`,
      ),
    ).toBe(true);

    const modelGroupRoute = settingsLayoutRoute?.children?.find(
      (route) => route.path === modelGroupPath,
    );
    expect(modelGroupRoute).toBeDefined();
    expect(modelGroupRoute?.element).toBeDefined();
    expect(modelGroupRoute?.errorElement).toBeDefined();

    const matches = matchRoutes(routers.routes, modelGroupPath);
    expect(matches?.some((match) => match.route === modelGroupRoute)).toBe(
      true,
    );
  });
});
