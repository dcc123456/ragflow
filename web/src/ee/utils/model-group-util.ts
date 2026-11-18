import { IModelGroupDetailResult } from '@/ee/interfaces/database/model-group';
import { IAddedModel } from '@/interfaces/database/llm';

export const ModelGroupRefPrefix = 'gp-';
export const ModelGroupActiveStatus = 'active';
const ModelGroupRefPattern = /^gp-[0-9a-f]{32}$/;
export function isModelGroupRef(value?: string | null): value is string {
  return !!value && ModelGroupRefPattern.test(value);
}
export function buildModelGroupRef(groupId: string) {
  return `${ModelGroupRefPrefix}${groupId}`;
}
export function normalizeModelTypes(modelTypes: string[]) {
  return Array.from(new Set(modelTypes)).sort();
}
function isDirectModelUsable(model?: IAddedModel) {
  return (
    !!model &&
    (model.status == null || model.status === ModelGroupActiveStatus) &&
    (model.instance_status == null ||
      model.instance_status === ModelGroupActiveStatus)
  );
}
export function buildCompatibleModelGroupIds(
  details: IModelGroupDetailResult[],
  allModels: IAddedModel[],
  modelTypes: string[],
  providers?: string[],
) {
  const directModels = new Map(
    allModels.map((model) => [model.model_id, model]),
  );
  const compatibleIds = new Set<string>();
  for (const detail of details) {
    if (detail.isError || detail.group.status !== ModelGroupActiveStatus)
      continue;
    const compatible = detail.members.some((member) => {
      const directModel = directModels.get(member.model_id);
      return (
        member.status === ModelGroupActiveStatus &&
        (member.model_type ?? []).some((type) => modelTypes.includes(type)) &&
        (!providers?.length || providers.includes(member.provider)) &&
        isDirectModelUsable(directModel)
      );
    });
    if (compatible) compatibleIds.add(detail.group.id);
  }
  return compatibleIds;
}
