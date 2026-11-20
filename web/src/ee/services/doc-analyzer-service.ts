import apiPrivate from '@/ee/utils/api-private';
import { registerNextServer } from '@/utils/register-server';

const methods = {
  getDocAnalyzers: {
    url: apiPrivate.getDocAnalyzers,
    method: 'get',
  },
} as const;

const docAnalyzerService = registerNextServer<keyof typeof methods>(methods);

export default docAnalyzerService;
