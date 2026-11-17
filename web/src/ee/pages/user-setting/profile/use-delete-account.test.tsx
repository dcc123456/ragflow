import { act, renderHook } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import accountService from '@/ee/services/account-service';
import storage, { redirectToLogin } from '@/utils/authorization-util';
import { useDeleteAccount } from './use-delete-account';

jest.mock('@/ee/services/account-service', () => ({
  __esModule: true,
  default: { deleteOwnAccount: jest.fn() },
}));
jest.mock('@/utils/authorization-util', () => ({
  __esModule: true,
  default: { removeAll: jest.fn() },
  redirectToLogin: jest.fn(),
}));

function setup() {
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false } },
  });
  const clear = jest.spyOn(queryClient, 'clear');
  function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
  }
  return { ...renderHook(useDeleteAccount, { wrapper: Wrapper }), clear };
}

beforeEach(() => jest.clearAllMocks());

it('clears credentials and cached data only after successful deletion', async () => {
  jest
    .mocked(accountService.deleteOwnAccount)
    .mockResolvedValue({ data: { code: 0 } } as any);
  const { result, clear } = setup();
  await act(async () => {
    await result.current.mutateAsync('actor@example.test');
  });
  expect(accountService.deleteOwnAccount).toHaveBeenCalledWith({
    confirmation_email: 'actor@example.test',
  });
  expect(storage.removeAll).toHaveBeenCalledTimes(1);
  expect(clear).toHaveBeenCalledTimes(1);
  expect(redirectToLogin).toHaveBeenCalledTimes(1);
});

it.each([
  [
    'application failure',
    { data: { code: 100, message: 'Transfer ownership first' } },
  ],
  ['transport failure', null],
])('retains authentication and cache on %s', async (_, response) => {
  if (response) {
    jest
      .mocked(accountService.deleteOwnAccount)
      .mockResolvedValue(response as any);
  } else {
    jest
      .mocked(accountService.deleteOwnAccount)
      .mockRejectedValue(new Error('Network failed'));
  }
  const { result, clear } = setup();
  await act(async () => {
    await expect(
      result.current.mutateAsync('actor@example.test'),
    ).rejects.toThrow();
  });
  expect(storage.removeAll).not.toHaveBeenCalled();
  expect(clear).not.toHaveBeenCalled();
  expect(redirectToLogin).not.toHaveBeenCalled();
});
