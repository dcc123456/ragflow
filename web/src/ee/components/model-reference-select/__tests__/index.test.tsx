import { fireEvent, render, screen } from '@testing-library/react';
import { forwardRef } from 'react';
import { ModelReferenceSelect } from '../index';

const GroupRef = 'gp-0123456789abcdef0123456789abcdef';
const mockDirectModelSelectProps = jest.fn();
const mockModelGroupTreeSelectProps = jest.fn();
const mockUseFetchTenantInfo = jest.fn();

jest.mock('@/hooks/use-user-setting-request', () => ({
  useFetchTenantInfo: (enabled?: boolean) => mockUseFetchTenantInfo(enabled),
}));

jest.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

jest.mock('@/constants/llm', () => ({
  ModelTypeProviderMap: {
    chat: ['ChatProvider'],
    embedding: ['EmbeddingProvider'],
  },
}));

jest.mock('../model-group-tree-select', () => ({
  ModelGroupTreeSelect: (props: Record<string, unknown>) => {
    mockModelGroupTreeSelectProps(props);
    return (
      <>
        {props.dropdownHeader as React.ReactNode}
        <div data-testid="group-select" />
      </>
    );
  },
}));

const FakeDirectSelect = forwardRef<
  HTMLButtonElement,
  import('@/components/model-tree-select').ModelTreeSelectProps
>(function FakeDirectSelect(props, ref) {
  mockDirectModelSelectProps(props);
  return (
    <>
      {props.dropdownHeader}
      <button ref={ref} type="button" data-testid="direct-select">
        {props.value || 'direct-empty'}
      </button>
    </>
  );
});

function selector(
  props: Partial<
    import('@/components/model-tree-select').ModelTreeSelectProps
  > = {},
) {
  return (
    <ModelReferenceSelect
      directModelSelect={FakeDirectSelect}
      modelTypes={['chat']}
      {...props}
    />
  );
}

function lastDirectProps() {
  return mockDirectModelSelectProps.mock.lastCall?.[0];
}

function lastGroupProps() {
  return mockModelGroupTreeSelectProps.mock.lastCall?.[0];
}

