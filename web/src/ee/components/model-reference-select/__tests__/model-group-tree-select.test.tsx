import { fireEvent, render, screen } from '@testing-library/react';
import { ModelGroupTreeSelect } from '../model-group-tree-select';

const mockUseFetchAllAddedModels = jest.fn();
const mockUseModelGroupOptions = jest.fn();
const mockRefetchModels = jest.fn();
const mockRefetchGroups = jest.fn();

jest.mock('@/hooks/use-llm-request', () => ({
  useFetchAllAddedModels: () => mockUseFetchAllAddedModels(),
}));

jest.mock('@/ee/hooks/use-model-group-options', () => ({
  useModelGroupOptions: (...args: unknown[]) =>
    mockUseModelGroupOptions(...args),
}));

jest.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

const AlphaGroup = {
  id: '0123456789abcdef0123456789abcdef',
  tenant_id: 'tenant-1',
  group_name: 'Alpha group',
  status: 'active',
};
const BetaGroup = {
  id: 'fedcba9876543210fedcba9876543210',
  tenant_id: 'tenant-1',
  group_name: 'Beta group',
  status: 'active',
};
const GammaGroup = {
  id: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
  tenant_id: 'tenant-1',
  group_name: 'Gamma group',
  status: 'active',
};
const Groups = [AlphaGroup, BetaGroup, GammaGroup];
const GroupRefs = {
  alpha: `gp-${AlphaGroup.id}`,
  beta: `gp-${BetaGroup.id}`,
  gamma: `gp-${GammaGroup.id}`,
};
const directModel = {
  model_id: 'model-1',
  status: 'active',
  instance_status: 'active',
  model_type: ['chat'],
  name: 'gpt-4',
  provider_id: 'provider-1',
  provider_name: 'OpenAI',
  instance_id: 'instance-1',
  instance_name: 'default',
};

const OriginalResizeObserver = globalThis.ResizeObserver;

beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
});

afterAll(() => {
  globalThis.ResizeObserver = OriginalResizeObserver;
});

type GroupSelectTestProps = {
  modelTypes: string[];
  providers?: string[];
  value?: string;
  onChange?: (value: string) => void;
  disabled?: boolean;
  placeholder?: string;
  showSearch?: boolean;
  allowClear?: boolean;
  className?: string;
  testId?: string;
};

function renderSelect(props: Partial<GroupSelectTestProps> = {}) {
  const onChange = jest.fn();
  const result = render(
    <ModelGroupTreeSelect
      modelTypes={['chat']}
      testId="model-group-select"
      onChange={onChange}
      {...props}
    />,
  );
  return { onChange, ...result };
}

function trigger() {
  return screen.getByTestId('model-group-select');
}

