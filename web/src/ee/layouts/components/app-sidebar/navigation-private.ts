import { PrivateRoutes } from '@/ee/private-routes';
import type { IntegrationChild } from '@/layouts/components/app-sidebar/navigation';
import { Routes } from '@/routes';

const modelGroupIntegrationChild: IntegrationChild = {
  key: 'model-group',
  path: `${Routes.Integrations}${PrivateRoutes.ModelGroup}`,
  labelKey: 'setting.modelGroup',
};

/** Add Model Group below Model without mutating the shared navigation. */
export function addModelGroupIntegrationNavigation(
  children: IntegrationChild[],
): IntegrationChild[] {
  const modelIndex = children.findIndex((child) => child.key === 'model');
  if (modelIndex === -1) return children;

  return [
    ...children.slice(0, modelIndex + 1),
    modelGroupIntegrationChild,
    ...children.slice(modelIndex + 1),
  ];
}
