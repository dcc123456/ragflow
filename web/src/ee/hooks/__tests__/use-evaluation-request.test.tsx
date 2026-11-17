import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react';
import request from '@/utils/next-request';
import evaluationService, {
  evaluationExportBlob,
} from '@/ee/services/evaluation-service';
import {
  EvaluationApiAction,
  EvaluationKeys,
  useCancelEvaluationRun,
  useCreateRunEvaluation,
  useExecuteEvaluationCase,
  useCalculateEvaluationCaseMetric,
  useStartEvaluationRun,
  useUpdateEvaluationRun,
  useUploadEvaluationFile,
  useFetchAllEvaluationCollection,
  useFetchEvaluationRunList,
} from '../use-evaluation-request';

const mockSetRunId = jest.fn();

jest.mock('@/utils/next-request', () => ({
  __esModule: true,
  default: jest.fn(),
}));
jest.mock('@/utils/api', () => ({
  __esModule: true,
  default: jest.requireActual('@/ee/utils/api-private').default,
}));
jest.mock('@/components/ui/message', () => ({
  __esModule: true,
  default: { success: jest.fn(), error: jest.fn() },
}));
jest.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));
jest.mock('react-router', () => ({
  useParams: () => ({ id: 'chat-1' }),
}));
jest.mock('@/hooks/common-hooks', () => ({ useSetModalState: jest.fn() }));
jest.mock('@/hooks/logic-hooks', () => ({
  useHandleSearchChange: () => ({
    searchString: '',
    handleInputChange: jest.fn(),
  }),
}));
jest.mock('@/utils/file-util', () => ({ downloadFileFromBlob: jest.fn() }));
jest.mock('@/ee/hooks/use-evaluation-url', () => ({
  useEvaluationUrl: () => ({
    type: 'chat',
    runId: 'run-1',
    setRunId: mockSetRunId,
  }),
}));

const Root = '/api/v1/evaluations';

it.each(['collections', 'runs'] as const)(
  'loads every page of %s within the REST page limit',
  async (resource) => {
    const { wrapper } = setup();
    const first = Array.from({ length: 200 }, (_, id) => ({ id: String(id) }));
    jest
      .mocked(request)
      .mockResolvedValueOnce({
        data: { code: 0, data: { [resource]: first, total: 201 } },
      })
      .mockResolvedValueOnce({
        data: { code: 0, data: { [resource]: [{ id: 'last' }], total: 201 } },
      });
    const { result } = renderHook(
      () =>
        resource === 'collections'
          ? useFetchAllEvaluationCollection()
          : useFetchEvaluationRunList(),
      { wrapper },
    );
    await waitFor(() => expect(result.current.data?.total).toBe(201));
    expect(request).toHaveBeenNthCalledWith(
      1,
      expect.objectContaining({
        params: expect.objectContaining({ page: 1, page_size: 200 }),
      }),
    );
    expect(request).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({
        params: expect.objectContaining({ page: 2, page_size: 200 }),
      }),
    );
  },
);

function setup() {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  const wrapper = ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return { client, wrapper };
}

beforeEach(() => {
  jest.clearAllMocks();
  jest.mocked(request).mockResolvedValue({
    data: { code: 0, data: { id: 'saved-run' } },
  });
});

it('saves the snapshot as a run resource without starting execution', async () => {
  const { wrapper } = setup();
  const { result } = renderHook(() => useCreateRunEvaluation(), { wrapper });
  const config = {
    target: { name: 'Snapshot name', llm_id: 'judge-1' },
    metrics: { faithfulness: { enable: true, llm_id: 'judge-1' } },
  };
  await act(async () => {
    await result.current.createRunEvaluation({
      collection_id: 'collection-1',
      config_snapshot: config,
    });
  });
  expect(request).toHaveBeenCalledTimes(1);
  expect(request).toHaveBeenCalledWith({
    url: `${Root}/runs`,
    method: 'post',
    data: {
      target_id: 'chat-1',
      target_type: 'chat',
      collection_id: 'collection-1',
      name: 'Snapshot name',
      config_snapshot: config,
    },
  });
  expect(mockSetRunId).toHaveBeenCalledWith('saved-run');
});

it('updates an existing saved run without creating or executing another run', async () => {
  const { wrapper } = setup();
  const { result } = renderHook(() => useUpdateEvaluationRun(), { wrapper });
  await act(async () => {
    await result.current.updateEvaluationRun({
      runId: 'run-1',
      name: 'Renamed',
      collection_id: 'collection-2',
    });
  });
  expect(request).toHaveBeenCalledTimes(1);
  expect(request).toHaveBeenCalledWith({
    url: `${Root}/runs/run-1`,
    method: 'patch',
    data: {
      name: 'Renamed',
      collection_id: 'collection-2',
    },
  });
});

it('starts exactly the selected cases and metrics using the execution resource', async () => {
  const { wrapper } = setup();
  const { result } = renderHook(() => useStartEvaluationRun(), { wrapper });
  await act(async () => {
    await result.current.startEvaluationRun({
      case_ids: ['case-2'],
      metrics: ['faithfulness'],
    });
  });
  expect(request).toHaveBeenCalledWith({
    url: `${Root}/runs/run-1/executions`,
    method: 'post',
    data: {
      mode: 'generate_and_score',
      case_ids: ['case-2'],
      metrics: ['faithfulness'],
    },
  });
});

