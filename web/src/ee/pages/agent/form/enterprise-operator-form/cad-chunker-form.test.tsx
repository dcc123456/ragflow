import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { TooltipProvider } from '@/components/ui/tooltip';
import CadChunkerForm from './cad-chunker-form';
import { EnterpriseOperatorForm } from '.';

jest.mock('@/pages/agent/hooks/use-watch-form-change', () => ({
  useWatchFormChange: () => {},
}));

jest.mock('@/hooks/use-llm-request', () => ({
  useFetchDefaultModelDictionary: () => ({ img2txt_id: 'model-A' }),
}));

jest.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

jest.mock('@/components/model-tree-select', () => ({
  ModelTypeMap: { img2txt_id: ['vision'] },
  ModelTreeSelectFormField: ({ name }: { name: string }) => {
    const { Controller, useFormContext } =
      require('react-hook-form') as typeof import('react-hook-form');
    const { control } = useFormContext();
    return (
      <Controller
        name={name}
        control={control}
        render={({ field }) => (
          <select aria-label="Vision model" {...field}>
            <option value="">Default</option>
            <option value="model-A">A</option>
            <option value="model-B">B</option>
          </select>
        )}
      />
    );
  },
}));

function buildNode(
  vision?: Partial<{
    enabled: boolean;
    llm_id: string;
    system_prompt: string;
  }>,
): import('@/interfaces/database/agent').RAGFlowNodeType {
  return {
    id: 'CADChunker:SpatialSheet',
    type: 'chunkerNode',
    position: { x: 0, y: 0 },
    data: {
      label: 'CADChunker',
      name: 'CAD Chunker',
      form: vision ? { vision } : {},
    },
  } as import('@/interfaces/database/agent').RAGFlowNodeType;
}

function renderForm(ui: import('react').ReactElement) {
  return render(<TooltipProvider>{ui}</TooltipProvider>);
}

beforeAll(() => {
  Object.defineProperty(globalThis, 'ResizeObserver', {
    configurable: true,
    value: class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  });
});

describe('CadChunkerForm', () => {
  it('prefills the default vision model and emits the backend parameter shape', async () => {
    const onValuesChange = jest.fn();
    renderForm(
      <CadChunkerForm node={buildNode()} onValuesChange={onValuesChange} />,
    );

    const toggle = screen.getByRole('switch');
    expect(toggle).not.toBeChecked();
    expect(screen.queryByLabelText('Vision model')).not.toBeInTheDocument();

    fireEvent.click(toggle);

    expect(screen.getByRole('switch')).toBeChecked();
    expect(screen.getByLabelText('Vision model')).toHaveValue('model-A');
    fireEvent.change(screen.getByLabelText('Vision model'), {
      target: { value: 'model-B' },
    });
    fireEvent.change(
      screen.getByPlaceholderText('flow.systemPromptPlaceholder'),
      {
        target: { value: 'Describe visible connections' },
      },
    );

    await waitFor(() => {
      expect(onValuesChange).toHaveBeenLastCalledWith({
        vision: {
          enabled: true,
          llm_id: 'model-B',
          system_prompt: 'Describe visible connections',
        },
      });
    });
  });

  it('restores saved values and keeps them across disable and enable', () => {
    renderForm(
      <CadChunkerForm
        node={buildNode({
          enabled: true,
          llm_id: 'model-B',
          system_prompt: 'Saved prompt',
        })}
      />,
    );

    expect(screen.getByRole('switch')).toBeChecked();
    expect(screen.getByLabelText('Vision model')).toHaveValue('model-B');
    expect(
      screen.getByPlaceholderText('flow.systemPromptPlaceholder'),
    ).toHaveValue('Saved prompt');

    fireEvent.click(screen.getByRole('switch'));
    expect(screen.queryByLabelText('Vision model')).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('switch'));
    expect(screen.getByLabelText('Vision model')).toHaveValue('model-B');
  });

  it('fills missing nested defaults without replacing saved values', async () => {
    const onValuesChange = jest.fn();
    renderForm(
      <CadChunkerForm
        node={buildNode({ enabled: true })}
        onValuesChange={onValuesChange}
      />,
    );

    expect(screen.getByRole('switch')).toBeChecked();
    expect(screen.getByLabelText('Vision model')).toHaveValue('');
    await waitFor(() => {
      expect(onValuesChange).toHaveBeenLastCalledWith({
        vision: { enabled: true, llm_id: '', system_prompt: '' },
      });
    });
  });
});

describe('EnterpriseOperatorForm', () => {
  it('renders CADChunker and leaves other operators empty', () => {
    const { rerender } = renderForm(
      <EnterpriseOperatorForm node={buildNode()} />,
    );
    expect(screen.getByRole('switch')).toBeInTheDocument();

    const otherNode = buildNode();
    otherNode.data.label = 'OtherChunker';
    rerender(
      <TooltipProvider>
        <EnterpriseOperatorForm node={otherNode} />
      </TooltipProvider>,
    );
    expect(screen.queryByRole('switch')).not.toBeInTheDocument();
  });
});
