'use client';

import { SelectWithSearch } from '@/components/originui/select-with-search';
import { PrivilegeAvatar } from '@/components/privilege-management/privilege-avatar';
import { Badge } from '@/components/ui/badge';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Permission } from '@/constants/team';
import { useTranslation } from 'react-i18next';
import { IKnowledgePermission } from './interface';

interface PermissionTableProps {
  data: IKnowledgePermission[];
  onPermissionChange: (kbId: string, permission: number) => void;
}

export function PermissionTable({
  data,
  onPermissionChange,
}: PermissionTableProps) {
  const { t } = useTranslation();

  const getPermissionOptions = () => [
    { value: String(Permission.Manage), label: t('permission.manage') },
    { value: String(Permission.Write), label: t('permission.write') },
    { value: String(Permission.Read), label: t('permission.read') },
    { value: '0', label: t('permission.invisible') },
  ];

  return (
    <div className="w-full">
      <div className="rounded-md border">
        <Table rootClassName="max-h-[50vh]">
          <TableHeader>
            <TableRow>
              <TableHead className="w-[60%]">{t('common.name')}</TableHead>
              <TableHead className="w-[40%] text-right">
                {t('permission.permission')}
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.length === 0 ? (
              <TableRow>
                <TableCell
                  colSpan={2}
                  className="h-24 text-center text-muted-foreground"
                >
                  {t('common.noData')}
                </TableCell>
              </TableRow>
            ) : (
              data.map((item) => (
                <TableRow key={item.kb_id}>
                  <TableCell>
                    <div className="flex items-center gap-3">
                      <PrivilegeAvatar
                        avatar={item.avatar}
                        className="size-8"
                      />
                      <span className="font-medium truncate">{item.name}</span>
                      {item.module_type && (
                        <Badge variant="secondary" className="text-xs">
                          {item.module_type}
                        </Badge>
                      )}
                    </div>
                  </TableCell>
                  <TableCell className="text-right">
                    <SelectWithSearch
                      value={String(item.permission)}
                      options={getPermissionOptions()}
                      onChange={(value) =>
                        onPermissionChange(item.kb_id, Number(value))
                      }
                      triggerClassName="w-[140px]"
                    />
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}
