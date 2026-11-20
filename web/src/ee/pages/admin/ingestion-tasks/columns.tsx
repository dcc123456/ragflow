import {
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  File as LucideFile,
  ScrollText,
} from 'lucide-react';
import type { TFunction } from 'i18next';
import { createColumnHelper, type ColumnDef } from '@tanstack/react-table';

import FileStatusBadge from '@/components/file-status-badge';
import SvgIcon, { hasSvgIcon } from '@/components/svg-icon';
import { TruncatedText } from '@/components/truncated-text';
import { Button } from '@/components/ui/button';
import { RAGFlowTooltip } from '@/components/ui/tooltip';
import { IngestionTaskStatus } from '@/constants/knowledge';
import { ingestionStatusToRunningStatus } from '@/pages/dataset/dataset/utils';
import { formatDate } from '@/utils/date';
import { getExtension } from '@/utils/document-util';

export type IngestionTaskSortColumn = 'create_time' | 'update_time';

const columnHelper = createColumnHelper<AdminServiceEe.IngestionTaskItem>();

function DocumentTypeIcon({ name }: { name: string }) {
  const iconName = `file-icon/${getExtension(name)}`;

  return hasSvgIcon(iconName) ? (
    <SvgIcon name={iconName} width={20} />
  ) : (
    <LucideFile className="size-5 shrink-0 text-text-secondary" />
  );
}

function toRunningStatus(status: AdminServiceEe.IngestionTaskStatusName) {
  return ingestionStatusToRunningStatus(
    status as unknown as IngestionTaskStatus,
  );
}

function SortableHead({
  label,
  column,
  sortColumn,
  sortDesc,
  onSort,
}: {
  label: string;
  column: IngestionTaskSortColumn;
  sortColumn: IngestionTaskSortColumn;
  sortDesc: boolean;
  onSort: (column: IngestionTaskSortColumn) => void;
}) {
  const active = sortColumn === column;

  return (
    <Button
      variant="ghost"
      size="auto"
      className="-ml-2 gap-1 px-2 font-medium"
      onClick={() => onSort(column)}
    >
      {label}
      {!active ? (
        <ArrowUpDown className="size-3.5" />
      ) : sortDesc ? (
        <ArrowDown className="size-3.5" />
      ) : (
        <ArrowUp className="size-3.5" />
      )}
    </Button>
  );
}

export function getIngestionTaskColumns({
  t,
  sortColumn,
  sortDesc,
  onSort,
  onShowLog,
}: {
  t: TFunction;
  sortColumn: IngestionTaskSortColumn;
  sortDesc: boolean;
  onSort: (column: IngestionTaskSortColumn) => void;
  onShowLog: (logId: string | null) => void;
}): ColumnDef<AdminServiceEe.IngestionTaskItem, any>[] {
  return [
    columnHelper.accessor('document_name', {
      header: t('admin.ingestionTasks.document'),
      meta: { colClassName: 'w-[19%]' },
      cell: ({ getValue }) => {
        const name = getValue();
        return (
          <div className="flex min-w-0 items-center gap-2">
            <DocumentTypeIcon name={name} />
            <TruncatedText
              className="min-w-0 truncate font-medium"
              tooltip={name}
            >
              {name || '-'}
            </TruncatedText>
          </div>
        );
      },
    }),
    columnHelper.accessor('dataset_name', {
      header: t('admin.ingestionTasks.dataset'),
      meta: { colClassName: 'w-[14%]' },
      cell: ({ getValue }) => (
        <TruncatedText className="truncate" tooltip={getValue()}>
          {getValue() || '-'}
        </TruncatedText>
      ),
    }),
    columnHelper.accessor((row) => row.nickname || row.email, {
      id: 'user',
      header: t('admin.ingestionTasks.user'),
      meta: { colClassName: 'w-[16%]' },
      cell: ({ row }) => (
        <div className="min-w-0">
          <TruncatedText className="truncate" tooltip={row.original.email}>
            {row.original.nickname || row.original.email || '-'}
          </TruncatedText>
          {row.original.nickname ? (
            <TruncatedText
              className="truncate text-xs text-text-secondary"
              tooltip={row.original.email}
            >
              {row.original.email}
            </TruncatedText>
          ) : null}
        </div>
      ),
    }),
    columnHelper.accessor('pipeline_name', {
      header: t('admin.ingestionTasks.ingestion'),
      meta: { colClassName: 'w-[12%]' },
      cell: ({ getValue }) => (
        <TruncatedText className="truncate" tooltip={getValue()}>
          {getValue() || '-'}
        </TruncatedText>
      ),
    }),
    columnHelper.accessor('create_date', {
      id: 'create_time',
      header: () => (
        <SortableHead
          label={t('admin.ingestionTasks.createAt')}
          column="create_time"
          sortColumn={sortColumn}
          sortDesc={sortDesc}
          onSort={onSort}
        />
      ),
      meta: { colClassName: 'w-[12%]', cellClassName: 'whitespace-nowrap' },
      cell: ({ getValue }) => formatDate(getValue()) || '-',
    }),
    columnHelper.accessor('update_date', {
      id: 'update_time',
      header: () => (
        <SortableHead
          label={t('admin.ingestionTasks.updateAt')}
          column="update_time"
          sortColumn={sortColumn}
          sortDesc={sortDesc}
          onSort={onSort}
        />
      ),
      meta: { colClassName: 'w-[12%]', cellClassName: 'whitespace-nowrap' },
      cell: ({ getValue }) => formatDate(getValue()) || '-',
    }),
    columnHelper.accessor('status', {
      header: t('admin.ingestionTasks.statusLabel'),
      meta: { colClassName: 'w-[8%]' },
      cell: ({ getValue }) => (
        <FileStatusBadge
          className="!w-auto"
          status={toRunningStatus(getValue())}
          name={t(`admin.ingestionTasks.status.${getValue()}`)}
        />
      ),
    }),
    columnHelper.display({
      id: 'actions',
      header: t('admin.ingestionTasks.action'),
      meta: { colClassName: 'w-[7%]' },
      cell: ({ row }) => (
        <RAGFlowTooltip asChild tooltip={t('admin.ingestionTasks.viewLog')}>
          <Button
            variant="ghost"
            size="icon-xs"
            disabled={!row.original.pipeline_log_id}
            aria-label={t('admin.ingestionTasks.viewLog')}
            onClick={() => onShowLog(row.original.pipeline_log_id)}
          >
            <ScrollText className="size-4" />
          </Button>
        </RAGFlowTooltip>
      ),
    }),
  ];
}
