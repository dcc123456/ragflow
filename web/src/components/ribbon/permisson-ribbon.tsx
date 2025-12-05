import { Permission } from '@/constants/team';
import {
  useFetchTenantInfo,
  useFetchUserInfo,
  useListTenantUser,
} from '@/hooks/use-user-setting-request';
import { cn } from '@/lib/utils';
import { Badge } from 'antd';
import { PropsWithChildren, useMemo } from 'react';

export function PermissionRibbon({
  children,
  name,
  permission,
  tenantId,
}: PropsWithChildren & {
  name?: string;
  permission: number;
  tenantId: string;
}) {
  const { data: userInfo } = useFetchUserInfo();
  const { data: tenantInfo } = useFetchTenantInfo();
  const { data: list } = useListTenantUser(tenantInfo.tenant_id);
  const nextName = useMemo(() => {
    return name || list.find((x) => x.user_id === tenantId)?.nickname;
  }, [list, name, tenantId]);

  return (
    <Badge.Ribbon
      text={nextName}
      color={userInfo?.nickname === nextName ? '#1677ff' : 'pink'}
      rootClassName="w-[96%]"
      className={cn('top-0', {
        hidden: permission === Permission.Owner,
      })}
    >
      {children}
    </Badge.Ribbon>
  );
}
