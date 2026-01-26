import api from '@/utils/api';
import { registerNextServer } from '@/utils/register-server';

const {
  evaluationCreateDataset,
  evaluationListDataset,
  evaluationGetDataset,
  evaluationUpdateDataset,
  evaluationDeleteDataset,
  evaluationAddCase,
  evaluationImportCase,
  evaluationListCase,
  evaluationDeleteCase,
  evaluationUpdateCase,
  evaluationStartRun,
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
} = api;

const methods = {
  // Dataset
  createDataset: {
    url: evaluationCreateDataset,
    method: 'post',
  },
  listDataset: {
    url: evaluationListDataset,
    method: 'get',
  },
  getDataset: {
    url: evaluationGetDataset,
    method: 'get',
  },
  updateDataset: {
    url: evaluationUpdateDataset,
    method: 'post',
  },
  deleteDataset: {
    url: evaluationDeleteDataset,
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
    method: 'post',
  },
  deleteRun: {
    url: evaluationDeleteRun,
    method: 'post',
  },
  duplicateRun: {
    url: evaluationDuplicateRun,
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

export default evaluationService;
