import api from '@/utils/api';
import { registerNextServer } from '@/utils/register-server';

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
  evaluationClearCaseResult,
  evaluationClearCaseMetricResult,
  evaluationClearCaseAnswer,
  evaluationExportRun,
  downloadEvaluationFile,
  evaluationCancelRun,
} = api;

const methods = {
  downloadEvaluationFile: {
    url: downloadEvaluationFile,
    method: 'get',
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
    method: 'patch',
  },
  deleteCollection: {
    url: evaluationDeleteCollection,
    method: 'delete',
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
    method: 'patch',
  },
  deleteCase: {
    url: evaluationDeleteCase,
    method: 'delete',
  },

  // Run
  startRun: {
    url: evaluationStartRun,
    method: 'post',
  },
  run: {
    url: evaluationRun,
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
    method: 'patch',
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
    method: 'patch',
  },

  // Clear Results
  clearCaseResult: {
    url: evaluationClearCaseResult,
    method: 'delete',
  },
  clearCaseMetricResult: {
    url: evaluationClearCaseMetricResult,
    method: 'delete',
  },
  clearCaseAnswer: {
    url: evaluationClearCaseAnswer,
    method: 'delete',
  },

  // Other
  exportRun: {
    url: evaluationExportRun,
    method: 'get',
  },
} as const;

const requests = registerNextServer<keyof typeof methods>(methods);
const evaluationService = {
  ...requests,
  updateCollection: (id: string, data: Record<string, unknown>) =>
    requests.updateCollection(
      { url: evaluationUpdateCollection(id), data },
      true,
    ),
  addCase: (id: string, data: Record<string, unknown>) =>
    requests.addCase({ url: evaluationAddCase(id), data }, true),
  importCase: (id: string, data: Record<string, unknown>) =>
    requests.importCase({ url: evaluationImportCase(id), data }, true),
  listCase: (id: string, params: Record<string, unknown>) =>
    requests.listCase({ url: evaluationListCase(id), params }, true),
  updateCase: (
    collectionId: string,
    caseId: string,
    data: Record<string, unknown>,
  ) =>
    requests.updateCase(
      { url: evaluationUpdateCase(collectionId, caseId), data },
      true,
    ),
  deleteCase: (collectionId: string, caseId: string) =>
    requests.deleteCase(
      { url: evaluationDeleteCase(collectionId, caseId) },
      true,
    ),
  startRun: (id: string, data: Record<string, unknown>) =>
    requests.startRun({ url: evaluationStartRun(id), data }, true),
  getRunResults: (id: string, params: Record<string, unknown>) =>
    requests.getRunResults({ url: evaluationGetRunResults(id), params }, true),
  updateRun: (id: string, data: Record<string, unknown>) =>
    requests.updateRun({ url: evaluationUpdateRun(id), data }, true),
  evaluationCancelRun: (id: string) =>
    requests.evaluationCancelRun(
      { url: evaluationCancelRun(id), data: { status: 'CANCEL' } },
      true,
    ),
  clearCaseResult: (runId: string, caseId: string) =>
    requests.clearCaseResult(
      { url: evaluationClearCaseResult(runId, caseId) },
      true,
    ),
  clearCaseMetricResult: (runId: string, caseId: string, metric: string) =>
    requests.clearCaseMetricResult(
      { url: evaluationClearCaseMetricResult(runId, caseId, metric) },
      true,
    ),
  clearCaseAnswer: (runId: string, caseId: string) =>
    requests.clearCaseAnswer(
      { url: evaluationClearCaseAnswer(runId, caseId) },
      true,
    ),
  exportRun: (runId: string, caseIds?: string[]) =>
    requests.exportRun(
      {
        url: evaluationExportRun(runId),
        params: caseIds?.length ? { case_ids: caseIds.join(',') } : undefined,
        responseType: 'blob',
      },
      true,
    ),
  downloadEvaluationFile: (id: string) =>
    requests.downloadEvaluationFile(
      { url: downloadEvaluationFile(id), responseType: 'blob' },
      true,
    ),
};
export default evaluationService;

export async function evaluationExportBlob(blob: Blob): Promise<Blob> {
  if (blob.type.includes('json') || blob.type.includes('text/')) {
    const body = JSON.parse(await blob.text());
    throw new Error(body.message || body.retmsg || 'Evaluation export failed');
  }
  return blob;
}
