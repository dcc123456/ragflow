import { IAddedModel } from '@/interfaces/database/llm';
import { useCallback, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import {
  buildModelGroupRef,
  isModelGroupRef,
} from '@/ee/utils/model-group-util';
import { useModelGroupOptions } from './use-model-group-options';
import { useModelGroupTenantAccess } from './use-model-group-tenant-access';

type AdditionalModelValidIdsOptions = {
  modelTypes: string[];
  models: IAddedModel[];
  modelsFetched: boolean;
  enabled: boolean;
  ownerTenantId?: string;
};

export function useAdditionalModelValidIds({
  modelTypes,
  models,
  modelsFetched,
  enabled,
  ownerTenantId,
}: AdditionalModelValidIdsOptions) {
  const { t } = useTranslation();
  const {
    canUseGroups,
    isSettled: tenantAccessSettled,
    resolveCanUseGroups,
  } = useModelGroupTenantAccess({ ownerTenantId, enabled });
  const groupsEnabled = enabled && canUseGroups;
  const {
    compatibleGroupIds,
    isFetched: groupsFetched,
    resolveValidIds: resolveGroupValidIds,
  } = useModelGroupOptions(modelTypes, models, groupsEnabled && modelsFetched);

  const validIds = useMemo(() => {
    if (!groupsEnabled) return new Set<string>();
    return new Set(Array.from(compatibleGroupIds, buildModelGroupRef));
  }, [compatibleGroupIds, groupsEnabled]);
  const isSettled =
    modelsFetched &&
    (!enabled || (tenantAccessSettled && (!groupsEnabled || groupsFetched)));

  const getPendingMessage = useCallback(
    (value?: string) => {
      if (!enabled || !isModelGroupRef(value) || isSettled) return undefined;
      return t('setting.modelGroupSelector.pending');
    },
    [enabled, isSettled, t],
  );

  const resolveValidIds = useCallback(
    async (freshModels: IAddedModel[]) => {
      if (!(await resolveCanUseGroups())) return new Set<string>();
      return resolveGroupValidIds(freshModels);
    },
    [resolveCanUseGroups, resolveGroupValidIds],
  );

  return { validIds, isSettled, getPendingMessage, resolveValidIds };
}