describe('ModelReferenceSelect', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockUseFetchTenantInfo.mockReturnValue({
      data: { tenant_id: 'tenant-1' },
      loading: false,
      isFetched: true,
    });
  });

  it('defaults a persisted group reference to the checked group mode', () => {
    const renderSelected = jest.fn();
    render(selector({ value: GroupRef, renderSelected }));

    expect(
      screen.getByRole('radio', {
        name: 'setting.modelGroupSelector.group',
      }),
    ).toBeChecked();
    expect(
      screen.getByRole('radio', {
        name: 'setting.modelGroupSelector.direct',
      }),
    ).not.toBeChecked();
    expect(screen.getByTestId('group-select')).toBeInTheDocument();
    expect(screen.queryByTestId('direct-select')).not.toBeInTheDocument();
    expect(lastGroupProps()).toMatchObject({
      modelTypes: ['chat'],
      value: GroupRef,
    });
    expect(lastGroupProps().renderSelected).toBeUndefined();
  });

  it('keeps the selected model across a mode round trip without emitting', () => {
    const onChange = jest.fn();
    render(selector({ value: 'model-1', onChange }));

    fireEvent.click(
      screen.getByRole('radio', { name: 'setting.modelGroupSelector.group' }),
    );
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.getByTestId('group-select')).toBeInTheDocument();
    // The group selector keeps showing the persisted model (echoed as a
    // non-selectable entry) and stays usable so a group can be picked.
    expect(lastGroupProps()).toMatchObject({
      value: 'model-1',
      disabled: false,
    });

    fireEvent.click(
      screen.getByRole('radio', {
        name: 'setting.modelGroupSelector.direct',
      }),
    );
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.getByTestId('direct-select')).toHaveTextContent('model-1');
    expect(lastDirectProps()).toMatchObject({ value: 'model-1' });
    expect(lastDirectProps().disabled).toBeFalsy();
  });

  it('keeps the selected group across a mode round trip without emitting', () => {
    const onChange = jest.fn();
    render(selector({ value: GroupRef, onChange }));

    fireEvent.click(
      screen.getByRole('radio', {
        name: 'setting.modelGroupSelector.direct',
      }),
    );
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.getByTestId('direct-select')).toBeInTheDocument();
    expect(lastDirectProps()).toMatchObject({ value: '', disabled: false });

    fireEvent.click(
      screen.getByRole('radio', { name: 'setting.modelGroupSelector.group' }),
    );
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.getByTestId('group-select')).toBeInTheDocument();
    expect(lastGroupProps()).toMatchObject({
      value: GroupRef,
      disabled: false,
    });
  });

  it('switches from an empty value without emitting a redundant clear', () => {
    const onChange = jest.fn();
    render(selector({ value: '', onChange }));

    fireEvent.click(
      screen.getByRole('radio', {
        name: 'setting.modelGroupSelector.group',
      }),
    );

    expect(onChange).not.toHaveBeenCalled();
    expect(
      screen.getByRole('radio', {
        name: 'setting.modelGroupSelector.group',
      }),
    ).toBeChecked();
    expect(screen.getByTestId('group-select')).toBeInTheDocument();
    expect(lastGroupProps().value).toBe('');
    expect(lastGroupProps().disabled).toBeFalsy();
  });

  it('follows later nonempty external values without emitting a change', () => {
    const onChange = jest.fn();
    const { rerender } = render(selector({ value: 'model-1', onChange }));

    rerender(selector({ value: GroupRef, onChange }));
    expect(
      screen.getByRole('radio', {
        name: 'setting.modelGroupSelector.group',
      }),
    ).toBeChecked();
    expect(screen.getByTestId('group-select')).toBeInTheDocument();

    rerender(selector({ value: 'model-2', onChange }));
    expect(
      screen.getByRole('radio', {
        name: 'setting.modelGroupSelector.direct',
      }),
    ).toBeChecked();
    expect(screen.getByTestId('direct-select')).toHaveTextContent('model-2');
    expect(onChange).not.toHaveBeenCalled();
  });

  it('enables group mode for an owner in the active tenant', () => {
    render(selector({ ownerTenantId: 'tenant-1', value: GroupRef }));

    expect(screen.getByTestId('group-select')).toBeInTheDocument();
    expect(screen.queryByTestId('direct-select')).not.toBeInTheDocument();
    expect(lastGroupProps().value).toBe(GroupRef);
  });

  it('masks a persisted group value until tenant access settles without clearing it', () => {
    const onChange = jest.fn();
    mockUseFetchTenantInfo.mockReturnValue({
      data: {},
      loading: true,
      isFetched: false,
      isError: false,
    });
    const { rerender } = render(
      selector({ ownerTenantId: 'tenant-1', value: GroupRef, onChange }),
    );

    expect(screen.getByTestId('direct-select')).toHaveTextContent(
      'direct-empty',
    );
    expect(lastDirectProps()).toMatchObject({ value: '', disabled: true });
    expect(screen.queryByRole('radio')).not.toBeInTheDocument();
    expect(mockModelGroupTreeSelectProps).not.toHaveBeenCalled();
    expect(onChange).not.toHaveBeenCalled();

    mockUseFetchTenantInfo.mockReturnValue({
      data: { tenant_id: 'tenant-1' },
      loading: false,
      isFetched: true,
      isError: false,
    });
    rerender(
      selector({ ownerTenantId: 'tenant-1', value: GroupRef, onChange }),
    );

    expect(screen.getByTestId('group-select')).toBeInTheDocument();
    expect(lastGroupProps()).toMatchObject({
      value: GroupRef,
      disabled: false,
    });
    expect(screen.queryByTestId('direct-select')).not.toBeInTheDocument();
    expect(onChange).not.toHaveBeenCalled();

    rerender(selector({ ownerTenantId: 'owner-1', value: GroupRef, onChange }));

    expect(screen.getByTestId('direct-select')).toHaveTextContent(GroupRef);
    expect(lastDirectProps()).toMatchObject({ value: GroupRef });
    expect(lastDirectProps().disabled).toBeFalsy();
    expect(screen.queryByRole('radio')).not.toBeInTheDocument();
    expect(onChange).not.toHaveBeenCalled();
  });

  it('renders only the direct selector for a cross-tenant owner', () => {
    render(selector({ ownerTenantId: 'owner-1', value: GroupRef }));

    expect(screen.getByTestId('direct-select')).toHaveTextContent(GroupRef);
    expect(lastDirectProps().value).toBe(GroupRef);
    expect(lastDirectProps().disabled).toBeFalsy();
    expect(screen.queryByRole('radio')).not.toBeInTheDocument();
    expect(screen.queryByTestId('group-select')).not.toBeInTheDocument();
    expect(mockModelGroupTreeSelectProps).not.toHaveBeenCalled();
  });

  it('renders only the direct selector when tenant info is skipped', () => {
    render(selector({ skipTenantInfo: true, value: GroupRef }));

    expect(mockUseFetchTenantInfo).toHaveBeenLastCalledWith(false);
    expect(screen.getByTestId('direct-select')).toHaveTextContent(GroupRef);
    expect(lastDirectProps().value).toBe(GroupRef);
    expect(lastDirectProps().disabled).toBeFalsy();
    expect(screen.queryByRole('radio')).not.toBeInTheDocument();
    expect(screen.queryByTestId('group-select')).not.toBeInTheDocument();
    expect(mockModelGroupTreeSelectProps).not.toHaveBeenCalled();
  });

  it('shares explicit providers between direct and group modes', () => {
    const { rerender } = render(
      selector({ value: 'model-1', providers: ['ExplicitProvider'] }),
    );
    expect(lastDirectProps().providers).toEqual(['ExplicitProvider']);

    rerender(selector({ value: GroupRef, providers: ['ExplicitProvider'] }));
    expect(lastGroupProps().providers).toEqual(['ExplicitProvider']);
  });

  it('uses the first declared provider fallback in model type order', () => {
    render(
      selector({
        modelTypes: ['unknown', 'embedding', 'chat'],
        ownerTenantId: 'owner-1',
      }),
    );

    expect(lastDirectProps().providers).toEqual(['EmbeddingProvider']);
  });

  it('disables both mode radios and the active group selector', () => {
    render(selector({ value: GroupRef, disabled: true }));

    expect(screen.getAllByRole('radio')).toHaveLength(2);
    for (const radio of screen.getAllByRole('radio')) {
      expect(radio).toBeDisabled();
    }
    expect(lastGroupProps().disabled).toBe(true);
  });
});
