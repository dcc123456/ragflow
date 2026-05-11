import { Button } from '@/components/ui/button';
import { Table } from '@/components/ui/table';
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

  const columns = [
    {
      title: t('common.name'),
      dataIndex: 'nickname',
      key: 'nickname',
    },
    {
      title: t('setting.email'),
      dataIndex: 'email',
      key: 'email',
    },
    {
      title: t('common.action'),
      key: 'action',
      render: (_, { role, tenant_id }) => {
        if (role === TenantRole.Invite) {
          return (
            <div>
              <Button variant="link" onClick={handleAgree(tenant_id, true)}>
                {t(`setting.agree`)}
              </Button>
              <Button variant="link" onClick={handleAgree(tenant_id, false)}>
                {t(`setting.refuse`)}
              </Button>
            </div>
          );
        } else if (role === TenantRole.Normal && user.id !== tenant_id) {
          return (
            <Button
              variant="link"
              onClick={handleQuitTenantUser(user.id, tenant_id)}
            >
              {t('setting.quit')}
            </Button>
          );
        }
      },
    },
  ];

  return (
    <Table
      columns={columns}
      dataSource={list}
      rowKey={'tenant_id'}
      loading={loading}
      pagination={false}
    />
  );
};

export default TenantTable;
