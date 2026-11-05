import api from '@/utils/api';
import { registerNextServer } from '@/utils/register-server';

const { exposeApi } = api;

const methods = {
  getExposeApi: {
    url: exposeApi,
    method: 'get',
  },
} as const;

const exposeApiService = registerNextServer<keyof typeof methods>(methods);

export default exposeApiService;
