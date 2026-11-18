import { renderHook } from '@testing-library/react';
import { useSettingMenuItems } from './use-setting-menu';

const exposure = { model: true, api: true };
let mockBillingEnabled = true;

jest.mock('@/ee/hooks/use-private-llm-request', () => ({
  useFetchExposeModelProvider: () => ({ data: exposure.model }),
  useFetchExposeApi: () => ({ data: exposure.api }),
}));
jest.mock('@/ee/pages/price/hook/use-price-hooks', () => ({
  useFetchCurrentPlan: () => ({ data: undefined }),
}));
jest.mock('@/ee/services/billingStatus', () => ({
  isBillingEnabled: () => mockBillingEnabled,
}));
jest.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

const items = [
  { icon: null, label: 'profile', key: '/profile' },
  { icon: null, label: 'api', key: '/api' },
];

describe('useSettingMenuItems', () => {
  beforeEach(() => {
    exposure.model = true;
    mockBillingEnabled = true;
  });

  it('keeps Model Provider in settings without duplicating Model Group', () => {
    const { result } = renderHook(() => useSettingMenuItems(items));
    const keys = result.current.map((item) => item.key);

    expect(keys).toEqual(['/profile', '/api', '/billing', '/model']);
  });

  it('hides Model Provider when model exposure is disabled', () => {
    exposure.model = false;
    mockBillingEnabled = false;
    const { result } = renderHook(() => useSettingMenuItems(items));
    const keys = result.current.map((item) => item.key);

    expect(keys).toEqual(['/profile', '/api']);
  });
});
