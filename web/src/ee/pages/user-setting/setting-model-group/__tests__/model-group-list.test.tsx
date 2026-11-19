import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react';

import { TooltipProvider } from '@/components/ui/tooltip';
import { ModelGroupList } from '../model-group-list';

const mockMembers = jest.fn();
const mockDeleteGroup = jest.fn();
const mockRemoveMember = jest.fn();
let mockDeleteGroupLoading = false;
let mockRemoveMemberLoading = false;

jest.mock('@/ee/hooks/use-model-group-request', () => ({
  useModelGroupMembers: (...args: unknown[]) => mockMembers(...args),
  useReorderModelGroupMembers: () => ({
    loading: false,
    run: jest.fn().mockResolvedValue(0),
  }),
  useDeleteModelGroup: () => ({
    loading: mockDeleteGroupLoading,
    run: mockDeleteGroup,
  }),
  useRemoveModelGroupMember: () => ({
    loading: mockRemoveMemberLoading,
    run: mockRemoveMember,
  }),
  useUpdateModelGroupMemberStatus: () => ({
    loading: false,
    run: jest.fn().mockResolvedValue(0),
  }),
}));

jest.mock('@/components/confirm-delete-dialog', () => ({
  ConfirmDeleteDialog: ({
    children,
    title,
    onOk,
    confirmButtonTestId,
  }: any) => (
    <>
      {children}
      <button type="button" data-testid={confirmButtonTestId} onClick={onOk}>
        {title}
      </button>
    </>
  ),
}));

jest.mock('../add-model-to-group-dialog', () => ({
  AddModelToGroupDialog: ({ groupId, members }: any) => (
    <div data-testid="add-dialog">
      {groupId}:{members.length}
    </div>
  ),
}));

jest.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

const group = {
  id: 'g1',
  tenant_id: 't1',
  nickname: 'Owner',
  group_name: 'a/b ?',
  status: 'active',
};

const member = {
  id: 'map-1',
  model_id: 'model-1',
  position: 3,
  provider: 'OpenAI',
  instance: 'default',
  model: 'gpt-4',
  model_type: ['chat'],
  status: 'active',
  group_name: group.group_name,
  group_id: group.id,
};

