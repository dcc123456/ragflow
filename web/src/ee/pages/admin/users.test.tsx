import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen } from '@testing-library/react';
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

  it('opens Settings when the signed-in admin changes their own password', async () => {
    jest.mocked(listUsers).mockResolvedValue({
      data: {
        code: 0,
        data: [
          { email: 'admin@example.com', is_active: '1', is_superuser: true },
        ],
        total: 1,
      },
    } as never);

    render(
      <QueryClientProvider client={new QueryClient()}>
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

    fireEvent.click(
      await screen.findByRole('button', { name: 'admin.changePassword' }),
    );
    expect(await screen.findByText('Admin Settings')).toBeInTheDocument();
  });
});
