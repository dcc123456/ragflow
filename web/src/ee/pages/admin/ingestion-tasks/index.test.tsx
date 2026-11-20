import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';

import { TooltipProvider } from '@/components/ui/tooltip';
import AdminIngestionTasks from './index';
import {
  getIngestionLog,
  listIngestionLogMessages,
  listIngestionTasks,
} from '@/ee/services/admin-service';

jest.mock('react-i18next', () => ({
  ...jest.requireActual('react-i18next'),
  useTranslation: () => ({ t: (key: string) => key }),
}));

jest.mock('@/ee/services/admin-service', () => ({
  getIngestionLog: jest.fn(),
  listIngestionLogMessages: jest.fn(),
  listIngestionTasks: jest.fn(),
}));

const renderPage = () =>
  render(
    <TooltipProvider>
      <QueryClientProvider client={new QueryClient()}>
        <AdminIngestionTasks />
      </QueryClientProvider>
    </TooltipProvider>,
  );

const task = {
  id: 'task-1',
  pipeline_log_id: 'log-1',
  pipeline_id: 'pipeline-1',
  pipeline_name: 'Example Pipeline',
  user_id: 'user-1',
  nickname: 'Alice',
  email: 'alice@example.com',
  document_id: 'doc-1',
  document_name: 'report.pdf',
  dataset_id: 'dataset-1',
  dataset_name: 'Example Dataset',
  status: 'COMPLETED' as const,
  create_date: '2026-01-01T16:00:00+08:00',
  update_date: '2026-01-01T16:06:00+08:00',
};

describe('AdminIngestionTasks', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    jest.mocked(listIngestionTasks).mockResolvedValue({
      data: { code: 0, message: 'success', data: [task], total: 1 },
    } as never);
  });

  it('renders ingestion tasks returned by the admin list API', async () => {
    renderPage();

    expect(await screen.findByText('report.pdf')).toBeInTheDocument();
    expect(screen.getByText('Example Dataset')).toBeInTheDocument();
    expect(screen.getByText('Alice')).toBeInTheDocument();
    expect(screen.getByText('alice@example.com')).toBeInTheDocument();
    expect(screen.getByText('Example Pipeline')).toBeInTheDocument();
    expect(screen.getByText('2026-01-01 16:00:00')).toBeInTheDocument();
    expect(screen.getByText('2026-01-01 16:06:00')).toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: 'admin.ingestionTasks.viewLog' }),
    ).toBeInTheDocument();
  });

  it('toggles the create and update time sort direction', async () => {
    renderPage();

    fireEvent.click(
      await screen.findByRole('button', {
        name: 'admin.ingestionTasks.createAt',
      }),
    );

    await waitFor(() =>
      expect(jest.mocked(listIngestionTasks)).toHaveBeenLastCalledWith(
        expect.objectContaining({
          order_by: 'create_time',
          desc: false,
          page: 1,
        }),
      ),
    );

    fireEvent.click(
      screen.getByRole('button', { name: 'admin.ingestionTasks.updateAt' }),
    );

    await waitFor(() =>
      expect(jest.mocked(listIngestionTasks)).toHaveBeenLastCalledWith(
        expect.objectContaining({
          order_by: 'update_time',
          desc: true,
          page: 1,
        }),
      ),
    );
  });

  it('opens the run log with its details and events', async () => {
    jest.mocked(getIngestionLog).mockResolvedValue({
      data: {
        code: 0,
        message: 'success',
        data: {
          id: 'log-1',
          document_id: 'doc-1',
          run_count: 1,
          name: 'report.pdf',
          suffix: 'pdf',
          nickname: 'Alice',
          create_date: '2026-01-01T16:00:00+08:00',
          size: 16384,
          chunk_count: 13,
          process_begin_at: '2026-01-01T16:00:10+08:00',
          process_duration: 1.196,
          operation_status: '3',
        },
      },
    } as never);
    jest.mocked(listIngestionLogMessages).mockResolvedValue({
      data: {
        code: 0,
        message: 'success',
        data: {
          run_count: 1,
          items: [
            {
              id: 1,
              ts: '2026-01-01T16:00:10+08:00',
              event_type: 1,
              component: 'Parser',
              phase: 2,
              message: 'Parsing document',
            },
          ],
          oldest_id: 1,
          newest_id: 1,
          has_more_before: false,
          has_more_after: false,
          terminal: true,
        },
      },
    } as never);

    renderPage();

    fireEvent.click(
      await screen.findByRole('button', {
        name: 'admin.ingestionTasks.viewLog',
      }),
    );

    expect(
      await screen.findByText('admin.ingestionTasks.logTitle'),
    ).toBeInTheDocument();
    expect(await screen.findByText('Parsing document')).toBeInTheDocument();
    expect(jest.mocked(getIngestionLog)).toHaveBeenCalledWith('log-1');
    expect(jest.mocked(listIngestionLogMessages)).toHaveBeenCalledWith(
      'log-1',
      { limit: 200 },
    );
  });
});
