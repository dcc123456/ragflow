import { IThirdOAIModel } from '@/interfaces/database/llm';

export function getLlmFactoryFromLlmId(llmId: string) {
  return llmId?.split('@')?.at(1)?.split('#').at(0);
}

export function buildLlmId(
  model: Pick<IThirdOAIModel, 'fid' | 'llm_name' | 'tenant_id'>,
) {
  return `${model.llm_name}@${model.fid}#${model.tenant_id}`;
}

// Will not jump to the login page
export function redirectToLogin() {
  window.location.href = location.origin + `/login`;
}

// Will not jump to the specified page
export function redirectToSpecifiedPage(page: string) {
  window.location.href = location.origin + page;
}
