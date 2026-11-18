import { IntegrationsNavGroup } from '@/layouts/components/app-sidebar/nav-item';
import { fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';

const mockUseFetchTenantInfo = jest.fn();
const mockUseTenantMyPermissions = jest.fn();

jest.mock('@/hooks/use-user-setting-request', () => ({
  useFetchTenantInfo: () => mockUseFetchTenantInfo(),
}));

jest.mock('@/ee/pages/user-setting/team/use-member-role-query', () => ({
  useTenantMyPermissions: () => mockUseTenantMyPermissions(),
}));

jest.mock('@/layouts/components/app-sidebar/use-sidebar-collapse', () => ({
  useSidebarCollapse: () => ({ collapsed: false }),
}));

jest.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

function mockModelPermission(enable: boolean) {
  mockUseTenantMyPermissions.mockReturnValue({
    data: { permissions: { model_provider: { enable } } },
  });
}

function renderNavigation(pathname = '/') {
  render(
    <MemoryRouter initialEntries={[pathname]}>
      <IntegrationsNavGroup />
    </MemoryRouter>,
  );
  const integrationsButton = screen.getByRole('button', {
    name: 'sidebar.integrations',
  });
  if (integrationsButton.getAttribute('aria-expanded') !== 'true') {
    fireEvent.click(integrationsButton);
  }
}

describe('integration navigation model permission', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockUseFetchTenantInfo.mockReturnValue({
      data: { tenant_id: 'tenant-1' },
    });
  });

  it('shows Model Group when model provider access is granted', () => {
    mockModelPermission(true);
    renderNavigation();

    expect(screen.getByText('setting.modelGroup')).toBeInTheDocument();
  });

  it('hides only Model Group when model provider access is denied', () => {
    mockModelPermission(false);
    renderNavigation();

    expect(screen.queryByText('setting.modelGroup')).not.toBeInTheDocument();
    expect(screen.getByText('sidebar.models')).toBeInTheDocument();
  });

  it('activates the inserted Model Group on its route', () => {
    mockModelPermission(true);
    renderNavigation('/integrations/model-group');

    expect(
      screen.getByRole('link', { name: 'setting.modelGroup' }),
    ).toHaveAttribute('aria-current', 'page');
  });
});
