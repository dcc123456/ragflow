import { TeamRole, TeamRoleList } from '@/constants/team';
import {
  useFetchDepartmentList,
  useFetchDepartmentMemberList,
  useFetchGroupList,
} from '@/hooks/use-team';
import {
  useFetchTenantInfo,
  useListTenantUser,
} from '@/hooks/use-user-setting-request';
import { uniqBy } from 'lodash';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { CollaboratorItem } from '../interface';

export function useSelectCollaborator() {
  const [list, setList] = useState<CollaboratorItem[]>([]);

  const handleClick = useCallback((id: string, value?: boolean) => {
    setList((pre) => {
      const nextList = [...pre];
      const item = nextList.find((x) => x.id === id);
      if (item) {
        item.checked = typeof value === 'boolean' ? value : !item.checked;
      }

      return nextList;
    });
  }, []);

  const selectAll = useCallback((checked: boolean) => {
    setList((pre) => {
      return pre.map((x) => ({ ...x, checked }));
    });
  }, []);

  return { list, setList, handleClick, selectAll };
}

export function useSwitchBreadcrumb() {
  const [breadcrumbs, setBreadcrumbs] = useState<CollaboratorItem[]>([]);
  const latestBreadcrumb = useMemo(() => {
    return breadcrumbs.at(-1)?.id;
  }, [breadcrumbs]);
  const firstBreadcrumb = useMemo(() => {
    return breadcrumbs.at(0)?.id;
  }, [breadcrumbs]);
  const secondBreadcrumb = useMemo(() => {
    return breadcrumbs.at(1)?.id;
  }, [breadcrumbs]);

  const handleCollaboratorClick = useCallback((item: CollaboratorItem) => {
    setBreadcrumbs((pre) => {
      const nextBreadcrumbs = [
        ...pre,
        {
          label: item.label,
          id: item.id,
        },
      ];

      if (pre.length === 0) {
        nextBreadcrumbs.unshift({ id: 'team', label: 'Team' });
      }

      return nextBreadcrumbs;
    });
  }, []);

  const handleBreadcrumbClick = useCallback((idx: number) => {
    setBreadcrumbs((pre) => {
      return pre.slice(0, idx === 0 ? idx : idx + 1);
    });
  }, []);

  return {
    breadcrumbs,
    latestBreadcrumb,
    firstBreadcrumb,
    secondBreadcrumb,
    setBreadcrumbs,
    clickCollaborator: handleCollaboratorClick,
    clickBreadcrumb: handleBreadcrumbClick,
  };
}

