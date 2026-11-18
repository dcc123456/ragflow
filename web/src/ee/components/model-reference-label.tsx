import LLMLabel from '@/components/llm-select/llm-label';
import { useModelGroupTenantAccess } from '@/ee/hooks/use-model-group-tenant-access';
import { useModelGroupList } from '@/ee/hooks/use-model-group-request';
import {
  buildModelGroupRef,
  isModelGroupRef,
} from '@/ee/utils/model-group-util';

type ModelReferenceLabelProps = {
  value?: string;
  ownerTenantId?: string;
};

export function ModelReferenceLabel({
  value,
  ownerTenantId,
}: ModelReferenceLabelProps) {
  const { canUseGroups } = useModelGroupTenantAccess({ ownerTenantId });
  const groupValue = canUseGroups && isModelGroupRef(value) ? value : undefined;
  const { data: groups } = useModelGroupList(!!groupValue);

  if (!groupValue) {
    return <LLMLabel value={value} ownerTenantId={ownerTenantId} />;
  }

  const label =
    groups.find((group) => buildModelGroupRef(group.id) === groupValue)
      ?.group_name ?? groupValue;
  return (
    <span className="block min-w-0 truncate font-medium" title={label}>
      {label}
    </span>
  );
}
