import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react';

import { CreateModelGroupDialog } from '../create-model-group-dialog';

const mockRun = jest.fn();

jest.mock('@/ee/hooks/use-model-group-request', () => ({
  useCreateModelGroup: () => ({ loading: false, run: mockRun }),
}));

jest.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

const existingGroup = {
  id: 'g1',
  tenant_id: 't1',
  group_name: 'existing',
  status: 'active',
};

function changeGroupName(value: string) {
  expect(
    screen.getByText('setting.modelGroupPage.groupName'),
  ).toBeInTheDocument();
  fireEvent.change(screen.getByRole('textbox'), { target: { value } });
}

function clickSave() {
  fireEvent.click(screen.getByRole('button', { name: 'common.save' }));
}

describe('CreateModelGroupDialog', () => {
  beforeEach(() => {
    mockRun.mockReset();
  });

  it('trims the name and closes only after a successful mutation', async () => {
    mockRun.mockResolvedValue(0);
    const hideModal = jest.fn();
    render(<CreateModelGroupDialog groups={[]} hideModal={hideModal} />);

    changeGroupName('  release group  ');
    clickSave();

    await waitFor(() =>
      expect(mockRun).toHaveBeenCalledWith({ name: 'release group' }),
    );
    await waitFor(() => expect(hideModal).toHaveBeenCalledTimes(1));
  });

  it('blocks a duplicate name before calling the API', async () => {
    const hideModal = jest.fn();
    render(
      <CreateModelGroupDialog groups={[existingGroup]} hideModal={hideModal} />,
    );

    changeGroupName(' existing ');
    clickSave();

    expect(
      await screen.findByText('setting.modelGroupPage.nameDuplicate'),
    ).toBeInTheDocument();
    expect(mockRun).not.toHaveBeenCalled();
    expect(hideModal).not.toHaveBeenCalled();
  });

  it('blocks a whitespace-only name before calling the API', async () => {
    const hideModal = jest.fn();
    render(<CreateModelGroupDialog groups={[]} hideModal={hideModal} />);

    changeGroupName('   ');
    clickSave();

    expect(
      await screen.findByText('setting.modelGroupPage.nameRequired'),
    ).toBeInTheDocument();
    expect(mockRun).not.toHaveBeenCalled();
    expect(hideModal).not.toHaveBeenCalled();
  });

  it('submits once while the first create request is still pending', async () => {
    let resolveCreate!: (code: number) => void;
    mockRun.mockImplementation(
      () =>
        new Promise<number>((resolve) => {
          resolveCreate = resolve;
        }),
    );
    const hideModal = jest.fn();
    render(<CreateModelGroupDialog groups={[]} hideModal={hideModal} />);

    changeGroupName('single submit');
    clickSave();
    clickSave();

    await waitFor(() => expect(mockRun).toHaveBeenCalledTimes(1));
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(mockRun).toHaveBeenCalledTimes(1);
    expect(mockRun).toHaveBeenCalledWith({ name: 'single submit' });
    expect(hideModal).not.toHaveBeenCalled();

    await act(async () => {
      resolveCreate(0);
      await Promise.resolve();
    });
    await waitFor(() => expect(hideModal).toHaveBeenCalledTimes(1));
  });

  it('keeps the entered name after a nonzero result and allows a successful retry', async () => {
    mockRun.mockResolvedValueOnce(1).mockResolvedValueOnce(0);
    const hideModal = jest.fn();
    render(<CreateModelGroupDialog groups={[]} hideModal={hideModal} />);

    changeGroupName('retry group');
    clickSave();

    await waitFor(() => expect(mockRun).toHaveBeenCalledTimes(1));
    expect(hideModal).not.toHaveBeenCalled();
    expect(screen.getByRole('textbox')).toHaveValue('retry group');

    clickSave();

    await waitFor(() => expect(mockRun).toHaveBeenCalledTimes(2));
    expect(mockRun).toHaveBeenNthCalledWith(2, { name: 'retry group' });
    await waitFor(() => expect(hideModal).toHaveBeenCalledTimes(1));
  });

  it('keeps the entered name after a rejected request and allows a successful retry', async () => {
    mockRun
      .mockRejectedValueOnce(new Error('create failed'))
      .mockResolvedValueOnce(0);
    const hideModal = jest.fn();
    render(<CreateModelGroupDialog groups={[]} hideModal={hideModal} />);

    changeGroupName('retry group');
    clickSave();

    await waitFor(() => expect(mockRun).toHaveBeenCalledTimes(1));
    expect(hideModal).not.toHaveBeenCalled();
    expect(screen.getByRole('textbox')).toHaveValue('retry group');

    clickSave();

    await waitFor(() => expect(mockRun).toHaveBeenCalledTimes(2));
    expect(mockRun).toHaveBeenNthCalledWith(2, { name: 'retry group' });
    await waitFor(() => expect(hideModal).toHaveBeenCalledTimes(1));
  });
});
