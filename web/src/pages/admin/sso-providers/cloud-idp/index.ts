import SSOProviderFeishu from './feishu';
import SSOProviderGithub from './github';
import SSOProviderGoogle from './google';

export const SSO_CLOUD_IDP_PROVIDERS = [
  SSOProviderGoogle,
  SSOProviderGithub,
  SSOProviderFeishu,
] as const;

export { SSOProviderFeishu, SSOProviderGithub, SSOProviderGoogle };
