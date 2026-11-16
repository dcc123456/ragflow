import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

import UserDetailAssets from './user-detail-assets';
import { listUserAgents, listUserDatasets } from '@/ee/services/admin-service';

jest.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

jest.mock('@/ee/services/admin-service', () => ({
  listUserAgents: jest.fn(),
  listUserDatasets: jest.fn(),
}));

jest.mock('@/components/ragflow-avatar', () => ({
  RAGFlowAvatar: () => <span data-testid="asset-avatar" />,
}));

jest.mock('@/components/truncated-text', () => ({
  TruncatedText: ({
    children,
    className,
  }: {
    children: React.ReactNode;
    className?: string;
  }) => <span className={className}>{children}</span>,
}));

jest.mock('@/components/table-skeleton', () => ({
  TableSkeleton: () => (
    <tr>
      <td>Loading</td>
    </tr>
  ),
  TableEmpty: () => (
    <tr>
      <td>No results</td>
    </tr>
  ),
}));

jest.mock('@/components/ui/ragflow-pagination', () => ({
  RAGFlowPagination: ({
    current,
    total,
    onChange,
  }: {
    current: number;
    total: number;
    onChange: (page: number, pageSize: number) => void;
  }) => (
    <div>
      <span>Total {total}</span>
      <span>Page {current}</span>
      <button onClick={() => onChange(2, 10)}>Page 2</button>
    </div>
  ),
}));

function renderAssets() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <UserDetailAssets userId="user@example.com" />
    </QueryClientProvider>,
  );
}

