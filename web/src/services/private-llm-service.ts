import api from '@/utils/private-api';
import { registerNextServer } from '@/utils/register-server';

const { setDefaultLlm, isAdmin, enableAdmin } = api;

const methods = {
  setDefaultLlm: {
    url: setDefaultLlm,
    method: 'post',
  },
  isAdmin: {
    url: isAdmin,
    method: 'get',
  },
  enableAdmin: {
    url: enableAdmin,
    method: 'get',
  },
} as const;

const privateLLMService = registerNextServer<keyof typeof methods>(methods);

export default privateLLMService;
