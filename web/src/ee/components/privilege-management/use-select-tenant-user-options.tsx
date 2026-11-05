import { useListTenantUser } from '@/hooks/use-user-setting-request';
import { useMemo } from 'react';
import { SelectWithSearchOptionType } from '@/components/originui/select-with-search';
import { PrivilegeAvatar } from '@/ee/components/privilege-management/privilege-avatar';

export function useSelectTenantUserOptions(
  tenantId: string,
  excludePendingInvitations = false,
) {
  const { data: list } = useListTenantUser(tenantId, excludePendingInvitations);

  const options: SelectWithSearchOptionType[] = useMemo(() => {
    return list.map((x) => ({
      label: (
        <div className="flex items-center gap-2">
          <PrivilegeAvatar avatar={x.avatar}></PrivilegeAvatar> {x.nickname}
        </div>
      ),
      value: x.id,
    }));
  }, [list]);

  return options;
}
