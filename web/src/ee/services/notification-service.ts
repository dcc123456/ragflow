import api from '@/utils/api';
import { registerNextServer } from '@/utils/register-server';

const { notification } = api;

const methods = {
  getNotification: {
    url: notification,
    method: 'get',
  },
} as const;

const notificationService = registerNextServer<keyof typeof methods>(methods);

export default notificationService;
