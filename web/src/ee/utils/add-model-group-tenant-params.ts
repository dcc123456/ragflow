import { isModelGroupRef } from './model-group-util';

const ModelGroupParamMap: Record<string, string> = {
  llm_id: 'tenant_llm_id',
  embd_id: 'tenant_embd_id',
  asr_id: 'tenant_asr_id',
  tts_id: 'tenant_tts_id',
  img2txt_id: 'tenant_img2txt_id',
  rerank_id: 'tenant_rerank_id',
  decision_id: 'tenant_decision_id',
};

export function addModelGroupTenantParams(data: any): any {
  if (!data || typeof data !== 'object') return data;
  if (Array.isArray(data)) return data.map(addModelGroupTenantParams);

  const prototype = Object.getPrototypeOf(data);
  if (prototype !== Object.prototype && prototype !== null) return data;

  const newData = { ...data };
  for (const [modelField, tenantField] of Object.entries(ModelGroupParamMap)) {
    if (!Object.hasOwn(newData, modelField)) continue;
    const value = newData[modelField];
    if (isModelGroupRef(value)) {
      newData[tenantField] = value;
    } else if (isModelGroupRef(newData[tenantField])) {
      newData[tenantField] = null;
    }
  }

  for (const [key, value] of Object.entries(newData)) {
    if (value && typeof value === 'object' && !ModelGroupParamMap[key]) {
      newData[key] = addModelGroupTenantParams(value);
    }
  }
  return newData;
}
