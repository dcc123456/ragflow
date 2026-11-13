import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';

import { useQuery } from '@tanstack/react-query';
import {
  createColumnHelper,
  flexRender,
  getCoreRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  useReactTable,
} from '@tanstack/react-table';

import { RAGFlowAvatar } from '@/components/ragflow-avatar';
import { CardContent } from '@/components/ui/card';
import { RAGFlowPagination } from '@/components/ui/ragflow-pagination';
import { ScrollArea } from '@/components/ui/scroll-area';
import {
  Table,
  TableBody,
  TableCell,
  // TableHead,
  // TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';

import { listUserAgents, listUserDatasets } from '@/ee/services/admin-service';

import { TableEmpty } from '@/components/table-skeleton';

const ASSET_NAMES = ['dataset', 'flow'];

const datasetColumnHelper =
  createColumnHelper<AdminServiceEe.ListUserDatasetItem>();
const agentColumnHelper =
  createColumnHelper<AdminServiceEe.ListUserAgentItem>();

function UserDatasetTable(props: {
  data?: AdminServiceEe.ListUserDatasetItem[];
}) {
  const { t } = useTranslation();

  const columnDefs = useMemo(
    () => [
      datasetColumnHelper.accessor('name', {
        header: t('admin.name'),
        cell: ({ row, cell }) => (
          <div className="flex items-center gap-2">
            <RAGFlowAvatar
              avatar={row.original.avatar}
              name={cell.getValue()}
            />
            <span>{cell.getValue()}</span>
          </div>
        ),
      }),
      // #region
      /*
      datasetColumnHelper.accessor('name', {
        header: t('admin.name'),
        enableSorting: false,
      }),
      datasetColumnHelper.accessor('status', {
        header: t('admin.status'),
        cell: ({ cell }) => {
          return (
            <Badge
              variant={parseBooleanish(cell.getValue()) ? 'success' : 'destructive'}
              className="pl-[.35em]"
            >
              <LucideDot className="size-[1em] stroke-[8] mr-1" />
              {t(
                parseBooleanish(cell.getValue())
                  ? 'admin.active'
                  : 'admin.inactive',
              )}"
            </Badge>
          );
        },
        enableSorting: false,
      }),
      datasetColumnHelper.accessor('chunk_num', {
        header: t('admin.chunkNum'),
      }),
      datasetColumnHelper.accessor('doc_num', {
        header: t('admin.docNum'),
      }),
      datasetColumnHelper.accessor('token_num', {
        header: t('admin.tokenNum'),
      }),
      datasetColumnHelper.accessor('language', {
        header: t('admin.language'),
        enableSorting: false,
      }),
      datasetColumnHelper.accessor('create_date', {
        header: t('admin.createDate'),
      }),
      datasetColumnHelper.accessor('update_date', {
        header: t('admin.updateDate'),
      }),
      datasetColumnHelper.accessor('permission', {
        header: t('admin.permission'),
        enableSorting: false,
      }),
      */
      // #endregion
    ],
    [t],
  );

  const table = useReactTable({
    data: props.data ?? [],
    columns: columnDefs,

    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getPaginationRowModel: getPaginationRowModel(),

    enableSorting: false,
  });

  return (
    <section className="space-y-4">
      <Table>
        {/* <TableHeader>
          {table.getHeaderGroups().map((headerGroup) => (
            <TableRow key={headerGroup.id}>
              {headerGroup.headers.map((header) => (
                <TableHead key={header.id}>
                  {header.isPlaceholder ? null : header.column.getCanSort() ? (
                    <Button
                      variant="ghost"
                      onClick={header.column.getToggleSortingHandler()}
                    >
                      {flexRender(
                        header.column.columnDef.header,
                        header.getContext(),
                      )}
                      {getSortIcon(header.column.getIsSorted())}
                    </Button>
                  ) : (
                    flexRender(
                      header.column.columnDef.header,
                      header.getContext(),
                    )
                  )}
                </TableHead>
              ))}
            </TableRow>
          ))}
        </TableHeader> */}

        <TableBody>
          {table.getRowModel().rows?.length ? (
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
            <TableEmpty columnsLength={columnDefs.length} />
          )}
        </TableBody>
      </Table>

      <RAGFlowPagination
        total={props.data?.length}
        current={table.getState().pagination.pageIndex + 1}
        pageSize={table.getState().pagination.pageSize}
        onChange={(page, pageSize) => {
          table.setPagination({
            pageIndex: page - 1,
            pageSize,
          });
        }}
      />
    </section>
  );
}

function UserAgentTable(props: { data?: AdminServiceEe.ListUserAgentItem[] }) {
  const { t } = useTranslation();

  const columnDefs = useMemo(
    () => [
      agentColumnHelper.accessor('title', {
        header: t('admin.agentTitle'),
        cell: ({ row, cell }) => (
          <div className="flex items-center gap-2">
            <RAGFlowAvatar
              avatar={row.original.avatar}
              name={cell.getValue()}
            />
            <span>{cell.getValue()}</span>
          </div>
        ),
      }),
      // #region
      /*
      agentColumnHelper.accessor('title', {
        header: t('admin.agentTitle'),
      }),
      agentColumnHelper.accessor('permission', {
        header: t('admin.permission'),
      }),
      agentColumnHelper.accessor('canvas_category', {
        header: t('admin.canvasCategory'),
      }),
      */
      // #endregion
    ],
    [t],
  );

  const table = useReactTable({
    data: props.data ?? [],
    columns: columnDefs,

    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getPaginationRowModel: getPaginationRowModel(),

    enableSorting: false,
  });

  return (
    <section className="space-y-4">
      <Table>
        {/* <TableHeader>
          {table.getHeaderGroups().map((headerGroup) => (
            <TableRow key={headerGroup.id}>
              {headerGroup.headers.map((header) => (
                <TableHead key={header.id}>
                  {header.isPlaceholder ? null : (
                    <>
                      {flexRender(
                        header.column.columnDef.header,
                        header.getContext(),
                      )}
                    </>
                  )}
                </TableHead>
              ))}
            </TableRow>
          ))}
        </TableHeader> */}

        <TableBody>
          {table.getRowModel().rows?.length ? (
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
            <TableEmpty columnsLength={columnDefs.length} />
          )}
        </TableBody>
      </Table>

      <RAGFlowPagination
        total={props.data?.length}
        current={table.getState().pagination.pageIndex + 1}
        pageSize={table.getState().pagination.pageSize}
        onChange={(page, pageSize) => {
          table.setPagination({
            pageIndex: page - 1,
            pageSize,
          });
        }}
      />
    </section>
  );
}

export default function UserDetailAssets(props: { userId?: string }) {
  const { t } = useTranslation();

  const { data: { datasets, agents } = {} } = useQuery({
    queryKey: ['admin/userAssets', props.userId],
    queryFn: async () => {
      const [userDatasets, userAgents] = await Promise.all([
        listUserDatasets(props.userId!),
        listUserAgents(props.userId!),
      ]);

      return {
        datasets: userDatasets.data.data,
        agents: userAgents.data.data,
      };
    },
    enabled: !!props.userId,
    retry: false,
  });

  return (
    <CardContent className="h-0 basis-0 grow pt-6">
      <Tabs className="h-full flex flex-col" defaultValue="dataset">
        <TabsList className="p-0 mb-2 gap-4 bg-transparent justify-start">
          {ASSET_NAMES.map((name) => (
            <TabsTrigger
              key={name}
              className="text-text-secondary border-0.5 border-border-button data-[state=active]:bg-bg-card"
              value={name}
            >
              {t(`header.${name}`)}
            </TabsTrigger>
          ))}
        </TabsList>

        <TabsContent value="dataset" className="h-0 basis-0 grow">
          <ScrollArea className="h-full">
            <UserDatasetTable data={datasets} />
          </ScrollArea>
        </TabsContent>

        <TabsContent value="flow" className="h-0 basis-0 grow">
          <ScrollArea className="h-full">
            <UserAgentTable data={agents} />
          </ScrollArea>
        </TabsContent>
      </Tabs>
    </CardContent>
  );
}
