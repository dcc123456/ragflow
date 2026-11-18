import { render, screen } from '@testing-library/react';
import { useSystemConfig } from '@/hooks/use-system-request';
import { useOperateApiKey } from '../hooks';
import ChatApiKeyModal from './index';

jest.mock('@/hooks/use-system-request', () => ({
  useSystemConfig: jest.fn(),
}));
jest.mock('../hooks', () => ({
  useOperateApiKey: jest.fn(),
}));
jest.mock('@/hooks/common-hooks', () => ({
  useTranslate: () => ({ t: (key: string) => key }),
}));
jest.mock('@/components/copy-to-clipboard', () => ({
  __esModule: true,
  default: () => null,
}));

describe('API key creation limit', () => {
  beforeEach(() => {
    jest.mocked(useOperateApiKey).mockReturnValue({
      createToken: jest.fn(),
      removeToken: jest.fn(),
      tokenList: Array.from({ length: 16 }, (_, index) => ({
        token: String(index),
        create_time: 0,
        create_date: '',
        tenant_id: 'tenant',
        beta: '',
      })),
      listLoading: false,
      creatingLoading: false,
    });
  });

  it('keeps the default cap at 16 when no limit is configured', () => {
    jest.mocked(useSystemConfig).mockReturnValue({
      config: undefined,
      loading: false,
    });
    render(<ChatApiKeyModal idKey="dialog_id" hideModal={jest.fn()} />);
    expect(screen.getByRole('button', { name: 'createNewKey' })).toBeDisabled();
  });

  it('allows creation beyond 16 when the configured limit is higher', () => {
    jest.mocked(useSystemConfig).mockReturnValue({
      config: {
        apiKeyMaxCount: 32,
        disablePasswordLogin: false,
        registerEnabled: true,
        emailVerificationEnabled: false,
        upload_size_limit: 0,
      },
      loading: false,
    });
    const { rerender } = render(
      <ChatApiKeyModal idKey="dialog_id" hideModal={jest.fn()} />,
    );
    expect(screen.getByRole('button', { name: 'createNewKey' })).toBeEnabled();

    jest.mocked(useSystemConfig).mockReturnValue({
      config: {
        apiKeyMaxCount: 8,
        disablePasswordLogin: false,
        registerEnabled: true,
        emailVerificationEnabled: false,
        upload_size_limit: 0,
      },
      loading: false,
    });
    rerender(<ChatApiKeyModal idKey="dialog_id" hideModal={jest.fn()} />);
    expect(screen.getByRole('button', { name: 'createNewKey' })).toBeDisabled();
    expect(screen.getAllByRole('row')).toHaveLength(17);
  });
});