describe('ModelGroupList', () => {
  beforeEach(() => {
    mockMembers.mockReset();
    mockDeleteGroup.mockReset();
    mockRemoveMember.mockReset();
    mockDeleteGroupLoading = false;
    mockRemoveMemberLoading = false;
    mockMembers.mockReturnValue({
      data: [member],
      loading: false,
      isError: false,
      refetch: jest.fn(),
    });
    mockDeleteGroup.mockResolvedValue(0);
    mockRemoveMember.mockResolvedValue(0);
  });

  it('expands only the first group by default while preserving independent toggling', () => {
    const secondGroup = {
      ...group,
      id: 'g2',
      group_name: 'second group',
      status: 'inactive',
    };
    render(
      <TooltipProvider>
        <ModelGroupList groups={[group, secondGroup]} />
      </TooltipProvider>,
    );

    const firstTrigger = screen.getByRole('button', { name: /a\/b \?/ });
    const secondTrigger = screen.getByRole('button', {
      name: /second group/,
    });
    expect(firstTrigger).toHaveAttribute('aria-expanded', 'true');
    expect(secondTrigger).toHaveAttribute('aria-expanded', 'false');
    expect(mockMembers).toHaveBeenCalledWith(group.id, true);
    expect(mockMembers).toHaveBeenCalledWith(secondGroup.id, false);

    fireEvent.click(secondTrigger);
    expect(firstTrigger).toHaveAttribute('aria-expanded', 'true');
    expect(secondTrigger).toHaveAttribute('aria-expanded', 'true');

    fireEvent.click(firstTrigger);
    expect(firstTrigger).toHaveAttribute('aria-expanded', 'false');
    expect(secondTrigger).toHaveAttribute('aria-expanded', 'true');
  });

  it('loads the first group by default and runs add, delete and remove actions', async () => {
    render(
      <TooltipProvider>
        <ModelGroupList groups={[group]} />
      </TooltipProvider>,
    );

    expect(mockMembers).toHaveBeenLastCalledWith(group.id, true);
    expect(await screen.findByText('gpt-4')).toBeInTheDocument();
    for (const label of [
      'setting.modelGroupPage.modelName',
      'setting.modelGroupPage.modelType',
      'setting.modelGroupPage.instanceName',
      'setting.modelGroupPage.status',
    ]) {
      expect(screen.getByText(label)).toBeInTheDocument();
    }
    expect(screen.getByRole('cell', { name: '3' })).toBeInTheDocument();
    expect(screen.getByRole('cell', { name: 'active' })).toBeInTheDocument();

    fireEvent.click(
      screen.getByRole('button', {
        name: 'setting.modelGroupPage.addMember',
      }),
    );
    expect(screen.getByTestId('add-dialog')).toHaveTextContent('a/b ?:1');

    fireEvent.click(screen.getByTestId('confirm-delete-group-g1'));
    await waitFor(() =>
      expect(mockDeleteGroup).toHaveBeenCalledWith(undefined),
    );

    fireEvent.click(screen.getByTestId('confirm-remove-map-1'));
    await waitFor(() =>
      expect(mockRemoveMember).toHaveBeenCalledWith({
        composite_model_name: 'gpt-4@default@OpenAI',
      }),
    );
  });

  it('reveals the group delete action on header hover or focus', () => {
    render(
      <TooltipProvider>
        <ModelGroupList groups={[group]} />
      </TooltipProvider>,
    );

    const groupHeader = screen.getByTestId(
      'confirm-delete-group-g1',
    ).parentElement;
    expect(groupHeader).toHaveClass('group');
    const deleteButton = within(groupHeader as HTMLElement).getByRole(
      'button',
      { name: 'common.delete' },
    );
    expect(deleteButton).toHaveClass(
      'opacity-0',
      'transition-opacity',
      'group-hover:opacity-100',
      'group-focus-within:opacity-100',
    );
    expect(deleteButton).toBeEnabled();
    deleteButton.focus();
    expect(deleteButton).toHaveFocus();
  });

  it('reveals the member remove action on row hover or focus', async () => {
    render(
      <TooltipProvider>
        <ModelGroupList groups={[group]} />
      </TooltipProvider>,
    );

    const memberRow = (await screen.findByText('gpt-4')).closest('tr');
    expect(memberRow).toHaveClass('group');
    const removeButton = within(memberRow as HTMLElement).getByRole('button', {
      name: 'common.delete',
    });
    expect(removeButton).toHaveClass(
      'opacity-0',
      'transition-opacity',
      'group-hover:opacity-100',
      'group-focus-within:opacity-100',
    );
    expect(removeButton).toBeEnabled();
    removeButton.focus();
    expect(removeButton).toHaveFocus();
  });

  it('disables group and member actions while their mutations are loading', async () => {
    mockDeleteGroupLoading = true;
    mockRemoveMemberLoading = true;
    render(
      <TooltipProvider>
        <ModelGroupList groups={[group]} />
      </TooltipProvider>,
    );

    const groupHeader = screen.getByTestId(
      'confirm-delete-group-g1',
    ).parentElement;
    const deleteButton = within(groupHeader as HTMLElement).getByRole(
      'button',
      { name: 'common.delete' },
    );
    expect(deleteButton).toBeDisabled();

    const memberRow = (await screen.findByText('gpt-4')).closest('tr');
    const removeButton = within(memberRow as HTMLElement).getByRole('button', {
      name: 'common.delete',
    });
    expect(removeButton).toBeDisabled();
  });

  it('provides a bounded vertical scroll container', () => {
    const { container } = render(
      <TooltipProvider>
        <ModelGroupList groups={[group]} />
      </TooltipProvider>,
    );

    expect(container.firstElementChild).toHaveClass(
      'flex-1',
      'min-h-0',
      'overflow-y-auto',
    );
  });

  it('renders a member with a null model type as an empty type cell', async () => {
    mockMembers.mockReturnValue({
      data: [{ ...member, model_type: null } as any],
      loading: false,
      isError: false,
      refetch: jest.fn(),
    });
    render(
      <TooltipProvider>
        <ModelGroupList groups={[group]} />
      </TooltipProvider>,
    );

    const modelName = await screen.findByText('gpt-4');
    const row = modelName.closest('tr');
    expect(row).not.toBeNull();
    const cells = within(row as HTMLElement).getAllByRole('cell');
    expect(cells[1].textContent).toBe('');
  });

  it('retries only the member request after a member loading error', async () => {
    const refetch = jest.fn();
    mockMembers.mockReturnValue({
      data: [],
      loading: false,
      isError: true,
      refetch,
    });
    render(
      <TooltipProvider>
        <ModelGroupList groups={[group]} />
      </TooltipProvider>,
    );

    const groupTrigger = screen.getByRole('button', { name: /a\/b \?/ });
    fireEvent.click(
      await screen.findByRole('button', {
        name: 'setting.modelGroupPage.retry',
      }),
    );

    expect(refetch).toHaveBeenCalledTimes(1);
    expect(groupTrigger).toBeEnabled();
  });

  it('uses green and red accessible dots for active and inactive groups', () => {
    render(
      <TooltipProvider>
        <ModelGroupList
          groups={[
            group,
            { ...group, id: 'g2', group_name: 'inactive', status: 'inactive' },
          ]}
        />
      </TooltipProvider>,
    );

    expect(screen.getByRole('img', { name: 'active' })).toHaveClass(
      'size-2',
      'rounded-full',
      'bg-state-success',
    );
    expect(screen.getByRole('img', { name: 'inactive' })).toHaveClass(
      'size-2',
      'rounded-full',
      'bg-state-error',
    );
  });

  it('places the chevron before a group name that toggles expansion', () => {
    render(
      <TooltipProvider>
        <ModelGroupList groups={[group]} />
      </TooltipProvider>,
    );

    const trigger = screen.getByRole('button', { name: /a\/b \?/ });
    const chevron = trigger.querySelector('svg');
    const groupName = screen.getByText(group.group_name);
    expect(chevron).not.toBeNull();
    expect(
      chevron!.compareDocumentPosition(groupName) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();

    expect(trigger).toHaveAttribute('aria-expanded', 'true');
    fireEvent.click(groupName);
    expect(trigger).toHaveAttribute('aria-expanded', 'false');
    fireEvent.click(groupName);
    expect(trigger).toHaveAttribute('aria-expanded', 'true');
  });

  it('places expanded Add Model before Delete without toggling the group', () => {
    render(
      <TooltipProvider>
        <ModelGroupList groups={[group]} />
      </TooltipProvider>,
    );

    const trigger = screen.getByRole('button', { name: /a\/b \?/ });
    expect(trigger).toHaveAttribute('aria-expanded', 'true');
    expect(
      screen.getByRole('button', { name: 'setting.modelGroupPage.addMember' }),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByText(group.group_name));
    expect(trigger).toHaveAttribute('aria-expanded', 'false');
    expect(
      screen.queryByRole('button', {
        name: 'setting.modelGroupPage.addMember',
      }),
    ).not.toBeInTheDocument();

    fireEvent.click(screen.getByText(group.group_name));
    const addButton = screen.getByRole('button', {
      name: 'setting.modelGroupPage.addMember',
    });
    const actions = screen.getByTestId('confirm-delete-group-g1')
      .parentElement as HTMLElement;
    const deleteButton = within(actions).getByRole('button', {
      name: 'common.delete',
    });

    expect(addButton.parentElement).toBe(actions);
    expect(
      addButton.compareDocumentPosition(deleteButton) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();

    fireEvent.click(addButton);
    expect(screen.getByTestId('add-dialog')).toBeInTheDocument();
    expect(trigger).toHaveAttribute('aria-expanded', 'true');
  });
});
