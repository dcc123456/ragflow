import SSOProviderDingtalk from './dingtalk';
import SSOProviderFeishu from './feishu';
import SSOProviderGithub from './github';
import SSOProviderGoogle from './google';
import SSOProviderWecom from './wecom';

export const SSO_CLOUD_IDP_PROVIDERS = [
  SSOProviderGoogle,
  SSOProviderGithub,
  SSOProviderFeishu,
  SSOProviderWecom,
  SSOProviderDingtalk,
] as const;

export {
  SSOProviderDingtalk,
  SSOProviderFeishu,
  SSOProviderGithub,
  SSOProviderGoogle,
  SSOProviderWecom,
};