describe('ModelGroupTreeSelect', () => {
  beforeEach(() => {
    mockUseFetchAllAddedModels.mockReset().mockReturnValue({
      data: [directModel],
      isFetched: true,
      isError: false,
      refetch: mockRefetchModels,
    });
    mockUseModelGroupOptions.mockReset().mockReturnValue({
      groups: Groups,
      compatibleGroupIds: new Set([AlphaGroup.id, GammaGroup.id]),
      isFetched: true,
      isError: false,
      refetch: mockRefetchGroups,
    });
    mockRefetchModels.mockReset().mockResolvedValue(undefined);
    mockRefetchGroups.mockReset().mockResolvedValue(undefined);
  });

  it('lists only compatible groups and passes exact compatibility inputs', () => {
    renderSelect({
      modelTypes: ['chat', 'vision'],
      providers: ['OpenAI'],
      placeholder: 'Pick a group',
    });

    expect(mockUseModelGroupOptions).toHaveBeenCalledWith(
      ['chat', 'vision'],
      [directModel],
      true,
      ['OpenAI'],
    );
    expect(trigger()).toHaveTextContent('Pick a group');

    fireEvent.click(trigger());
    expect(screen.getByText('Alpha group')).toBeInTheDocument();
    expect(screen.getByText('Gamma group')).toBeInTheDocument();
    expect(screen.queryByText('Beta group')).not.toBeInTheDocument();
  });

  it('filters compatible groups through the real TreeSelect search', () => {
    renderSelect();
    fireEvent.click(trigger());

    fireEvent.change(screen.getByPlaceholderText('common.search'), {
      target: { value: 'gamma' },
    });

    expect(screen.getByText('Gamma group')).toBeInTheDocument();
    expect(screen.queryByText('Alpha group')).not.toBeInTheDocument();
    expect(screen.queryByText('Beta group')).not.toBeInTheDocument();
  });

  it('emits the gp reference when a compatible group is clicked', () => {
    const { onChange } = renderSelect();
    fireEvent.click(trigger());
    fireEvent.click(screen.getByText('Alpha group'));

    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith(GroupRefs.alpha);
  });

  it('clears through the real TreeSelect clear icon', () => {
    const { onChange } = renderSelect({
      value: GroupRefs.alpha,
      allowClear: true,
    });
    const clearIcon = trigger().querySelector('svg.lucide-x');
    expect(clearIcon).not.toBeNull();

    fireEvent.click(clearIcon!);
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith('');
  });

  it('echoes an incompatible current group as a disabled unavailable option', () => {
    mockUseModelGroupOptions.mockReturnValue({
      groups: Groups,
      compatibleGroupIds: new Set(),
      isFetched: true,
      isError: false,
      refetch: mockRefetchGroups,
    });
    const { onChange } = renderSelect({ value: GroupRefs.beta });
    const unavailable = 'Beta group (setting.modelGroupSelector.unavailable)';

    expect(trigger()).toHaveTextContent(unavailable);
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();

    fireEvent.click(trigger());
    const matches = screen.getAllByText(unavailable);
    const option = matches[matches.length - 1];
    expect(option.closest('.pointer-events-none')).not.toBeNull();
    fireEvent.click(option);
    expect(onChange).not.toHaveBeenCalled();
  });

  it('uses the raw gp value when an unavailable group has no loaded name', () => {
    mockUseModelGroupOptions.mockReturnValue({
      groups: [AlphaGroup],
      compatibleGroupIds: new Set(),
      isFetched: true,
      isError: false,
      refetch: mockRefetchGroups,
    });
    renderSelect({ value: GroupRefs.beta });

    expect(trigger()).toHaveTextContent(
      `${GroupRefs.beta} (setting.modelGroupSelector.unavailable)`,
    );
  });

  it('keeps the raw gp value without an unavailable conclusion while loading', () => {
    mockUseFetchAllAddedModels.mockReturnValue({
      data: [directModel],
      isFetched: false,
      isError: false,
      refetch: mockRefetchModels,
    });
    renderSelect({ value: GroupRefs.beta });

    expect(trigger()).toHaveTextContent(GroupRefs.beta);
    expect(trigger()).not.toHaveTextContent(
      'setting.modelGroupSelector.unavailable',
    );
    expect(trigger().querySelector('.lucide-triangle-alert')).toBeNull();
  });

  it.each(['models', 'groups'] as const)(
    'keeps the raw value and retries both queries on %s failure',
    (failedQuery) => {
      if (failedQuery === 'models') {
        mockUseFetchAllAddedModels.mockReturnValue({
          data: [directModel],
          isFetched: true,
          isError: true,
          refetch: mockRefetchModels,
        });
      } else {
        mockUseModelGroupOptions.mockReturnValue({
          groups: Groups,
          compatibleGroupIds: new Set(),
          isFetched: true,
          isError: true,
          refetch: mockRefetchGroups,
        });
      }
      renderSelect({ value: GroupRefs.beta });

      expect(trigger()).toHaveTextContent(GroupRefs.beta);
      expect(trigger()).not.toHaveTextContent(
        'setting.modelGroupSelector.unavailable',
      );
      expect(trigger().querySelector('.lucide-triangle-alert')).toBeNull();
      expect(screen.getByRole('alert')).toHaveTextContent(
        'setting.modelGroupPage.loadFailed',
      );

      fireEvent.click(
        screen.getByRole('button', { name: 'setting.modelGroupPage.retry' }),
      );
      expect(mockRefetchModels).toHaveBeenCalledTimes(1);
      expect(mockRefetchGroups).toHaveBeenCalledTimes(1);
    },
  );

  it('leaves disabled interaction and the default placeholder to TreeSelect', () => {
    renderSelect({ disabled: true });

    expect(trigger()).toHaveTextContent('common.pleaseSelect');
    fireEvent.click(trigger());
    expect(
      screen.queryByPlaceholderText('common.search'),
    ).not.toBeInTheDocument();
  });
});
