import { TenantRole, TeamRole } from '@/ee/constants/team';
import { ITenantGroup } from '@/ee/interfaces/database/team';
import {
  useFetchTenantInfo,
  useListTenantUser,
} from '@/hooks/use-user-setting-request';
import { uniqBy } from 'lodash';
import { listTenantGroups } from '@/ee/services/group-service';
import { useQuery } from '@tanstack/react-query';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { CollaboratorItem } from '@/ee/components/privilege-management/interface';

export function useSelectCollaborator() {
  const [list, setList] = useState<CollaboratorItem[]>([]);

  const handleClick = useCallback((id: string, value?: boolean) => {
    setList((current) =>
      current.map((item) =>
        item.id === id
          ? {
              ...item,
              checked: typeof value === 'boolean' ? value : !item.checked,
            }
          : item,
      ),
    );
  }, []);

  const selectAll = useCallback((checked: boolean, ids?: string[]) => {
    const selectedIds = ids ? new Set(ids) : undefined;
    setList((pre) => {
      return pre.map((item) =>
        !selectedIds || selectedIds.has(item.id) ? { ...item, checked } : item,
      );
    });
  }, []);

  return { list, setList, handleClick, selectAll };
}

export function useOperateMember(tenantId: string) {
  const { handleClick, list, setList, selectAll } = useSelectCollaborator();
  const { data: tenantInfo } = useFetchTenantInfo();
  const { data } = useListTenantUser(tenantId || tenantInfo.tenant_id, true);

  useEffect(() => {
    const arr = data
      .filter(
        (member) => member.status === '1' && member.role !== TenantRole.Invite,
      )
      .map((member) => ({
        id: member.id,
        label: member.nickname || member.email,
        checked: false,
        permission: 0,
        role: TeamRole.Member,
        avatar: member.avatar,
      }));
    setList(arr);
  }, [data, setList]);

  return {
    handleMemberClick: handleClick,
    memberList: list,
    setMemberList: setList,
    selectAllMember: selectAll,
  };
}

export function useOperateGroup(tenantId: string) {
  const { handleClick, list, setList, selectAll } = useSelectCollaborator();
  const { data = [] } = useQuery<ITenantGroup[]>({
    queryKey: ['tenantGroup', 'list', tenantId],
    initialData: [],
    queryFn: async () => {
      const { data } = await listTenantGroups();
      return Array.isArray(data?.data?.groups) ? data.data.groups : [];
    },
  });

  useEffect(() => {
    setList(
      data.map((group) => ({
        id: group.id,
        label: group.name || group.id,
        checked: false,
        permission: 0,
        role: TeamRole.Group,
        avatar: group.avatar,
        parentId: group.parent_id,
      })),
    );
  }, [data, setList]);

  return {
    handleGroupClick: handleClick,
    groupList: list,
    setGroupList: setList,
    selectAllGroup: selectAll,
  };
}

export function useSelectCheckedList(list: CollaboratorItem[]) {
  return useMemo(() => {
    const nextList = list.filter((x) => x.checked);
    return uniqBy(nextList, 'id');
  }, [list]);
}

export type HandleClickType = ReturnType<
  typeof useSelectCollaborator
>['handleClick'];

type UseHandleRightItemClickType = {
  handleMemberClick: HandleClickType;
  handleGroupClick: HandleClickType;
};

export function useHandleRightItemClick({
  handleGroupClick,
  handleMemberClick,
}: UseHandleRightItemClickType) {
  return (item: CollaboratorItem) => {
    if (item.role === TeamRole.Member) {
      handleMemberClick(item.id, false);
    } else if (item.role === TeamRole.Group) {
      handleGroupClick(item.id, false);
    }
  };
}

function addItem(
  checkedList: CollaboratorItem[],
  list: CollaboratorItem[],
  id: string,
) {
  const item = list.find((x) => x.id === id);
  if (item && checkedList.every((x) => x.id !== id)) {
    checkedList.push({ ...item, checked: true });
  }
}

export function useCacheCheckedList(list: CollaboratorItem[]) {
  const [checkedList, setCheckedList] = useState<CollaboratorItem[]>([]);

  const add = useCallback(
    (id: string) => {
      setCheckedList((pre) => {
        const nextList = [...pre];
        addItem(nextList, list, id);

        return nextList;
      });
    },
    [list],
  );

  const remove = useCallback((id: string) => {
    setCheckedList((pre) => {
      return pre.filter((x) => x.id !== id);
    });
  }, []);

  const addList = useCallback(
    (ids: string[]) => {
      setCheckedList((pre) => {
        const nextList = [...pre];
        ids.forEach((id) => {
          addItem(nextList, list, id);
        });

        return nextList;
      });
    },
    [list],
  );

  const removeList = useCallback((ids: string[]) => {
    setCheckedList((pre) => {
      return pre.filter((x) => !ids.some((y) => y === x.id));
    });
  }, []);

  const cacheCheckedItem = useCallback(
    (id: string, value?: boolean) => {
      if (typeof value === 'boolean') {
        if (value) {
          add(id);
        } else {
          remove(id);
        }
      }
    },
    [add, remove],
  );

  const cacheCheckedList = useCallback(
    (value?: boolean, ids?: string[]) => {
      const nextIds = ids || list.map((x) => x.id);
      if (typeof value === 'boolean') {
        if (value) {
          addList(nextIds);
        } else {
          removeList(nextIds);
        }
      }
    },
    [addList, list, removeList],
  );

  return {
    cacheCheckedItem,
    cacheCheckedList,
    checkedList,
  };
}