it('uses generation-only and scoring-only execution without regenerating scores', async () => {
  const { wrapper } = setup();
  const { result } = renderHook(
    () => ({
      generate: useExecuteEvaluationCase(),
      score: useCalculateEvaluationCaseMetric(),
    }),
    { wrapper },
  );
  await act(async () => {
    await result.current.generate.executeEvaluationCase({
      runId: 'run-1',
      caseId: 'case-2',
    });
    await result.current.score.calculateEvaluationCaseMetric({
      runId: 'run-1',
      caseId: 'case-2',
      metric: 'context_relevance',
    });
  });
  expect(request).toHaveBeenNthCalledWith(1, {
    url: `${Root}/runs/run-1/executions`,
    method: 'post',
    data: { mode: 'generate', case_ids: ['case-2'] },
  });
  expect(request).toHaveBeenNthCalledWith(2, {
    url: `${Root}/runs/run-1/executions`,
    method: 'post',
    data: {
      mode: 'score',
      case_ids: ['case-2'],
      metrics: ['context_relevance'],
    },
  });
});

it('cancels by patching the run and invalidates the result polling query', async () => {
  const { wrapper, client } = setup();
  const invalidate = jest.spyOn(client, 'invalidateQueries');
  const { result } = renderHook(() => useCancelEvaluationRun(), { wrapper });
  await act(async () => {
    await result.current.cancelEvaluationRun();
  });
  expect(request).toHaveBeenCalledWith({
    url: `${Root}/runs/run-1`,
    method: 'patch',
    data: { status: 'CANCEL' },
  });
  expect(invalidate).toHaveBeenCalledWith({
    queryKey: EvaluationKeys.action(EvaluationApiAction.GetRunResults, 'run-1'),
  });
});

it('uploads every selected spreadsheet as its own collection', async () => {
  const { wrapper } = setup();
  const { result } = renderHook(() => useUploadEvaluationFile(), { wrapper });
  const first = new File(['question\nhello'], 'one.csv');
  const second = new File(['question\nworld'], 'two.csv');
  await act(async () => {
    expect(
      await result.current.uploadFile({
        fileList: [first, { file: second, path: 'folder/two.csv' }],
      }),
    ).toBe(0);
  });
  expect(request).toHaveBeenCalledTimes(2);
  const calls = jest.mocked(request).mock.calls as unknown as [
    { data: FormData; url: string },
  ][];
  expect(calls[0][0].data.get('file')).toBe(first);
  expect(calls[1][0].data.get('file')).toBe(second);
  expect(calls[0][0].url).toBe(`${Root}/collections`);
});

it('does not treat an application upload error as a successful import', async () => {
  jest.mocked(request).mockResolvedValueOnce({
    data: { code: 102, message: 'Invalid question column' },
  });
  const { wrapper } = setup();
  const { result } = renderHook(() => useUploadEvaluationFile(), { wrapper });
  await act(async () => {
    expect(
      await result.current.uploadFile({
        fileList: [new File(['bad'], 'invalid.csv')],
      }),
    ).toBe(102);
  });
});

it('uses nested REST case resources, JSON imports, and precise result clears', async () => {
  await evaluationService.updateCase('collection-1', 'case-2', {
    variable: { question: 'Updated' },
  });
  await evaluationService.deleteCase('collection-1', 'case-2');
  await evaluationService.importCase('collection-1', {
    cases: [{ question: 'New question' }],
  });
  await evaluationService.clearCaseMetricResult(
    'run-1',
    'case-2',
    'faithfulness',
  );
  expect(request).toHaveBeenNthCalledWith(1, {
    url: `${Root}/collections/collection-1/cases/case-2`,
    method: 'patch',
    data: { variable: { question: 'Updated' } },
  });
  expect(request).toHaveBeenNthCalledWith(2, {
    url: `${Root}/collections/collection-1/cases/case-2`,
    method: 'delete',
  });
  expect(request).toHaveBeenNthCalledWith(3, {
    url: `${Root}/collections/collection-1/imports`,
    method: 'post',
    data: { cases: [{ question: 'New question' }] },
  });
  expect(request).toHaveBeenNthCalledWith(4, {
    url: `${Root}/runs/run-1/results/case-2/metrics/faithfulness`,
    method: 'delete',
  });
});

it('exports selected cases with GET and rejects JSON error blobs', async () => {
  await evaluationService.exportRun('run-1', ['case-2', 'case-3']);
  expect(request).toHaveBeenCalledWith({
    url: `${Root}/runs/run-1/export`,
    method: 'get',
    params: { case_ids: 'case-2,case-3' },
    responseType: 'blob',
  });
  const errorBlob = {
    type: 'application/json',
    text: async () => JSON.stringify({ code: 403, message: 'Access denied' }),
  } as Blob;
  await expect(evaluationExportBlob(errorBlob)).rejects.toThrow(
    'Access denied',
  );
  const spreadsheet = new Blob(['sheet'], {
    type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
  });
  await expect(evaluationExportBlob(spreadsheet)).resolves.toBe(spreadsheet);
});

it('removes chat identity and form-only switches from saved snapshots', async () => {
  const { wrapper } = setup();
  const { result } = renderHook(() => useCreateRunEvaluation(), { wrapper });
  await act(async () => {
    await result.current.createRunEvaluation({
      collection_id: 'collection-1',
      config_snapshot: {
        target: {
          name: 'Snapshot',
          id: 'chat',
          tenant_id: 'tenant',
          temperatureEnabled: true,
          llm_id: 'judge',
          dataset_ids: ['saved-dataset'],
          keywords_similarity_weight: 0.75,
        },
        metrics: {},
      },
    } as any);
  });
  expect(request).toHaveBeenCalledWith(
    expect.objectContaining({
      data: expect.objectContaining({
        config_snapshot: expect.objectContaining({
          target: {
            name: 'Snapshot',
            llm_id: 'judge',
            kb_ids: ['saved-dataset'],
            vector_similarity_weight: 0.25,
          },
        }),
      }),
    }),
  );
});
