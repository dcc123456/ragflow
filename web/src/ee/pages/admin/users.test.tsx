import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';

import AdminUserManagement from './users';
import { CurrentUserInfoContext } from '@/pages/admin/layouts/root-layout';
import { listRoles, listUsers } from '@/ee/services/admin-service';

jest.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

jest.mock('@/ee/services/billingStatus', () => ({
  isBillingEnabled: () => false,
}));

jest.mock('@/ee/pages/admin/utils', () => ({
  ...jest.requireActual('@/ee/pages/admin/utils'),
  IS_ENTERPRISE: true,
  USE_LDAP: false,
}));

jest.mock('@/ee/services/admin-service', () => ({
  createUser: jest.fn(),
  deleteUser: jest.fn(),
  grantSuperuser: jest.fn(),
  listRoles: jest.fn(),
  listUsers: jest.fn(),
  revokeSuperuser: jest.fn(),
  updateUserPassword: jest.fn(),
  updateUserStatus: jest.fn(),
}));

describe('AdminUserManagement', () => {
  it('does not show the role column', async () => {
    jest.mocked(listRoles).mockResolvedValue({
      data: { data: [] },
    } as never);
    jest.mocked(listUsers).mockResolvedValue({
      data: {
        code: 0,
        data: [
          {
            create_date: 0,
            email: 'user@example.com',
            is_active: '1',
            is_superuser: false,
            nickname: 'User',
          },
        ],
        total: 1,
      },
    } as never);

    render(
      <QueryClientProvider client={new QueryClient()}>
        <MemoryRouter>
          <AdminUserManagement />
        </MemoryRouter>
      </QueryClientProvider>,
    );

    expect(await screen.findByText('user@example.com')).toBeInTheDocument();
    expect(screen.queryByText('admin.role')).not.toBeInTheDocument();
  });

  it('opens the password dialog directly for the signed-in admin', async () => {
    jest.mocked(listUsers).mockResolvedValue({
      data: {
        code: 0,
        data: [
          { email: 'admin@example.com', is_active: '1', is_superuser: true },
          {
            email: 'other-admin@example.com',
            is_active: '1',
            is_superuser: true,
          },
          { email: 'user@example.com', is_active: '1', is_superuser: false },
        ],
        total: 1,
      },
    } as never);

    const queryClient = new QueryClient();
    render(
      <QueryClientProvider client={queryClient}>
        <CurrentUserInfoContext.Provider
          value={[
            {
              source: 'localStorage',
              userInfo: {
                email: 'admin@example.com',
                name: 'Admin',
                avatar: null,
              },
            },
            jest.fn(),
          ]}
        >
          <MemoryRouter initialEntries={['/admin/users']}>
            <Routes>
              <Route path="/admin/users" element={<AdminUserManagement />} />
              <Route path="/admin/settings" element={<p>Admin Settings</p>} />
            </Routes>
          </MemoryRouter>
        </CurrentUserInfoContext.Provider>
      </QueryClientProvider>,
    );

    const ownRow = (await screen.findByText('admin@example.com')).closest(
      'tr',
    )!;
    const otherAdminRow = screen
      .getByText('other-admin@example.com')
      .closest('tr')!;
    const userRow = screen.getByText('user@example.com').closest('tr')!;
    expect(
      within(otherAdminRow).queryByRole('button', {
        name: 'admin.changePassword',
      }),
    ).not.toBeInTheDocument();
    expect(
      within(userRow).getByRole('button', { name: 'admin.changePassword' }),
    ).toBeInTheDocument();
    fireEvent.click(
      within(userRow).getByRole('button', { name: 'admin.changePassword' }),
    );
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(
      screen.queryByLabelText(/setting.currentPassword/),
    ).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'admin.cancel' }));
    fireEvent.click(
      within(ownRow).getByRole('button', { name: 'admin.changePassword' }),
    );
    expect(await screen.findByRole('dialog')).toBeInTheDocument();
    expect(
      screen.getByLabelText(/setting.currentPassword/),
    ).toBeInTheDocument();
    expect(screen.queryByText('Admin Settings')).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText(/setting.currentPassword/), {
      target: { value: 'typed password' },
    });
    await act(async () => {
      await queryClient.invalidateQueries();
    });
    expect(screen.getByLabelText(/setting.currentPassword/)).toHaveValue(
      'typed password',
    );
  });
});
