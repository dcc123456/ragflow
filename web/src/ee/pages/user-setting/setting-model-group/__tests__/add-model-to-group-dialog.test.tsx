import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from '@testing-library/react';

import { AddModelToGroupDialog } from '../add-model-to-group-dialog';

const mockRun = jest.fn();
const mockModelTreeSelectProps = jest.fn();
const mockDirectModelTreeSelectProps = jest.fn();

jest.mock('@/ee/hooks/use-model-group-request', () => ({
  useAddModelGroupMember: () => ({ loading: false, run: mockRun }),
}));

jest.mock('@/components/model-tree-select', () => {
  type MockSelectProps = {
    onChange: (value: string) => void;
    modelTypes?: string[];
    allowClear?: boolean;
    requireModelId?: boolean;
  };
  const createSelect = (recordProps: (props: MockSelectProps) => void) =>
    function Select(props: MockSelectProps) {
      recordProps(props);
      return (
        <button
          type="button"
          data-testid="choose-model"
          onClick={() => props.onChange('model-1')}
        >
          choose model
        </button>
      );
    };

  return {
    ModelTreeSelect: createSelect((props) => mockModelTreeSelectProps(props)),
    DirectModelTreeSelect: createSelect((props) =>
      mockDirectModelTreeSelectProps(props),
    ),
  };
});

jest.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

function chooseModel() {
  fireEvent.click(screen.getByTestId('choose-model'));
}

function clickSave() {
  fireEvent.click(screen.getByRole('button', { name: 'common.save' }));
}

describe('AddModelToGroupDialog', () => {
  beforeEach(() => {
    mockRun.mockReset();
    mockModelTreeSelectProps.mockReset();
    mockDirectModelTreeSelectProps.mockReset();
  });

  it('uses the direct selector with every member type and no nested groups', () => {
    render(
      <AddModelToGroupDialog
        groupId="group-id"
        members={[]}
        hideModal={jest.fn()}
      />,
    );

    expect(mockDirectModelTreeSelectProps).toHaveBeenCalled();
    expect(mockModelTreeSelectProps).not.toHaveBeenCalled();
    const props = mockDirectModelTreeSelectProps.mock.lastCall?.[0];
    expect(props).toMatchObject({
      modelTypes: [
        'chat',
        'vlm',
        'decision',
        'embedding',
        'asr',
        'rerank',
        'tts',
        'ocr',
      ],
      allowClear: true,
      requireModelId: true,
    });
  });

  it('submits once while the first add request is still pending', async () => {
    let resolveAdd!: (code: number) => void;
    mockRun.mockImplementation(
      () =>
        new Promise<number>((resolve) => {
          resolveAdd = resolve;
        }),
    );
    const hideModal = jest.fn();
    render(
      <AddModelToGroupDialog
        groupId="group-id"
        members={[]}
        hideModal={hideModal}
      />,
    );

    chooseModel();
    clickSave();
    clickSave();

    await waitFor(() =>
      expect(mockRun).toHaveBeenCalledWith({ model_id: 'model-1' }),
    );
    expect(mockRun).toHaveBeenCalledTimes(1);
    expect(hideModal).not.toHaveBeenCalled();

    await act(async () => {
      resolveAdd(0);
      await Promise.resolve();
    });
    await waitFor(() => expect(hideModal).toHaveBeenCalledTimes(1));
  });

  it('blocks a model that is already a group member', () => {
    const hideModal = jest.fn();
    render(
      <AddModelToGroupDialog
        groupId="group-id"
        members={[{ model_id: 'model-1' } as any]}
        hideModal={hideModal}
      />,
    );

    chooseModel();

    expect(screen.getByRole('alert')).toHaveTextContent(
      'setting.modelGroupPage.memberAlreadyAdded',
    );
    expect(screen.getByRole('button', { name: 'common.save' })).toBeDisabled();
    clickSave();
    expect(mockRun).not.toHaveBeenCalled();
    expect(hideModal).not.toHaveBeenCalled();
  });

  it('keeps the selected model after a nonzero result and allows a successful retry', async () => {
    mockRun.mockResolvedValueOnce(1).mockResolvedValueOnce(0);
    const hideModal = jest.fn();
    render(
      <AddModelToGroupDialog
        groupId="group-id"
        members={[]}
        hideModal={hideModal}
      />,
    );

    chooseModel();
    clickSave();

    await waitFor(() => expect(mockRun).toHaveBeenCalledTimes(1));
    expect(hideModal).not.toHaveBeenCalled();

    clickSave();

    await waitFor(() => expect(mockRun).toHaveBeenCalledTimes(2));
    expect(mockRun).toHaveBeenNthCalledWith(2, { model_id: 'model-1' });
    await waitFor(() => expect(hideModal).toHaveBeenCalledTimes(1));
  });

  it('keeps the selected model after a rejected request and allows a successful retry', async () => {
    mockRun
      .mockRejectedValueOnce(new Error('add failed'))
      .mockResolvedValueOnce(0);
    const hideModal = jest.fn();
    render(
      <AddModelToGroupDialog
        groupId="group-id"
        members={[]}
        hideModal={hideModal}
      />,
    );

    chooseModel();
    clickSave();

    await waitFor(() => expect(mockRun).toHaveBeenCalledTimes(1));
    expect(hideModal).not.toHaveBeenCalled();

    clickSave();

    await waitFor(() => expect(mockRun).toHaveBeenCalledTimes(2));
    expect(mockRun).toHaveBeenNthCalledWith(2, { model_id: 'model-1' });
    await waitFor(() => expect(hideModal).toHaveBeenCalledTimes(1));
  });
});
