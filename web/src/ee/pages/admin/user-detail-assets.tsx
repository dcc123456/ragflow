import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';

import { keepPreviousData, useQuery } from '@tanstack/react-query';
import {
  ColumnDef,
  createColumnHelper,
  flexRender,
  getCoreRowModel,
  useReactTable,
} from '@tanstack/react-table';

import { RAGFlowAvatar } from '@/components/ragflow-avatar';
import { TableEmpty, TableSkeleton } from '@/components/table-skeleton';
import { TruncatedText } from '@/components/truncated-text';
import { CardContent } from '@/components/ui/card';
import { RAGFlowPagination } from '@/components/ui/ragflow-pagination';
import { ScrollArea } from '@/components/ui/scroll-area';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { listUserAgents, listUserDatasets } from '@/ee/services/admin-service';
import { formatFileSize } from '@/utils/common-util';
import { formatIsoDateTime } from '@/utils/date';

type AssetPagination = { page: number; pageSize: number };
type UserAssetPage<T> = { items: T[]; total: number };
type IdentifiedAsset = { id: string };

const DefaultPagination: AssetPagination = { page: 1, pageSize: 10 };
const AssetTabs = [
  { value: 'dataset', labelKey: 'header.dataset' },
  { value: 'workflow', labelKey: 'admin.workflow' },
  { value: 'pipeline', labelKey: 'admin.pipeline' },
] as const;

const UserAssetKeys = {
  dataset: (userId: string, pagination: AssetPagination) =>
    [
      'admin/userAssets',
      'dataset',
      userId,
      pagination.page,
      pagination.pageSize,
    ] as const,
  canvas: (
    userId: string,
    category: AdminServiceEe.CanvasCategory,
    pagination: AssetPagination,
  ) =>
    [
      'admin/userAssets',
      category,
      userId,
      pagination.page,
      pagination.pageSize,
    ] as const,
};

const datasetColumnHelper =
  createColumnHelper<AdminServiceEe.ListUserDatasetItem>();
const canvasColumnHelper =
  createColumnHelper<AdminServiceEe.ListUserAgentItem>();

function AssetText({ value }: { value: string }) {
  return (
    <TruncatedText as="span" className="block min-w-0 truncate" tooltip={value}>
      {value}
    </TruncatedText>
  );
}

function formatAssetDate(value?: string) {
  return value ? formatIsoDateTime(value) : '-';
}

function UserAssetTable<T extends IdentifiedAsset>({
  columns,
  data,
  total,
  pagination,
  isLoading,
  isError,
  minTableWidth,
  onPaginationChange,
}: {
  columns: ColumnDef<T, any>[];
  data: T[];
  total: number;
  pagination: AssetPagination;
  isLoading: boolean;
  isError: boolean;
  minTableWidth: string;
  onPaginationChange: (page: number, pageSize: number) => void;
}) {
  const { t } = useTranslation();
  const table = useReactTable({
    data,
    columns,
    getRowId: (row) => row.id,
    getCoreRowModel: getCoreRowModel(),
    enableSorting: false,
  });

  return (
    <section className="space-y-4">
      <Table className={`table-fixed ${minTableWidth}`}>
        <colgroup>
          {table.getVisibleLeafColumns().map((column) => (
            <col
              key={column.id}
              className={column.columnDef.meta?.colClassName}
            />
          ))}
        </colgroup>
        <TableHeader>
          {table.getHeaderGroups().map((headerGroup) => (
            <TableRow key={headerGroup.id}>
              {headerGroup.headers.map((header) => (
                <TableHead key={header.id}>
                  <TruncatedText
                    as="span"
                    className="block truncate"
                    tooltip={
                      typeof header.column.columnDef.header === 'string'
                        ? header.column.columnDef.header
                        : undefined
                    }
                  >
                    {header.isPlaceholder
                      ? null
                      : flexRender(
                          header.column.columnDef.header,
                          header.getContext(),
                        )}
                  </TruncatedText>
                </TableHead>
              ))}
            </TableRow>
          ))}
        </TableHeader>
        <TableBody>
          {isLoading ? (
            <TableSkeleton columnsLength={columns.length} />
          ) : isError ? (
            <TableRow>
              <TableCell
                colSpan={columns.length}
                className="h-24 text-center text-text-secondary"
              >
                {t('admin.assetLoadFailed')}
              </TableCell>
            </TableRow>
          ) : table.getRowModel().rows.length ? (
            table.getRowModel().rows.map((row) => (
              <TableRow key={row.id}>
                {row.getVisibleCells().map((cell) => (
                  <TableCell key={cell.id}>
                    {flexRender(cell.column.columnDef.cell, cell.getContext())}
                  </TableCell>
                ))}
              </TableRow>
            ))
          ) : (
            <TableEmpty columnsLength={columns.length} />
          )}
        </TableBody>
      </Table>

      {isLoading || isError ? null : (
        <RAGFlowPagination
          total={total}
          current={pagination.page}
          pageSize={pagination.pageSize}
          onChange={onPaginationChange}
        />
      )}
    </section>
  );
}

