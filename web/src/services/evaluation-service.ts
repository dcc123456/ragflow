import api from '@/utils/api';
import { registerNextServer } from '@/utils/register-server';
import request from '@/utils/request';

const {
  evaluationCreateCollection,
  evaluationListCollection,
  evaluationGetCollection,
  evaluationUpdateCollection,
  evaluationDeleteCollection,
  evaluationAddCase,
  evaluationImportCase,
  evaluationListCase,
  evaluationDeleteCase,
  evaluationUpdateCase,
  evaluationStartRun,
  evaluationRun,
  evaluationGetRun,
  evaluationGetRunResults,
  evaluationListRun,
  evaluationDeleteRun,
  evaluationUpdateRun,
  evaluationDuplicateRun,
  evaluationExecuteAll,
  evaluationExecuteCase,
  evaluationCalculateCaseMetric,
  evaluationCalculateRunsMetrics,
  evaluationCalculateRunsMetric,
  evaluationCalculateCaseMetrics,
  evaluationClearCaseResult,
  evaluationClearCaseMetricResult,
  evaluationClearCaseAnswer,
  evaluationGetRecommendations,
  evaluationExportRun,
  downloadEvaluationFile,
  listEvaluationDetailFile,
  evaluationCancelRun,
} = api;

const methods = {
  downloadEvaluationFile: {
    url: downloadEvaluationFile,
    method: 'get',
    responseType: 'blob',
  },
  // Collection
  createCollection: {
    url: evaluationCreateCollection,
    method: 'post',
  },
  listCollection: {
    url: evaluationListCollection,
    method: 'get',
  },
  getCollection: {
    url: evaluationGetCollection,
    method: 'get',
  },
  updateCollection: {
    url: evaluationUpdateCollection,
    method: 'put',
  },
  deleteCollection: {
    url: evaluationDeleteCollection,
    method: 'post',
  },

  // Case
  addCase: {
    url: evaluationAddCase,
    method: 'post',
  },
  importCase: {
    url: evaluationImportCase,
    method: 'post',
  },
  listCase: {
    url: evaluationListCase,
    method: 'get',
  },
  updateCase: {
    url: evaluationUpdateCase,
    method: 'post',
  },
  deleteCase: {
    url: evaluationDeleteCase,
    method: 'post',
  },

  // Run
  startRun: {
    url: evaluationStartRun,
    method: 'post',
  },
  run: {
    url: evaluationRun,
    method: 'put',
  },

  getRun: {
    url: evaluationGetRun,
    method: 'get',
  },
  getRunResults: {
    url: evaluationGetRunResults,
    method: 'get',
  },
  listRun: {
    url: evaluationListRun,
    method: 'get',
  },
  updateRun: {
    url: evaluationUpdateRun,
    method: 'put',
  },
  deleteRun: {
    url: evaluationDeleteRun,
    method: 'delete',
  },
  duplicateRun: {
    url: evaluationDuplicateRun,
    method: 'post',
  },
  evaluationCancelRun: {
    url: evaluationCancelRun,
    method: 'post',
  },

  // Execute
  executeAll: {
    url: evaluationExecuteAll,
    method: 'post',
  },
  executeCase: {
    url: evaluationExecuteCase,
    method: 'post',
  },

  // Metrics
  calculateCaseMetric: {
    url: evaluationCalculateCaseMetric,
    method: 'post',
  },
  calculateRunsMetrics: {
    url: evaluationCalculateRunsMetrics,
    method: 'post',
  },
  calculateRunsMetric: {
    url: evaluationCalculateRunsMetric,
    method: 'post',
  },
  calculateCaseMetrics: {
    url: evaluationCalculateCaseMetrics,
    method: 'post',
  },

  // Clear Results
  clearCaseResult: {
    url: evaluationClearCaseResult,
    method: 'post',
  },
  clearCaseMetricResult: {
    url: evaluationClearCaseMetricResult,
    method: 'post',
  },
  clearCaseAnswer: {
    url: evaluationClearCaseAnswer,
    method: 'post',
  },

  // Other
  getRecommendations: {
    url: evaluationGetRecommendations,
    method: 'get',
  },
  exportRun: {
    url: evaluationExportRun,
    method: 'post',
  },
} as const;

const evaluationService = registerNextServer<keyof typeof methods>(methods);
export const getListEvaluationDetailFile = (
  id: string,
  data: { page: number; page_size: number },
) => {
  return request.get(listEvaluationDetailFile(id), {
    params: data,
  });
};
export default evaluationService;
