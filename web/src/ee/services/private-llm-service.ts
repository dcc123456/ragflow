import api from '@/ee/utils/private-api';
import { registerNextServer } from '@/utils/register-server';

const { setDefaultLlm } = api;

const methods = {
  setDefaultLlm: {
    url: setDefaultLlm,
    method: 'post',
  },
} as const;

const privateLLMService = registerNextServer<keyof typeof methods>(methods);

export default privateLLMService;
