import {
  ConfirmDeleteDialog,
  ConfirmDeleteDialogNode,
} from '@/components/confirm-delete-dialog';
import { TruncatedText } from '@/components/truncated-text';
import { TableEmpty, TableSkeleton } from '@/components/table-skeleton';
import { Button } from '@/components/ui/button';
import { Switch } from '@/components/ui/switch';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { useTranslation } from 'react-i18next';
import { Pencil, ShieldCheck, Trash2 } from 'lucide-react';
import { SearchInput } from '@/components/ui/input';
import { UserPlus } from 'lucide-react';

export type RoleTableRow = {
  id: number;
  role_name: string;
  description?: string | null;
  is_default: boolean;
  count: number;
};

export function RoleTableView({
  keyword,
  roles,
  loading,
  canManageRoles,
  settingDefault,
  deleting,
  countLabel,
  toolbar = false,
  onKeywordChange,
  onCreate,
  onSetDefault,
  onConfigure,
  onEdit,
  onDelete,
}: {
  keyword: string;
  roles: RoleTableRow[];
  loading: boolean;
  canManageRoles: boolean;
  settingDefault: boolean;
  deleting: boolean;
  countLabel: string;
  toolbar?: boolean;
  onKeywordChange?: (keyword: string) => void;
  onCreate?: () => void;
  onSetDefault: (roleId: number) => void;
  onConfigure: (role: RoleTableRow) => void;
  onEdit: (role: RoleTableRow) => void;
  onDelete: (role: RoleTableRow) => Promise<void> | void;
}) {
  const { t } = useTranslation();
  const normalizedKeyword = keyword.trim().toLowerCase();
  const visibleRoles = normalizedKeyword
    ? roles.filter(
        (role) =>
          role.role_name.toLowerCase().includes(normalizedKeyword) ||
          role.description?.toLowerCase().includes(normalizedKeyword),
      )
    : roles;

  return (
    <div className="flex min-w-0 flex-col gap-3">
      {toolbar && (
        <div className="flex items-center justify-end gap-2">
          <SearchInput
            value={keyword}
            onChange={(event) => onKeywordChange?.(event.target.value)}
            rootClassName="w-[200px]"
          />
          {canManageRoles && (
            <Button className="rounded-lg" onClick={onCreate}>
              <UserPlus />
              {t('permission.createRole')}
            </Button>
          )}
        </div>
      )}
      <Table rootClassName="max-h-[68vh]">
        <TableHeader>
          <TableRow>
            <TableHead>{t('admin.roleName')}</TableHead>
            <TableHead>{t('admin.description')}</TableHead>
            <TableHead className="w-[150px]">{countLabel}</TableHead>
            <TableHead className="w-[88px] text-center">
              {t('permission.defaultRole')}
            </TableHead>
            <TableHead className="w-[148px] text-right">
              {t('common.action')}
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {loading ? (
            <TableSkeleton columnsLength={5} />
          ) : visibleRoles.length > 0 ? (
            visibleRoles.map((role) => (
              <TableRow key={role.id} className="group">
                <TableCell className="max-w-[240px]">
                  <TruncatedText
                    as="span"
                    className="block truncate"
                    tooltip={role.role_name}
                  >
                    {role.role_name}
                  </TruncatedText>
                </TableCell>
                <TableCell className="max-w-[360px]">
                  <TruncatedText
                    as="span"
                    className="block truncate text-text-secondary"
                    tooltip={role.description || ''}
                  >
                    {role.description || '—'}
                  </TruncatedText>
                </TableCell>
                <TableCell>{role.count}</TableCell>
                <TableCell className="text-center">
                  <Switch
                    aria-label={
                      role.is_default
                        ? t('admin.defaultTenantRole')
                        : t('admin.setAsDefaultTenantRole')
                    }
                    checked={role.is_default}
                    disabled={
                      !canManageRoles || role.is_default || settingDefault
                    }
                    onCheckedChange={(checked) => {
                      if (checked) onSetDefault(role.id);
                    }}
                  />
                </TableCell>
                <TableCell>
                  <div className="flex items-center justify-end gap-1">
                    {canManageRoles && (
                      <>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          aria-label={t('permission.roleConfiguration')}
                          title={t('permission.roleConfiguration')}
                          onClick={() => onConfigure(role)}
                        >
                          <ShieldCheck />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          aria-label={t('permission.editRole')}
                          title={t('permission.editRole')}
                          onClick={() => onEdit(role)}
                        >
                          <Pencil />
                        </Button>
                        <ConfirmDeleteDialog
                          title={t('permission.deleteRole')}
                          okButtonText={t('permission.deleteRole')}
                          onOk={() => onDelete(role)}
                          content={{
                            node: (
                              <ConfirmDeleteDialogNode
                                name={role.role_name}
                                warnText={t('permission.deleteRoleWarning')}
                              />
                            ),
                          }}
                        >
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            disabled={role.is_default || deleting}
                            aria-label={t('permission.deleteRole')}
                            title={
                              role.is_default
                                ? t('admin.defaultTenantRole')
                                : t('permission.deleteRole')
                            }
                          >
                            <Trash2 />
                          </Button>
                        </ConfirmDeleteDialog>
                      </>
                    )}
                  </div>
                </TableCell>
              </TableRow>
            ))
          ) : (
            <TableEmpty columnsLength={5} />
          )}
        </TableBody>
      </Table>
    </div>
  );
}
