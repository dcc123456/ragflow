import apiPrivate from '@/ee/utils/api-private';
import { registerNextServer } from '@/utils/register-server';

const methods = {
  joinCanvasPresence: {
    url: apiPrivate.joinCanvasPresence,
    method: 'post',
  },
  heartbeatCanvasPresence: {
    url: apiPrivate.heartbeatCanvasPresence,
    method: 'post',
  },
  leaveCanvasPresence: {
    url: apiPrivate.leaveCanvasPresence,
    method: 'post',
  },
  fetchCanvasPresence: {
    url: apiPrivate.fetchCanvasPresence,
    method: 'get',
  },
} as const;

const canvasPresenceService = registerNextServer<keyof typeof methods>(methods);

export default canvasPresenceService;
