import api from '@/utils/private-api';
import { registerNextServer } from '@/utils/register-server';

const { heartBeat } = api;

const methods = {
  heartBeat: {
    url: heartBeat,
    method: 'get',
  },
} as const;

const heartBeatService = registerNextServer<keyof typeof methods>(methods);

export default heartBeatService;
