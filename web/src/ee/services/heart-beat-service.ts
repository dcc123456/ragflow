import apiPrivate from '@/ee/utils/api-private';
import { registerNextServer } from '@/utils/register-server';

const methods = {
  heartBeat: {
    url: apiPrivate.heartBeat,
    method: 'get',
  },
} as const;

const heartBeatService = registerNextServer<keyof typeof methods>(methods);

export default heartBeatService;
