import {
  RootLayoutContent,
  useRootLayoutChrome,
} from '@/ee/layouts/root-layout-shell';
import { isBillingEnabled } from '@/ee/services/billingStatus';
import request from '@/utils/next-request';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, render, screen } from '@testing-library/react';

jest.mock('@/utils/next-request', () => ({
  __esModule: true,
  default: jest.fn(),
}));

jest.mock('@/ee/services/billingStatus', () => ({
  isBillingEnabled: jest.fn(),
}));

jest.mock('@/ee/layouts/components/notification-banner', () => ({
  NotificationBanner: () => null,
}));

jest.mock('@/ee/pages/price/global', () => ({
  UpgradeModalProvider: ({ children }: { children?: any }) => children,
}));

jest.mock('react-router', () => ({
  useLocation: () => ({ pathname: '/home' }),
}));

function Layout() {
  useRootLayoutChrome();
  return <RootLayoutContent>Page content</RootLayoutContent>;
}

describe('root layout heartbeat', () => {
  let queryClient = new QueryClient();

  beforeEach(() => {
    jest.useFakeTimers();
    jest.clearAllMocks();
    queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false, refetchOnWindowFocus: false },
      },
    });
    jest.mocked(request).mockResolvedValue({ data: { code: 0, data: null } });
  });

  afterEach(() => {
    queryClient.clear();
    jest.useRealTimers();
  });

  it.each([true, false])(
    'sends immediately and every two minutes with billing_enabled=%s',
    async (billingEnabled) => {
      jest.mocked(isBillingEnabled).mockReturnValue(billingEnabled);
      const { unmount } = render(
        <QueryClientProvider client={queryClient}>
          <Layout />
        </QueryClientProvider>,
      );

      await act(async () => {
        await jest.advanceTimersByTimeAsync(0);
      });

      expect(screen.getByText('Page content')).toBeInTheDocument();
      expect(request).toHaveBeenCalledTimes(1);
      expect(request).toHaveBeenLastCalledWith({
        url: '/api/v1/heartbeat',
        method: 'get',
        signal: expect.any(AbortSignal),
      });

      await act(async () => {
        await jest.advanceTimersByTimeAsync(119_999);
      });
      expect(request).toHaveBeenCalledTimes(1);

      await act(async () => {
        await jest.advanceTimersByTimeAsync(1);
      });
      expect(request).toHaveBeenCalledTimes(2);

      unmount();
      await act(async () => {
        await jest.advanceTimersByTimeAsync(240_000);
      });
      expect(request).toHaveBeenCalledTimes(2);
    },
  );

  it('continues polling after a failed request', async () => {
    jest.mocked(isBillingEnabled).mockReturnValue(false);
    jest
      .mocked(request)
      .mockRejectedValueOnce(new Error('Network unavailable'));
    const { unmount } = render(
      <QueryClientProvider client={queryClient}>
        <Layout />
      </QueryClientProvider>,
    );

    await act(async () => {
      await jest.advanceTimersByTimeAsync(0);
    });
    expect(request).toHaveBeenCalledTimes(1);

    await act(async () => {
      await jest.advanceTimersByTimeAsync(120_000);
    });
    expect(request).toHaveBeenCalledTimes(2);
    unmount();
  });

  it('cancels an in-flight heartbeat when the layout unmounts', () => {
    jest.mocked(isBillingEnabled).mockReturnValue(false);
    jest.mocked(request).mockReturnValue(new Promise(() => {}));
    const { unmount } = render(
      <QueryClientProvider client={queryClient}>
        <Layout />
      </QueryClientProvider>,
    );

    const { signal } = jest.mocked(request).mock.calls[0][0] as unknown as {
      signal: AbortSignal;
    };
    expect(signal?.aborted).toBe(false);
    unmount();
    expect(signal?.aborted).toBe(true);
  });
});
