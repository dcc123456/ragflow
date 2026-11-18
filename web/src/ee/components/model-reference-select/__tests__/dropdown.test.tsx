import { TreeSelect } from '@/components/tree-select';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { forwardRef } from 'react';

jest.mock('@/hooks/use-user-setting-request', () => ({
  useFetchTenantInfo: () => ({
    data: { tenant_id: 'tenant-1' },
    loading: false,
    isFetched: true,
  }),
}));

jest.mock('@/constants/llm', () => ({
  ModelTypeProviderMap: { chat: ['ChatProvider'] },
}));

jest.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

const MockGroupSelect = forwardRef<
  HTMLButtonElement,
  import('@/components/model-tree-select').ModelTreeSelectProps
>(function MockGroupSelect(props, ref) {
  return (
    <TreeSelect
      ref={ref}
      data={[
        {
          id: 'gp-0123456789abcdef0123456789abcdef',
          title: 'Group option',
        },
      ]}
      value={props.value}
      onChange={props.onChange}
      showSearch
      dropdownHeader={props.dropdownHeader}
      open={props.open}
      onOpenChange={props.onOpenChange}
      testId="group-select"
    />
  );
});

jest.mock('../model-group-tree-select', () => ({
  ModelGroupTreeSelect: MockGroupSelect,
}));

const FakeDirectSelect = forwardRef<
  HTMLButtonElement,
  import('@/components/model-tree-select').ModelTreeSelectProps
>(function FakeDirectSelect(props, ref) {
  return (
    <TreeSelect
      ref={ref}
      data={[{ id: 'model-1', title: 'Direct option' }]}
      value={props.value}
      onChange={props.onChange}
      showSearch
      dropdownHeader={props.dropdownHeader}
      open={props.open}
      onOpenChange={props.onOpenChange}
      testId="direct-select"
    />
  );
});

const { ModelReferenceSelect } =
  jest.requireActual<typeof import('../index')>('../index');

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

describe('ModelReferenceSelect dropdown', () => {
  it('keeps both mode radios inside the open dropdown while switching lists', () => {
    const onChange = jest.fn();
    render(
      <ModelReferenceSelect
        directModelSelect={FakeDirectSelect}
        modelTypes={['chat']}
        value=""
        onChange={onChange}
      />,
    );

    expect(screen.queryByRole('radio')).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId('direct-select'));

    let dropdown = screen.getByRole('dialog');
    const groupRadio = within(dropdown).getByRole('radio', {
      name: 'setting.modelGroupSelector.group',
    });
    const search = within(dropdown).getByPlaceholderText('common.search');
    expect(
      groupRadio.compareDocumentPosition(search) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(within(dropdown).getByText('Direct option')).toBeInTheDocument();

    fireEvent.click(groupRadio);
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.getByTestId('group-select')).toHaveAttribute(
      'aria-expanded',
      'true',
    );
    dropdown = screen.getByRole('dialog');
    expect(within(dropdown).getByText('Group option')).toBeInTheDocument();
    expect(screen.queryByText('Direct option')).not.toBeInTheDocument();
    expect(
      within(dropdown).getByRole('radio', {
        name: 'setting.modelGroupSelector.group',
      }),
    ).toBeChecked();

    fireEvent.click(
      within(dropdown).getByRole('radio', {
        name: 'setting.modelGroupSelector.direct',
      }),
    );
    expect(screen.getByTestId('direct-select')).toHaveAttribute(
      'aria-expanded',
      'true',
    );
    expect(screen.getByText('Direct option')).toBeInTheDocument();
  });
});
