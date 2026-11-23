import {
  createTable,
  flexRender,
  getCoreRowModel,
} from '@tanstack/react-table';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen } from '@testing-library/react';
import React from 'react';

jest.mock('i18next', () => ({ t: (key: string) => key }));

jest.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

jest.mock('@/hooks/common-hooks', () => ({
  useTranslate: () => ({ t: (key: string) => key }),
}));

jest.mock('@/services/knowledge-service', () => ({
  listIngestionMessages: jest.fn(),
}));

jest.mock('@/ee/services/data-source-service', () => ({
  listSyncLogEvents: jest.fn(),
}));

import { TooltipProvider } from '@/components/ui/tooltip';
import { RunningStatus } from '@/constants/knowledge';
import { listSyncLogEvents } from '@/ee/services/data-source-service';
import { getDataSourceLogsTableColumns } from '@/pages/user-setting/data-source/log-columns';
import { SyncLogModal } from './sync-log-modal';

const Logs = [
  {
    connector_id: 'connector',
    error_count: 0,
    error_msg: '',
    id: 'task-1',
    kb_id: 'kb-1',
    kb_name: 'Dataset',
    new_docs_indexed: 0,
    status: RunningStatus.RUNNING,
    update_date: '',
  },
];

function LogAction() {
  const table = createTable({
    columns: getDataSourceLogsTableColumns({ now: 0 }),
    data: Logs,
    state: {},
    onStateChange: () => {},
    renderFallbackValue: null,
    getCoreRowModel: getCoreRowModel(),
  });
  const cell = table
    .getRowModel()
    .rows[0].getAllCells()
    .find((candidate) => candidate.column.id === 'operations');
  return cell
    ? flexRender(cell.column.columnDef.cell, cell.getContext())
    : null;
}

const mockEvents = jest.mocked(listSyncLogEvents);

function renderLog(showAction = false) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    React.createElement(
      QueryClientProvider,
      { client },
      React.createElement(
        TooltipProvider,
        null,
        showAction
          ? React.createElement(LogAction)
          : React.createElement(SyncLogModal, {
              log: Logs[0],
              onClose: jest.fn(),
            }),
      ),
    ),
  );
}

function eventResponse(
  id?: number,
  message?: string,
  hasMoreBefore = false,
  terminal = false,
) {
  return {
    data: {
      data: {
        items:
          id == null
            ? []
            : [
                {
                  id,
                  ts: '2026-09-18T10:20:30Z',
                  event_type: 0,
                  component: 'sync',
                  phase: 0,
                  message,
                },
              ],
        oldest_id: id,
        newest_id: id,
        has_more_before: hasMoreBefore,
        has_more_after: false,
        terminal,
      },
    },
  } as never;
}

afterEach(() => {
  jest.resetAllMocks();
  jest.useRealTimers();
});

it('makes one final poll to collect a late event after the run becomes terminal', async () => {
  jest.useFakeTimers();
  mockEvents
    .mockResolvedValueOnce(eventResponse(1, 'Sync started'))
    .mockResolvedValueOnce(eventResponse(undefined, undefined, false, true))
    .mockResolvedValueOnce(eventResponse(2, 'Sync completed', false, true));
  renderLog();
  expect(await screen.findByText('Sync started')).toBeInTheDocument();

  await act(async () => {
    jest.advanceTimersByTime(5000);
  });
  // Let React Query notify its subscriber of the terminal response.
  await act(async () => {
    jest.advanceTimersByTime(1);
  });
  await act(async () => {
    jest.advanceTimersByTime(5000);
  });
  expect(await screen.findByText('Sync completed')).toBeInTheDocument();
  expect(mockEvents).toHaveBeenNthCalledWith(3, 'task-1', {
    limit: 200,
    after_id: 1,
  });
  await act(async () => {
    jest.advanceTimersByTime(10000);
  });
  expect(mockEvents).toHaveBeenCalledTimes(3);
});

it('keeps the existing summary visible if a run has no event history', async () => {
  mockEvents.mockResolvedValue(eventResponse());
  renderLog();
  expect(await screen.findByText('Sync in progress')).toBeInTheDocument();
  expect(mockEvents).toHaveBeenCalledWith('task-1', { limit: 200 });
});

it('loads older events when scrolled to the top and polls after the newest event', async () => {
  jest.useFakeTimers();
  mockEvents
    .mockResolvedValueOnce(eventResponse(2, 'Fetched files', true))
    .mockResolvedValueOnce(eventResponse(1, 'Sync started'))
    .mockResolvedValueOnce(eventResponse(3, 'Queued ingestion'));
  renderLog();
  const latest = await screen.findByText('Fetched files');
  expect(screen.getByText('Sync in progress')).toBeInTheDocument();

  fireEvent.scroll(latest.closest('.overflow-y-auto')!);
  expect(await screen.findByText('Sync started')).toBeInTheDocument();
  expect(mockEvents).toHaveBeenNthCalledWith(2, 'task-1', {
    limit: 200,
    before_id: 2,
  });

  await act(async () => {
    jest.advanceTimersByTime(5000);
  });
  expect(await screen.findByText('Queued ingestion')).toBeInTheDocument();
  expect(mockEvents).toHaveBeenNthCalledWith(3, 'task-1', {
    limit: 200,
    after_id: 2,
  });
});

it('opens the log from the table action and starts loading only on demand', async () => {
  mockEvents.mockResolvedValue(eventResponse(1, 'Fetched files'));
  renderLog(true);
  expect(mockEvents).not.toHaveBeenCalled();
  fireEvent.click(
    screen.getByRole('button', { name: 'knowledgeDetails.syncLog' }),
  );
  expect(await screen.findByText('Fetched files')).toBeInTheDocument();
});
