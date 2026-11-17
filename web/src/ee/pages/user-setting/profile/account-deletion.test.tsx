import { fireEvent, render, screen } from '@testing-library/react';
import AccountDeletion from './account-deletion';

jest.mock('@/hooks/use-user-setting-request', () => ({
  useFetchUserInfo: () => ({ data: { email: 'actor@example.test' } }),
}));
const mockMutate = jest.fn();
let mockPending = false;
let mockError: Error | null = null;
jest.mock('./use-delete-account', () => ({
  useDeleteAccount: () => ({
    mutate: mockMutate,
    isPending: mockPending,
    error: mockError,
  }),
}));
jest.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

beforeEach(() => {
  mockMutate.mockClear();
  mockPending = false;
  mockError = null;
});

it('requires explicit confirmation and resets it after cancellation', () => {
  render(<AccountDeletion />);
  fireEvent.click(
    screen.getByRole('button', { name: 'setting.deleteAccount' }),
  );
  const confirm = screen
    .getAllByRole('button', { name: 'setting.deleteAccount' })
    .at(-1)!;
  expect(confirm).toBeDisabled();
  fireEvent.change(screen.getByLabelText('setting.deleteAccountConfirmation'), {
    target: { value: 'actor@example.test' },
  });
  expect(confirm).toBeEnabled();
  fireEvent.click(screen.getByRole('button', { name: 'common.cancel' }));
  expect(mockMutate).not.toHaveBeenCalled();
  fireEvent.click(
    screen.getByRole('button', { name: 'setting.deleteAccount' }),
  );
  expect(
    screen.getByLabelText('setting.deleteAccountConfirmation'),
  ).toHaveValue('');
});

it('submits once confirmed and disables controls while pending', () => {
  const { rerender } = render(<AccountDeletion />);
  fireEvent.click(
    screen.getByRole('button', { name: 'setting.deleteAccount' }),
  );
  fireEvent.change(screen.getByLabelText('setting.deleteAccountConfirmation'), {
    target: { value: 'actor@example.test' },
  });
  fireEvent.click(
    screen.getAllByRole('button', { name: 'setting.deleteAccount' }).at(-1)!,
  );
  expect(mockMutate).toHaveBeenCalledTimes(1);
  mockPending = true;
  rerender(<AccountDeletion />);
  expect(
    screen.getAllByRole('button', { name: 'setting.deleteAccount' }).at(-1),
  ).toBeDisabled();
  expect(screen.getByRole('button', { name: 'common.cancel' })).toBeDisabled();
});

it('shows deletion errors while keeping the confirmation dialog open', () => {
  mockError = new Error('Transfer workspace ownership first');
  render(<AccountDeletion />);
  fireEvent.click(
    screen.getByRole('button', { name: 'setting.deleteAccount' }),
  );
  expect(screen.getByRole('alert')).toHaveTextContent(
    'Transfer workspace ownership first',
  );
});
