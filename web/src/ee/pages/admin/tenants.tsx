import { useState } from 'react';
import type { ChangeEvent } from 'react';
import { useDebounceFn } from 'ahooks';
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query';
import { LucideDot, Search } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';

import Spotlight from '@/components/spotlight';
import { TableEmpty, TableSkeleton } from '@/components/table-skeleton';
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { RAGFlowPagination } from '@/components/ui/ragflow-pagination';
import { ScrollArea } from '@/components/ui/scroll-area';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import {
  listAdminTenants,
  updateAdminTenantRole,
  updateAdminTenantStatus,
} from '@/ee/services/admin-tenant-service';
import type { AdminTenantListParams } from '@/ee/services/admin-tenant-service';
import { useRoleList } from '@/ee/pages/admin/hooks/useRole';

const AdminTenantKeys = {
  all: () => ['admin', 'tenants'] as const,
  list: (params: AdminTenantListParams) =>
    [...AdminTenantKeys.all(), params] as const,
};

function AdminTenants() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { roleList, isFetching: isRoleListFetching } = useRoleList();
  const [keyword, setKeyword] = useState('');
  const [searchKeyword, setSearchKeyword] = useState('');
  const [pagination, setPagination] = useState({ page: 1, pageSize: 10 });

  const { run: applyKeyword } = useDebounceFn(
    (value: string) => {
      setSearchKeyword(value);
      setPagination((current) => ({ ...current, page: 1 }));
    },
    { wait: 300 },
  );

  const handleKeywordChange = (event: ChangeEvent<HTMLInputElement>) => {
    const value = event.target.value;
    setKeyword(value);
    applyKeyword(value);
  };

  const params = {
    keyword: searchKeyword || undefined,
    page: pagination.page,
    page_size: pagination.pageSize,
  };

  const {
    data: tenantList,
    error: tenantListError,
    isError,
    isPending,
  } = useQuery({
    queryKey: AdminTenantKeys.list(params),
    queryFn: async () => {
      const { data } = await listAdminTenants(params);
      if (data.code !== 0) {
        throw new Error(data.message || t('admin.tenantsLoadFailed'));
      }
      return data.data;
    },
    placeholderData: keepPreviousData,
    retry: false,
  });

  const updateRole = useMutation({
    mutationFn: async ({
      tenantId,
      roleId,
    }: {
      tenantId: string;
      roleId: number;
    }) => {
      const { data } = await updateAdminTenantRole(tenantId, roleId);
      if (data.code !== 0) {
        throw new Error(data.message || t('admin.tenantRoleUpdateFailed'));
      }
      return data.data;
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: AdminTenantKeys.all() });
      toast.success(t('admin.tenantRoleUpdated'));
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : t('admin.tenantRoleUpdateFailed'),
      );
    },
  });

  const updateStatus = useMutation({
    mutationFn: async ({
      tenantId,
      status,
    }: {
      tenantId: string;
      status: string;
    }) => {
      const { data } = await updateAdminTenantStatus(tenantId, status);
      if (data.code !== 0) {
        throw new Error(data.message || t('admin.tenantStatusUpdateFailed'));
      }
      return data.data;
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: AdminTenantKeys.all() });
      toast.success(t('admin.tenantStatusUpdated'));
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : t('admin.tenantStatusUpdateFailed'),
      );
    },
  });

  const handleStatusChange = (tenantId: string) => (status: string) => {
    updateStatus.mutate({ tenantId, status });
  };

  const tenants = tenantList?.tenants ?? [];

  return (
    <Card className="!shadow-none relative h-full bg-transparent overflow-hidden">
      <Spotlight />
      <ScrollArea className="size-full">
        <CardHeader className="space-y-0 flex flex-row justify-between items-center">
          <CardTitle>{t('admin.tenants')}</CardTitle>

          <div className="ml-auto flex justify-end gap-4">
            <div className="relative w-56">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 text-text-secondary h-4 w-4" />
              <Input
                className="pl-10 h-10 bg-bg-input border-border-button"
                value={keyword}
                onChange={handleKeywordChange}
                placeholder={t('admin.searchTenants')}
                aria-label={t('admin.searchTenants')}
              />
            </div>
          </div>
        </CardHeader>

        <CardContent>
          <Table className="table-fixed min-w-[64rem]">
            <colgroup>
              <col width="28%" />
              <col width="22%" />
              <col width="12%" />
              <col width="18%" />
              <col width="20%" />
            </colgroup>

            <TableHeader>
              <TableRow>
                <TableHead>{t('admin.tenantName')}</TableHead>
                <TableHead>{t('admin.tenantOwner')}</TableHead>
                <TableHead>{t('admin.tenantMemberCount')}</TableHead>
                <TableHead>{t('admin.status')}</TableHead>
                <TableHead>{t('admin.tenantRole')}</TableHead>
              </TableRow>
            </TableHeader>

            <TableBody>
              {isPending ? (
                <TableSkeleton columnsLength={5} />
              ) : isError ? (
                <TableRow>
                  <TableCell
                    colSpan={5}
                    className="h-24 text-center text-text-secondary"
                  >
                    {tenantListError instanceof Error
                      ? tenantListError.message
                      : t('admin.tenantsLoadFailed')}
                  </TableCell>
                </TableRow>
              ) : tenants.length ? (
                tenants.map((tenant) => {
                  const name = tenant.name?.trim() || tenant.tenant_id;
                  const owner =
                    tenant.owner_email || tenant.owner_nickname || '—';
                  return (
                    <TableRow key={tenant.tenant_id}>
                      <TableCell className="min-w-0">
                        <div className="truncate font-medium" title={name}>
                          {name}
                        </div>
                        <div
                          className="truncate text-xs text-text-secondary"
                          title={tenant.tenant_id}
                        >
                          {tenant.tenant_id}
                        </div>
                      </TableCell>
                      <TableCell className="min-w-0">
                        <div className="truncate" title={owner}>
                          {owner}
                        </div>
                      </TableCell>
                      <TableCell>
                        {(tenant.member_count ?? 0).toLocaleString()}
                      </TableCell>
                      <TableCell>
                        <Select
                          disabled={updateStatus.isPending}
                          value={tenant.status === '0' ? '0' : '1'}
                          onValueChange={handleStatusChange(tenant.tenant_id)}
                        >
                          <SelectTrigger className="w-32 max-w-full">
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="0">
                              <div className="flex items-center">
                                <LucideDot className="size-[1em] stroke-[8] mr-1" />
                                {t('admin.inactive')}
                              </div>
                            </SelectItem>
                            <SelectItem value="1">
                              <div className="flex items-center text-state-success">
                                <LucideDot className="size-[1em] stroke-[8] mr-1" />
                                {t('admin.active')}
                              </div>
                            </SelectItem>
                          </SelectContent>
                        </Select>
                      </TableCell>
                      <TableCell>
                        <Select
                          disabled={isRoleListFetching || updateRole.isPending}
                          value={
                            tenant.role_id ? String(tenant.role_id) : undefined
                          }
                          onValueChange={(value) =>
                            updateRole.mutate({
                              tenantId: tenant.tenant_id,
                              roleId: Number.parseInt(value, 10),
                            })
                          }
                        >
                          <SelectTrigger className="w-40 max-w-full">
                            <SelectValue
                              className="truncate"
                              placeholder={t('common.pleaseSelect')}
                            />
                          </SelectTrigger>
                          <SelectContent>
                            {roleList?.map((role) => (
                              <SelectItem key={role.id} value={String(role.id)}>
                                {role.role_name}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      </TableCell>
                    </TableRow>
                  );
                })
              ) : (
                <TableEmpty columnsLength={5} />
              )}
            </TableBody>
          </Table>
        </CardContent>

        <CardFooter className="flex items-center justify-end">
          <RAGFlowPagination
            total={tenantList?.total ?? 0}
            current={pagination.page}
            pageSize={pagination.pageSize}
            onChange={(page, pageSize) => setPagination({ page, pageSize })}
          />
        </CardFooter>
      </ScrollArea>
    </Card>
  );
}

export default AdminTenants;
