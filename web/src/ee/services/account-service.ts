import apiPrivate from '@/ee/utils/api-private';
import { registerNextServer } from '@/utils/register-server';

export default registerNextServer({
  deleteOwnAccount: { url: apiPrivate.deleteOwnAccount, method: 'delete' },
});