export function useOperateMember(tenantId: string) {
  const { handleClick, list, setList, selectAll } = useSelectCollaborator();
  const { data: tenantInfo } = useFetchTenantInfo();
  const { data } = useListTenantUser(tenantId || tenantInfo.tenant_id, true);

  useEffect(() => {
    const arr = data.map((x) => ({
      id: x.id,
      label: x.nickname,
      checked: false,
      permission: 0,
      role: TeamRole.Member,
      avatar: x.avatar,
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
  const { data: tenantInfo } = useFetchTenantInfo();
  const { data } = useFetchGroupList(tenantId || tenantInfo.tenant_id);

  useEffect(() => {
    const arr = data.map((x) => ({
      id: x.group_id,
      label: x.name,
      checked: false,
      permission: 0,
      role: TeamRole.Group,
      avatar: x.avatar,
    }));
    setList(arr);
  }, [data, setList]);

  return {
    handleGroupClick: handleClick,
    groupList: list,
    setGroupList: setList,
    selectAllGroup: selectAll,
  };
}

export function useOperateDepartment(tenantId: string) {
  const { handleClick, list, setList, selectAll } = useSelectCollaborator();
  const { data: tenantInfo } = useFetchTenantInfo();
  const { data, setFetchDepartmentListParams } = useFetchDepartmentList(
    tenantId || tenantInfo.tenant_id,
  );
  const {
    data: departmentMemberList,
    setTeamId,
    setId,
    departmentId,
  } = useFetchDepartmentMemberList();

  useEffect(() => {
    const arr = data.map((x) => ({
      id: x.department_id,
      label: x.name,
      checked: false,
      permission: 0,
      role: TeamRole.Department,
      avatar: x.avatar,
    }));
    const memberList = departmentMemberList.map((x) => ({
      id: x.member_id,
      label: x.nickname,
      checked: false,
      permission: 0,
      isMember: true,
      role: TeamRole.Member,
      avatar: x.avatar,
    }));
    setList(arr.concat(departmentId ? memberList : []));
  }, [data, departmentId, departmentMemberList, setList]);

  useEffect(() => {
    setTeamId(tenantInfo.tenant_id);
  }, [setTeamId, tenantInfo.tenant_id]);

  return {
    handleDepartmentClick: handleClick,
    departmentList: list,
    setDepartmentList: setList,
    setFetchDepartmentListParams,
    setId,
    selectAllDepartment: selectAll,
    departmentMemberList: list.filter((x) => x.role === TeamRole.Member),
    pureDepartmentList: list.filter((x) => x.role === TeamRole.Department),
  };
}

export type UseOperateDepartmentType = ReturnType<typeof useOperateDepartment>;

export type UseWatchBreadcrumbChangeType = {
  latestBreadcrumb?: string;
} & Pick<ReturnType<typeof useFetchDepartmentMemberList>, 'setId'> &
  Pick<
    ReturnType<typeof useFetchDepartmentList>,
    'setFetchDepartmentListParams'
  >;

export function useWatchBreadcrumbChange({
  latestBreadcrumb,
  setId,
  setFetchDepartmentListParams,
}: UseWatchBreadcrumbChangeType) {
  useEffect(() => {
    if (latestBreadcrumb && TeamRoleList.every((x) => x !== latestBreadcrumb)) {
      setId(latestBreadcrumb);
      setFetchDepartmentListParams({ parentId: latestBreadcrumb });
    }

    if (TeamRoleList.some((x) => x === latestBreadcrumb)) {
      setId('');
      setFetchDepartmentListParams({ parentId: '' });
    }
  }, [latestBreadcrumb, setFetchDepartmentListParams, setId]);
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
  handleDepartmentClick: HandleClickType;
  handleMemberClick: HandleClickType;
  handleGroupClick: HandleClickType;
};

export function useHandleRightItemClick({
  handleDepartmentClick,
  handleGroupClick,
  handleMemberClick,
}: UseHandleRightItemClickType) {
  const ClickMap: Record<TeamRole, HandleClickType> = {
    [TeamRole.Department]: handleDepartmentClick,
    [TeamRole.Group]: handleGroupClick,
    [TeamRole.Member]: handleMemberClick,
  };
  return (item: CollaboratorItem) => {
    if (item.role) {
      const click = ClickMap[item.role];
      click(item.id, false);
    }
  };
}

function addItem(
  preCheckedList: CollaboratorItem[],
  checkedList: CollaboratorItem[],
  list: CollaboratorItem[],
  id: string,
) {
  const item = list.find((x) => x.id === id);
  if (item && preCheckedList.every((x) => x.id !== id)) {
    checkedList.push({ ...item, checked: true });
  }
}

export function useCacheCheckedList(list: CollaboratorItem[]) {
  const [checkedList, setCheckedList] = useState<CollaboratorItem[]>([]);

  const add = useCallback(
    (id: string) => {
      setCheckedList((pre) => {
        const nextList = [...pre];
        // const item = list.find((x) => x.id === id);
        // if (item) {
        //   nextList.push({ ...item, checked: true });
        // }

        addItem(pre, nextList, list, id);

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
          addItem(pre, nextList, list, id);
          // const item = list.find((x) => x.id === id);
          // if (item && pre.every((x) => x.id !== id)) {
          //   nextList.push({ ...item, checked: true });
          // }
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
      console.info(checkedList);
      if (typeof value === 'boolean') {
        if (value) {
          add(id);
        } else {
          remove(id);
        }
      }
    },
    [add, checkedList, remove],
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