function UserDatasetAssets({ userId }: { userId: string }) {
  const { t } = useTranslation();
  const [pagination, setPagination] = useState(DefaultPagination);
  const columns = useMemo<ColumnDef<AdminServiceEe.ListUserDatasetItem, any>[]>(
    () => [
      datasetColumnHelper.accessor('name', {
        header: t('admin.name'),
        meta: { colClassName: 'w-[20%]' },
        cell: ({ row }) => (
          <div className="flex min-w-0 items-center gap-2">
            <RAGFlowAvatar
              avatar={row.original.avatar ?? undefined}
              name={row.original.name}
            />
            <AssetText value={row.original.name} />
          </div>
        ),
      }),
      datasetColumnHelper.accessor('tenant_name', {
        header: t('admin.assetColumns.team'),
        meta: { colClassName: 'w-[14%]' },
        cell: ({ cell }) => <AssetText value={cell.getValue()} />,
      }),
      datasetColumnHelper.accessor('doc_num', {
        header: t('admin.assetColumns.documentNum'),
        meta: { colClassName: 'w-[10%]' },
      }),
      datasetColumnHelper.accessor('file_size', {
        header: t('admin.assetColumns.fileSize'),
        meta: { colClassName: 'w-[10%]' },
        cell: ({ cell }) => formatFileSize(cell.getValue(), false),
      }),
      datasetColumnHelper.accessor('chunk_num', {
        header: t('admin.assetColumns.chunkNum'),
        meta: { colClassName: 'w-[10%]' },
      }),
      datasetColumnHelper.accessor('create_date', {
        header: t('admin.assetColumns.createAt'),
        meta: { colClassName: 'w-[18%]' },
        cell: ({ cell }) => (
          <AssetText value={formatAssetDate(cell.getValue())} />
        ),
      }),
      datasetColumnHelper.accessor('update_date', {
        header: t('admin.assetColumns.lastSaveAt'),
        meta: { colClassName: 'w-[18%]' },
        cell: ({ cell }) => (
          <AssetText value={formatAssetDate(cell.getValue())} />
        ),
      }),
    ],
    [t],
  );

  const { data, isLoading, isError } = useQuery({
    queryKey: UserAssetKeys.dataset(userId, pagination),
    queryFn: async (): Promise<
      UserAssetPage<AdminServiceEe.ListUserDatasetItem>
    > => {
      const response = await listUserDatasets(userId, {
        page: pagination.page,
        page_size: pagination.pageSize,
      });
      return {
        items: response.data.data,
        total: response.data.total ?? 0,
      };
    },
    placeholderData: keepPreviousData,
    retry: false,
  });

  const handlePaginationChange = (page: number, pageSize: number) => {
    setPagination({ page, pageSize });
  };

  return (
    <UserAssetTable
      columns={columns}
      data={data?.items ?? []}
      total={data?.total ?? 0}
      pagination={pagination}
      isLoading={isLoading}
      isError={isError}
      minTableWidth="min-w-[1200px]"
      onPaginationChange={handlePaginationChange}
    />
  );
}

