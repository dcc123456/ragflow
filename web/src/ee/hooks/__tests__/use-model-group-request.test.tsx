import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook } from '@testing-library/react';
import {
  ModelGroupKeys,
  useAddModelGroupMember,
} from '../use-model-group-request';

const mockAddMember = jest.fn();
jest.mock('@/ee/services/model-group-service', () => ({
  __esModule: true,
  default: { addMember: (...args: unknown[]) => mockAddMember(...args) },
}));
describe('ModelGroupKeys', () => {
  it('uses normalized types and stable detail keys', () => {
    expect(ModelGroupKeys.options(['vision', 'chat', 'chat'])).toEqual(
      ModelGroupKeys.options(['chat', 'vision']),
    );
    expect(ModelGroupKeys.detail('a/b')).toEqual([
      'modelGroup',
      'detail',
      'a/b',
    ]);
  });
});
describe('useAddModelGroupMember', () => {
  it('invalidates only the current detail plus option caches after add', async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const invalidateSpy = jest.spyOn(queryClient, 'invalidateQueries');
    mockAddMember.mockResolvedValue({ data: { code: 0 } });
    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
    const { result } = renderHook(() => useAddModelGroupMember('a/b'), {
      wrapper,
    });
    await result.current.run({ model_id: 'model-1' });
    expect(mockAddMember).toHaveBeenCalledWith('a/b', { model_id: 'model-1' });
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: ModelGroupKeys.detail('a/b'),
    });
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: ModelGroupKeys.optionsRoot(),
    });
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: ModelGroupKeys.validIdsRoot(),
    });
    expect(invalidateSpy).not.toHaveBeenCalledWith({
      queryKey: ModelGroupKeys.detail('other'),
    });
    queryClient.clear();
  });
});
