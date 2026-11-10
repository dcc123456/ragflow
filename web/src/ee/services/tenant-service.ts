import api from '@/ee/utils/private-api';
import request from '@/utils/next-request';

export const switchTenant = (tenantId: string) =>
  request.post(api.tenantSwitch(tenantId));
