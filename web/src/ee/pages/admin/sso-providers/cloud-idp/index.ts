import SSOProviderFeishu from './feishu';
import SSOProviderGithub from './github';
import SSOProviderGoogle from './google';
import SSOProviderWecom from './wecom';

export const SSO_CLOUD_IDP_PROVIDERS = [
  SSOProviderGoogle,
  SSOProviderGithub,
  SSOProviderFeishu,
  SSOProviderWecom,
] as const;

export {
  SSOProviderFeishu,
  SSOProviderGithub,
  SSOProviderGoogle,
  SSOProviderWecom,
};
