import { changeOwnPassword } from '@/ee/services/admin-service';
import * as utils from '@/utils';
import { adminStorage } from '@/utils/authorization-util';
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { ChangePasswordDialog } from './change-password-dialog';

jest.mock('@/ee/services/admin-service', () => ({
  changeOwnPassword: jest.fn(),
}));
jest.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));
jest.mock('@/utils', () => ({
  ...jest.requireActual('@/utils'),
  rsaPsw: jest.fn(jest.requireActual('@/utils').rsaPsw),
}));

const changePassword = jest.mocked(changeOwnPassword);

function renderPasswordSettings() {
  render(
    <MemoryRouter initialEntries={['/admin/settings']}>
      <Routes>
        <Route path="/admin/settings" element={<ChangePasswordDialog />} />
        <Route path="/admin" element={<p>Admin sign-in</p>} />
      </Routes>
    </MemoryRouter>,
  );
}

function openPasswordDialog(confirmPassword = 'NewPassword123') {
  renderPasswordSettings();
  fireEvent.click(screen.getByRole('button', { name: 'setting.edit' }));
  const dialog = within(screen.getByRole('dialog'));
  fireEvent.change(dialog.getByLabelText(/setting.currentPassword/), {
    target: { value: 'OldPassword123' },
  });
  fireEvent.change(dialog.getByLabelText(/setting.newPassword/), {
    target: { value: 'NewPassword123' },
  });
  fireEvent.change(dialog.getByLabelText(/setting.confirmPassword/), {
    target: { value: confirmPassword },
  });
  fireEvent.click(dialog.getByRole('button', { name: 'common.save' }));
}

beforeEach(() => {
  changePassword.mockReset();
  jest.mocked(utils.rsaPsw).mockClear();
  adminStorage.saveLoginCredentials({
    authorization: 'Bearer admin-session',
    token: 'token',
    userInfo: '{}',
  });
});

afterEach(() => adminStorage.removeAll());

it('uses the account password row and edit modal interface', () => {
  renderPasswordSettings();
  expect(screen.getByText('setting.password')).toBeInTheDocument();
  expect(screen.getByText('********')).toBeInTheDocument();
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();

  fireEvent.click(screen.getByRole('button', { name: 'setting.edit' }));
  const dialog = screen.getByRole('dialog');
  expect(dialog).toHaveClass('!w-[480px]');
  expect(
    within(dialog).getByRole('heading', { name: 'setting.editPassword' }),
  ).toBeInTheDocument();
  expect(
    within(dialog).getByLabelText(/setting.currentPassword/),
  ).toHaveAttribute('autocomplete', 'current-password');
  expect(within(dialog).getByLabelText(/setting.newPassword/)).toHaveAttribute(
    'autocomplete',
    'new-password',
  );
  expect(
    within(dialog).getByLabelText(/setting.confirmPassword/),
  ).toHaveAttribute('autocomplete', 'new-password');
  expect(
    within(dialog).getByRole('button', { name: 'setting.cancel' }),
  ).toBeInTheDocument();
  expect(
    within(dialog).getByRole('button', { name: 'common.save' }),
  ).toBeInTheDocument();
});

it('does not submit mismatched passwords', async () => {
  openPasswordDialog('DifferentPassword123');
  expect(
    await screen.findByText('setting.confirmPasswordNonMatchMessage'),
  ).toBeInTheDocument();
  expect(changePassword).not.toHaveBeenCalled();
});

it('keeps the dialog and session on rejection', async () => {
  changePassword.mockResolvedValueOnce({
    code: 400,
    message: 'Current password is incorrect',
  });
  openPasswordDialog();
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'Current password is incorrect',
  );
  expect(screen.getByRole('dialog')).toBeInTheDocument();
  expect(adminStorage.getAuthorization()).toBe('Bearer admin-session');
});

it('clears the admin session and returns to sign-in after success', async () => {
  changePassword.mockResolvedValueOnce({
    code: 0,
    message: 'success',
  });
  openPasswordDialog();
  await waitFor(() => expect(changePassword).toHaveBeenCalledTimes(1));
  const [currentPassword, newPassword] = changePassword.mock.calls[0];
  expect(currentPassword).toMatch(/^[A-Za-z0-9+/]{342}==$/);
  expect(newPassword).toMatch(/^[A-Za-z0-9+/]{342}==$/);
  expect(await screen.findByText('Admin sign-in')).toBeInTheDocument();
  expect(adminStorage.getAuthorization()).toBe('');
  expect(adminStorage.getUserInfoObject()).toBeNull();
});

it('prevents closing the modal while the password change is pending', async () => {
  let resolveChange!: (result: { code: number; message: string }) => void;
  changePassword.mockReturnValueOnce(
    new Promise((resolve) => {
      resolveChange = resolve;
    }),
  );
  openPasswordDialog();
  await waitFor(() => expect(changePassword).toHaveBeenCalledTimes(1));
  const dialog = within(screen.getByRole('dialog'));
  expect(dialog.getByRole('button', { name: 'setting.cancel' })).toBeDisabled();
  expect(dialog.getByRole('button', { name: 'common.save' })).toBeDisabled();
  expect(
    screen.getByRole('dialog').querySelector('.lucide-x'),
  ).not.toBeInTheDocument();
  fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
  expect(screen.getByRole('dialog')).toBeInTheDocument();
  resolveChange({ code: 0, message: 'success' });
  expect(await screen.findByText('Admin sign-in')).toBeInTheDocument();
});

it('preserves spaces in password values', async () => {
  changePassword.mockResolvedValueOnce({ code: 0, message: 'success' });
  renderPasswordSettings();
  fireEvent.click(screen.getByRole('button', { name: 'setting.edit' }));
  const dialog = within(screen.getByRole('dialog'));
  fireEvent.change(dialog.getByLabelText(/setting.currentPassword/), {
    target: { value: ' old password ' },
  });
  for (const label of ['setting.newPassword', 'setting.confirmPassword']) {
    fireEvent.change(dialog.getByLabelText(new RegExp(label)), {
      target: { value: ' new password ' },
    });
  }
  fireEvent.click(dialog.getByRole('button', { name: 'common.save' }));
  expect(await screen.findByText('Admin sign-in')).toBeInTheDocument();
  expect(utils.rsaPsw).toHaveBeenCalledWith(' old password ');
  expect(utils.rsaPsw).toHaveBeenCalledWith(' new password ');
});