function UserCanvasAssets({
  userId,
  category,
}: {
  userId: string;
  category: AdminServiceEe.CanvasCategory;
}) {
  const { t } = useTranslation();
  const [pagination, setPagination] = useState(DefaultPagination);
  const columns = useMemo<ColumnDef<AdminServiceEe.ListUserAgentItem, any>[]>(
    () => [
      canvasColumnHelper.accessor('title', {
        header: t('admin.name'),
        meta: { colClassName: 'w-[28%]' },
        cell: ({ row }) => (
          <div className="flex min-w-0 items-center gap-2">
            <RAGFlowAvatar
              avatar={row.original.avatar ?? undefined}
              name={row.original.title}
            />
            <AssetText value={row.original.title} />
          </div>
        ),
      }),
      canvasColumnHelper.accessor('tenant_name', {
        header: t('admin.assetColumns.team'),
        meta: { colClassName: 'w-[20%]' },
        cell: ({ cell }) => <AssetText value={cell.getValue()} />,
      }),
      canvasColumnHelper.accessor('create_date', {
        header: t('admin.assetColumns.createAt'),
        meta: { colClassName: 'w-[26%]' },
        cell: ({ cell }) => (
          <AssetText value={formatAssetDate(cell.getValue())} />
        ),
      }),
      canvasColumnHelper.accessor('update_date', {
        header: t('admin.assetColumns.lastSaveAt'),
        meta: { colClassName: 'w-[26%]' },
        cell: ({ cell }) => (
          <AssetText value={formatAssetDate(cell.getValue())} />
        ),
      }),
    ],
    [t],
  );

  const { data, isLoading, isError } = useQuery({
    queryKey: UserAssetKeys.canvas(userId, category, pagination),
    queryFn: async (): Promise<
      UserAssetPage<AdminServiceEe.ListUserAgentItem>
    > => {
      const response = await listUserAgents(userId, {
        canvas_category: category,
        page: pagination.page,
        page_size: pagination.pageSize,
      });
      return {
        items: response.data.data,
        total: response.data.total ?? 0,
      };
    },
    placeholderData: keepPreviousData,
    retry: false,
  });

  const handlePaginationChange = (page: number, pageSize: number) => {
    setPagination({ page, pageSize });
  };

  return (
    <UserAssetTable
      columns={columns}
      data={data?.items ?? []}
      total={data?.total ?? 0}
      pagination={pagination}
      isLoading={isLoading}
      isError={isError}
      minTableWidth="min-w-[760px]"
      onPaginationChange={handlePaginationChange}
    />
  );
}

export default function UserDetailAssets({ userId }: { userId?: string }) {
  const { t } = useTranslation();

  if (!userId) {
    return null;
  }

  return (
    <CardContent className="h-0 basis-0 grow pt-6">
      <Tabs className="h-full flex flex-col" defaultValue="dataset">
        <TabsList className="p-0 mb-2 gap-4 bg-transparent justify-start">
          {AssetTabs.map(({ value, labelKey }) => (
            <TabsTrigger
              key={value}
              className="text-text-secondary border-0.5 border-border-button data-[state=active]:bg-bg-card"
              value={value}
            >
              {t(labelKey)}
            </TabsTrigger>
          ))}
        </TabsList>

        <TabsContent value="dataset" className="h-0 basis-0 grow">
          <ScrollArea className="h-full">
            <UserDatasetAssets userId={userId} />
          </ScrollArea>
        </TabsContent>
        <TabsContent value="workflow" className="h-0 basis-0 grow">
          <ScrollArea className="h-full">
            <UserCanvasAssets userId={userId} category="agent_canvas" />
          </ScrollArea>
        </TabsContent>
        <TabsContent value="pipeline" className="h-0 basis-0 grow">
          <ScrollArea className="h-full">
            <UserCanvasAssets userId={userId} category="dataflow_canvas" />
          </ScrollArea>
        </TabsContent>
      </Tabs>
    </CardContent>
  );
}