describe('UserDetailAssets', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('loads each canvas category only when its tab is selected', async () => {
    jest.mocked(listUserDatasets).mockResolvedValue({
      data: {
        code: 0,
        data: [
          {
            id: 'dataset-1',
            name: 'Dataset one',
            avatar: '',
            tenant_name: 'Team One',
            doc_num: 0,
            file_size: 0,
            chunk_num: 0,
            create_date: '2026-11-13T10:41:30',
            update_date: '2026-11-14T11:42:31',
          },
        ],
        total: 1,
      },
    } as never);
    jest.mocked(listUserAgents).mockImplementation(
      async (_email, params) =>
        ({
          data: {
            code: 0,
            data: [
              {
                id: params.canvas_category,
                title: `${params.canvas_category} asset`,
                avatar: '',
                tenant_name: 'Canvas Team',
                create_date: '2026-11-13T09:08:07',
                update_date: '2026-11-14T10:11:12',
              },
            ],
            total: 1,
          },
        }) as never,
    );

    renderAssets();

    expect(await screen.findByText('Dataset one')).toHaveClass('truncate');
    expect(listUserAgents).not.toHaveBeenCalled();

    // Radix Tabs activates on mousedown rather than click.
    fireEvent.mouseDown(screen.getByRole('tab', { name: 'admin.workflow' }));
    expect(await screen.findByText('agent_canvas asset')).toBeInTheDocument();
    expect(listUserAgents).toHaveBeenLastCalledWith('user@example.com', {
      canvas_category: 'agent_canvas',
      page: 1,
      page_size: 10,
    });

    // Radix Tabs activates on mousedown rather than click.
    fireEvent.mouseDown(screen.getByRole('tab', { name: 'admin.pipeline' }));
    expect(
      await screen.findByText('dataflow_canvas asset'),
    ).toBeInTheDocument();
    expect(listUserAgents).toHaveBeenLastCalledWith('user@example.com', {
      canvas_category: 'dataflow_canvas',
      page: 1,
      page_size: 10,
    });
  });

  it('uses the response total and requests the selected server page', async () => {
    jest.mocked(listUserDatasets).mockResolvedValue({
      data: {
        code: 0,
        data: [
          {
            id: 'dataset-11',
            name: 'Dataset eleven',
            tenant_name: 'Team One',
            doc_num: 0,
            file_size: 0,
            chunk_num: 0,
            create_date: '2026-11-13T10:41:30',
            update_date: '2026-11-14T11:42:31',
          },
        ],
        total: 21,
      },
    } as never);

    renderAssets();

    expect(await screen.findByText('Total 21')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Page 2' }));

    await waitFor(() => {
      expect(listUserDatasets).toHaveBeenLastCalledWith('user@example.com', {
        page: 2,
        page_size: 10,
      });
    });
    // The mock renders both a "Page 2" status span and a "Page 2" button.
    expect(
      screen.getByText('Page 2', { selector: 'span' }),
    ).toBeInTheDocument();
  });

  it('renders the existing empty state for an empty successful page', async () => {
    jest.mocked(listUserDatasets).mockResolvedValue({
      data: { code: 0, data: [], total: 0 },
    } as never);

    renderAssets();

    expect(await screen.findByText('No results')).toBeInTheDocument();
    expect(screen.getByText('Total 0')).toBeInTheDocument();
  });

  it('shows an error state instead of an empty result when loading fails', async () => {
    jest
      .mocked(listUserDatasets)
      .mockRejectedValue(new Error('Network failed'));

    renderAssets();

    expect(
      await screen.findByText('admin.assetLoadFailed'),
    ).toBeInTheDocument();
    expect(screen.queryByText('No results')).not.toBeInTheDocument();
    expect(screen.queryByText('Total 0')).not.toBeInTheDocument();
  });

  it('renders every available Dataset field with formatted values', async () => {
    jest.mocked(listUserDatasets).mockResolvedValue({
      data: {
        code: 0,
        data: [
          {
            id: 'dataset-1',
            avatar: null,
            name: 'Dataset one',
            tenant_name: 'Team One',
            doc_num: 12,
            file_size: 1024,
            chunk_num: 34,
            create_date: '2026-11-13T10:41:30',
            update_date: '2026-11-14T11:42:31',
          },
        ],
        total: 1,
      },
    } as never);

    renderAssets();

    expect(await screen.findByText('Dataset one')).toBeInTheDocument();
    for (const header of [
      'admin.name',
      'admin.assetColumns.team',
      'admin.assetColumns.documentNum',
      'admin.assetColumns.fileSize',
      'admin.assetColumns.chunkNum',
      'admin.assetColumns.createAt',
      'admin.assetColumns.lastSaveAt',
    ]) {
      expect(
        screen.getByRole('columnheader', { name: header }),
      ).toBeInTheDocument();
    }
    expect(screen.getByText('Team One')).toHaveClass('truncate');
    expect(screen.getByText('12')).toBeInTheDocument();
    expect(screen.getByText('1.0 KiB')).toBeInTheDocument();
    expect(screen.getByText('34')).toBeInTheDocument();
    expect(screen.getByText('13/11/2026 10:41:30')).toBeInTheDocument();
    expect(screen.getByText('14/11/2026 11:42:31')).toBeInTheDocument();
  });

  it('renders Team and timestamps for Workflow without Dataset-only columns', async () => {
    jest.mocked(listUserDatasets).mockResolvedValue({
      data: { code: 0, data: [], total: 0 },
    } as never);
    jest.mocked(listUserAgents).mockResolvedValue({
      data: {
        code: 0,
        data: [
          {
            id: 'workflow-1',
            avatar: null,
            title: 'Workflow one',
            tenant_name: 'Canvas Team',
            create_date: '2026-11-13T09:08:07',
            update_date: '2026-11-14T10:11:12',
          },
        ],
        total: 1,
      },
    } as never);

    renderAssets();
    fireEvent.mouseDown(screen.getByRole('tab', { name: 'admin.workflow' }));

    expect(await screen.findByText('Workflow one')).toBeInTheDocument();
    expect(screen.getByText('Canvas Team')).toHaveClass('truncate');
    expect(screen.getByText('13/11/2026 09:08:07')).toBeInTheDocument();
    expect(screen.getByText('14/11/2026 10:11:12')).toBeInTheDocument();
    expect(
      screen.queryByRole('columnheader', {
        name: 'admin.assetColumns.documentNum',
      }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole('columnheader', {
        name: 'admin.assetColumns.fileSize',
      }),
    ).not.toBeInTheDocument();
  });
});
