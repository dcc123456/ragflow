import api from '@/utils/api';
import { registerNextServer } from '@/utils/register-server';

const { exposeModelProvider } = api;

const methods = {
  getExposeModelProvider: {
    url: exposeModelProvider,
    method: 'get',
  },
} as const;

const exposeModelProviderService =
  registerNextServer<keyof typeof methods>(methods);

export default exposeModelProviderService;
