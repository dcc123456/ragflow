import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react';
import { useExtraParserOptions } from '../use-extra-parser-options';

const mockGetDocAnalyzers = jest.fn();
jest.mock('@/ee/services/doc-analyzer-service', () => ({
  __esModule: true,
  default: {
    getDocAnalyzers: (...args: unknown[]) => mockGetDocAnalyzers(...args),
  },
}));

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return {
    queryClient,
    wrapper: ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    ),
  };
}

describe('useExtraParserOptions', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('returns only available EE analyzers as parser options', async () => {
    mockGetDocAnalyzers.mockResolvedValue({
      data: {
        code: 0,
        data: [
          {
            id: 'deepdoc',
            kind: 'builtin',
            available: true,
            label: 'DeepDoc (in-process)',
          },
          {
            id: 'deepdoc_cpu_ee',
            kind: 'ee',
            available: true,
            label: 'DeepDoc EE (CPU)',
          },
          {
            id: 'deepdoc_gpu_ee',
            kind: 'ee',
            available: false,
            label: 'DeepDoc EE (GPU)',
          },
        ],
      },
    });
    const { wrapper } = createWrapper();
    const { result } = renderHook(() => useExtraParserOptions(), { wrapper });
    await waitFor(() => expect(result.current).toHaveLength(1));
    expect(result.current).toEqual([
      { value: 'deepdoc_cpu_ee', label: 'DeepDoc EE (CPU)' },
    ]);
    expect(mockGetDocAnalyzers).toHaveBeenCalledWith(
      { signal: expect.anything(), skipGlobalErrorNotification: true },
      true,
    );
  });

  it('falls back to the analyzer id when no label is given', async () => {
    mockGetDocAnalyzers.mockResolvedValue({
      data: {
        code: 0,
        data: [{ id: 'deepdoc_cpu_ee', kind: 'ee', available: true }],
      },
    });
    const { wrapper } = createWrapper();
    const { result } = renderHook(() => useExtraParserOptions(), { wrapper });
    await waitFor(() => expect(result.current).toHaveLength(1));
    expect(result.current).toEqual([
      { value: 'deepdoc_cpu_ee', label: 'deepdoc_cpu_ee' },
    ]);
  });

  it('returns empty on a non-zero response code', async () => {
    mockGetDocAnalyzers.mockResolvedValue({
      data: { code: 100, message: 'unavailable' },
    });
    const { wrapper } = createWrapper();
    const { result } = renderHook(() => useExtraParserOptions(), { wrapper });
    await waitFor(() => expect(mockGetDocAnalyzers).toHaveBeenCalledTimes(1));
    expect(result.current).toEqual([]);
  });

  it('returns empty when the request fails', async () => {
    mockGetDocAnalyzers.mockRejectedValue(new Error('404'));
    const { wrapper } = createWrapper();
    const { result } = renderHook(() => useExtraParserOptions(), { wrapper });
    await waitFor(() => expect(mockGetDocAnalyzers).toHaveBeenCalledTimes(1));
    expect(result.current).toEqual([]);
  });

  it('keeps a stable identity across re-renders once loaded', async () => {
    mockGetDocAnalyzers.mockResolvedValue({
      data: {
        code: 0,
        data: [
          {
            id: 'deepdoc_cpu_ee',
            kind: 'ee',
            available: true,
            label: 'DeepDoc EE (CPU)',
          },
        ],
      },
    });
    const { wrapper } = createWrapper();
    const { result, rerender } = renderHook(() => useExtraParserOptions(), {
      wrapper,
    });
    await waitFor(() => expect(result.current).toHaveLength(1));
    const first = result.current;
    rerender();
    expect(result.current).toBe(first);
  });
});
