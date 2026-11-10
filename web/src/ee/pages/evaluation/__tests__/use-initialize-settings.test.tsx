import { renderHook } from '@testing-library/react';
import { useInitializeSettingsOnMount } from '../use-initialize-settings';

const mockChat = {
  name: 'Live chat',
  icon: '',
  dataset_ids: ['live-dataset'],
  keywords_similarity_weight: 0.7,
  llm_setting: {},
};
const mockRun = {
  collection_id: 'collection',
  config_snapshot: {
    target: {
      name: 'Saved snapshot',
      kb_ids: ['saved-dataset'],
      vector_similarity_weight: 0.25,
      llm_setting: {},
    },
    metrics: { faithfulness: { enable: true, llm_id: 'saved-judge' } },
  },
};
jest.mock('@/hooks/use-chat-request', () => ({
  useFetchChat: () => ({ data: mockChat }),
}));
jest.mock('@/ee/hooks/use-evaluation-request', () => ({
  useFetchEvaluationRun: () => ({ data: mockRun }),
}));
jest.mock('@/ee/hooks/use-evaluation-url', () => ({
  useEvaluationUrl: () => ({ runId: 'saved-run' }),
}));
jest.mock('@/hooks/use-llm-request', () => ({
  useFetchDefaultModelDictionary: () => ({ llm_id: 'default-judge' }),
}));

it('hydrates shared chat form fields from the saved evaluation instead of live settings', () => {
  const reset = jest.fn();
  const form = { reset, getValues: jest.fn() } as any;
  renderHook(() => useInitializeSettingsOnMount(form));
  expect(reset).toHaveBeenCalledTimes(1);
  expect(reset.mock.calls[0][0]).toEqual(
    expect.objectContaining({
      collection_id: 'collection',
      config_snapshot: expect.objectContaining({
        target: expect.objectContaining({
          name: 'Saved snapshot',
          icon: '',
          dataset_ids: ['saved-dataset'],
          keywords_similarity_weight: 0.75,
        }),
        metrics: mockRun.config_snapshot.metrics,
      }),
    }),
  );
});
