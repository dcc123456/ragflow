import { TableEmpty, TableSkeleton } from '@/components/table-skeleton';
import { Button } from '@/components/ui/button';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { TenantRole } from '@/constants/team';
import {
  useFetchUserInfo,
  useListTenant,
} from '@/hooks/use-user-setting-request';
import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import {
  useHandleAgreeTenant,
  useHandleQuitUser,
} from './use-agree-invitation';

const TenantTable = () => {
  const { t } = useTranslation();
  const { data, loading } = useListTenant();
  const { handleAgree } = useHandleAgreeTenant();
  const { data: user } = useFetchUserInfo();
  const { handleQuitTenantUser } = useHandleQuitUser();
  const list = useMemo(() => {
    return data.filter((x) => x.role === TenantRole.Invite);
  }, [data]);

  const columnsLength = 3;

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('common.name')}</TableHead>
          <TableHead>{t('setting.email')}</TableHead>
          <TableHead>{t('common.action')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {loading ? (
          <TableSkeleton columnsLength={columnsLength} />
        ) : list.length === 0 ? (
          <TableEmpty columnsLength={columnsLength} />
        ) : (
          list.map(({ nickname, email, role, tenant_id }) => (
            <TableRow key={tenant_id}>
              <TableCell>{nickname}</TableCell>
              <TableCell>{email}</TableCell>
              <TableCell>
                {role === TenantRole.Invite ? (
                  <div>
                    <Button
                      variant="link"
                      onClick={handleAgree(tenant_id, true)}
                    >
                      {t('setting.agree')}
                    </Button>
                    <Button
                      variant="link"
                      onClick={handleAgree(tenant_id, false)}
                    >
                      {t('setting.refuse')}
                    </Button>
                  </div>
                ) : role === TenantRole.Normal && user.id !== tenant_id ? (
                  <Button
                    variant="link"
                    onClick={handleQuitTenantUser(user.id, tenant_id)}
                  >
                    {t('setting.quit')}
                  </Button>
                ) : null}
              </TableCell>
            </TableRow>
          ))
        )}
      </TableBody>
    </Table>
  );
};

export default TenantTable;
