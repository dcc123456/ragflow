import { ButtonLoading } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { RAGFlowSelect } from '@/components/ui/select';
import {
  LabelMap,
  Permission,
  PermissionList,
  TeamRole,
} from '@/constants/team';
import { IModalProps } from '@/interfaces/common';
import { IUpdatePermission } from '@/interfaces/request/team';
import { uniqBy } from 'lodash';
import { Eye, ShieldCheck, SquarePen, UserPlus } from 'lucide-react';
import { useCallback, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  CheckedListContext,
  DepartmentContext,
  GroupContext,
  MemberContext,
} from '../context';
import { mapCollaboratorId } from '../utils';
import { LeftPanel } from './left-panel';
import { RightPanel } from './right-panel';
import {
  useCacheCheckedList,
  useHandleRightItemClick,
  useOperateDepartment,
  useOperateGroup,
  useOperateMember,
} from './use-select-collaborator';

function getPermissionIcon(permission: Permission) {
  if (permission === Permission.Read) {
    return <Eye className="size-4" />;
  }
  if (permission === Permission.Write) {
    return <SquarePen className="size-4" />;
  }
  if (permission === Permission.Manage) {
    return <ShieldCheck className="size-4" />;
  }
  return null;
}

export function AddCollaboratorDialog({
  hideModal,
  onOk,
  loading,
  tenantId,
}: IModalProps<Partial<IUpdatePermission>> & {
  tenantId: string;
}) {
  const { t } = useTranslation();
  const [value, setValue] = useState<string>(Permission.Read.toString());

  const { memberList } = useOperateMember(tenantId);
  const { groupList, handleGroupClick, selectAllGroup } =
    useOperateGroup(tenantId);
  const {
    departmentList,
    setId,
    setFetchDepartmentListParams,
    pureDepartmentList,
    departmentMemberList,
  } = useOperateDepartment(tenantId);

  const currentMemberList = useMemo(() => {
    // Usually departmentMemberList is a subset of memberList
    return uniqBy(memberList.concat(departmentMemberList), 'id');
  }, [departmentMemberList, memberList]);

  const {
    cacheCheckedItem: cacheMemberCheckedItem,
    checkedList: memberCheckedList,
    cacheCheckedList: cacheMemberCheckedList,
  } = useCacheCheckedList(currentMemberList);

  const {
    cacheCheckedItem: cacheDepartmentCheckedItem,
    checkedList: departmentCheckedList,
    cacheCheckedList: cacheDepartmentCheckedList,
  } = useCacheCheckedList(pureDepartmentList);

  const selectAll = useCallback(
    (role: TeamRole, checked: boolean) => {
      if (role === TeamRole.Group) {
        selectAllGroup(checked);
      }
      if (role === TeamRole.Member) {
        cacheMemberCheckedList(checked);
      }

      if (role === TeamRole.Department) {
        cacheMemberCheckedList(
          checked,
          mapCollaboratorId(departmentMemberList),
        );
        cacheDepartmentCheckedList(checked);
      }
    },
    [
      cacheDepartmentCheckedList,
      cacheMemberCheckedList,
      departmentMemberList,
      selectAllGroup,
    ],
  );

  const options = PermissionList.map((x) => ({
    label: (
      <div className="flex items-center gap-2">
        <span className="shrink-0">{getPermissionIcon(x)}</span>
        <span>{t(`permission.${LabelMap[x]}Permission`)}</span>
      </div>
    ),
    value: x.toString(),
  }));

  const clickMember = useCallback(
    (id: string, value?: boolean) => {
      cacheMemberCheckedItem(id, value);
    },
    [cacheMemberCheckedItem],
  );

  const memberContextValue = useMemo(() => {
    return {
      list: memberList,
      handleClick: clickMember,
    };
  }, [clickMember, memberList]);

  const groupContextValue = useMemo(() => {
    return {
      list: groupList,
      handleClick: handleGroupClick,
    };
  }, [groupList, handleGroupClick]);

  const clickDepartment = useCallback(
    (id: string, value?: boolean) => {
      cacheDepartmentCheckedItem(id, value);
    },
    [cacheDepartmentCheckedItem],
  );

  const departmentContextValue = useMemo(() => {
    return {
      list: departmentList,
      handleClick: clickDepartment,
      setId,
      setFetchDepartmentListParams,
    };
  }, [clickDepartment, departmentList, setFetchDepartmentListParams, setId]);

  const groupCheckedList = useMemo(() => {
    return groupList.filter((x) => x.checked);
  }, [groupList]);

  const nextCheckedList = useMemo(() => {
    return {
      departmentCheckedList,
      memberCheckedList,
      groupCheckedList,
    };
  }, [departmentCheckedList, groupCheckedList, memberCheckedList]);

  const flatCheckedList = useMemo(() => {
    return [
      ...memberCheckedList,
      ...departmentCheckedList,
      ...groupCheckedList,
    ];
  }, [departmentCheckedList, groupCheckedList, memberCheckedList]);

  const clickRightItem = useHandleRightItemClick({
    handleMemberClick: clickMember,
    handleDepartmentClick: clickDepartment,
    handleGroupClick,
  });

  const handleOk = useCallback(() => {
    onOk?.({
      permission: Number(value),
      member_list: mapCollaboratorId(memberCheckedList),
      department_list: mapCollaboratorId(departmentCheckedList),
      group_list: mapCollaboratorId(groupCheckedList),
    });
  }, [departmentCheckedList, groupCheckedList, memberCheckedList, onOk, value]);

  return (
    <Dialog open onOpenChange={hideModal}>
      <DialogContent className="max-w-4xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <UserPlus />
            {t('permission.addCollaborator')}
          </DialogTitle>
        </DialogHeader>
        <section className="border rounded-lg divide-x flex">
          <div className="flex-1 p-4">
            <CheckedListContext.Provider value={nextCheckedList}>
              <MemberContext.Provider value={memberContextValue}>
                <DepartmentContext.Provider value={departmentContextValue}>
                  <GroupContext.Provider value={groupContextValue}>
                    <LeftPanel selectAll={selectAll}></LeftPanel>
                  </GroupContext.Provider>
                </DepartmentContext.Provider>
              </MemberContext.Provider>
            </CheckedListContext.Provider>
          </div>
          <div className="flex-1 p-4">
            <RightPanel
              list={flatCheckedList}
              click={clickRightItem}
            ></RightPanel>
          </div>
        </section>
        <DialogFooter>
          <RAGFlowSelect
            options={options}
            onChange={setValue}
            value={value}
          ></RAGFlowSelect>
          <ButtonLoading
            type="submit"
            onClick={handleOk}
            loading={loading}
            disabled={flatCheckedList.length === 0}
          >
            {t('common.save')}
          </ButtonLoading>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
