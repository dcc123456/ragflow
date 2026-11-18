import { PrivateRoutes } from '@/ee/private-routes';
import { IntegrationChildren } from '@/layouts/components/app-sidebar/navigation';
import { Routes } from '@/routes';
import { addModelGroupIntegrationNavigation } from './navigation-private';

describe('enterprise integrations navigation', () => {
  it('adds Model Group directly below Model without mutating shared navigation', () => {
    const children = addModelGroupIntegrationNavigation(IntegrationChildren);

    expect(children.map((child) => child.key)).toEqual([
      'model',
      'model-group',
      'data-source',
      'chat-channel',
      'mcp',
    ]);
    expect(children[1]).toEqual({
      key: 'model-group',
      path: `${Routes.Integrations}${PrivateRoutes.ModelGroup}`,
      labelKey: 'setting.modelGroup',
    });
    expect(IntegrationChildren.map((child) => child.key)).not.toContain(
      'model-group',
    );
  });

  it('returns the original navigation when Model is absent', () => {
    const children = [IntegrationChildren[1]];

    expect(addModelGroupIntegrationNavigation(children)).toBe(children);
  });
});
