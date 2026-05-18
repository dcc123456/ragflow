import { Permission } from '@/constants/team';
import {
  useFetchTenantInfo,
  useFetchUserInfo,
  useListTenantUser,
} from '@/hooks/use-user-setting-request';
import { cn } from '@/lib/utils';
import { PropsWithChildren, useMemo } from 'react';
import { Badge } from '../ui/badge';

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

  if (permission === Permission.Owner) {
    return <>{children}</>;
  }

  const isCurrentUser = userInfo?.nickname === nextName;

  return (
    <div className="relative w-[96%]">
      <Badge
        className={cn(
          'absolute -top-2 -right-2 z-10',
          isCurrentUser ? 'bg-bg-card' : 'bg-bg-accent',
        )}
      >
        {nextName}
      </Badge>
      {children}
    </div>
  );
}
