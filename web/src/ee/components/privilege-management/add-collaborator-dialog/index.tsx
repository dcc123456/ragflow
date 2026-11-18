import { ButtonLoading } from '@/components/ui/button';
import { SelectWithSearch } from '@/components/originui/select-with-search';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import {
  LabelMap,
  Permission,
  PermissionResourceType,
  TeamRole,
  getAssignableResourcePermissionOptions,
} from '@/ee/constants/team';
import {
  CheckedListContext,
  GroupContext,
  MemberContext,
} from '@/ee/components/privilege-management/context';
import { LeftPanel } from '@/ee/components/privilege-management/add-collaborator-dialog/left-panel';
import { RightPanel } from '@/ee/components/privilege-management/add-collaborator-dialog/right-panel';
import {
  useCacheCheckedList,
  useHandleRightItemClick,
  useOperateGroup,
  useOperateMember,
} from '@/ee/components/privilege-management/add-collaborator-dialog/use-select-collaborator';
import { mapCollaboratorId } from '@/ee/components/privilege-management/utils';
import { IUpdatePermission } from '@/ee/interfaces/request/team';
import { IModalProps } from '@/interfaces/common';
import { Eye, ShieldCheck, SquarePen, UserPlus } from 'lucide-react';
import { useCallback, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';

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
  canGrantManage,
  resourceType,
}: IModalProps<Partial<IUpdatePermission>> & {
  tenantId: string;
  canGrantManage: boolean;
  resourceType: PermissionResourceType;
}) {
  const { t } = useTranslation();
  const [value, setValue] = useState<string>(Permission.Read.toString());

  const { memberList } = useOperateMember(tenantId);
  const { groupList, handleGroupClick, selectAllGroup } =
    useOperateGroup(tenantId);
  const {
    cacheCheckedItem: cacheMemberCheckedItem,
    checkedList: memberCheckedList,
    cacheCheckedList: cacheMemberCheckedList,
  } = useCacheCheckedList(memberList);

  const selectAll = useCallback(
    (role: TeamRole, checked: boolean, ids: string[]) => {
      if (role === TeamRole.Group) {
        selectAllGroup(checked, ids);
      }
      if (role === TeamRole.Member) {
        cacheMemberCheckedList(checked, ids);
      }
    },
    [cacheMemberCheckedList, selectAllGroup],
  );

  const options = getAssignableResourcePermissionOptions(
    resourceType,
    canGrantManage,
  ).map(({ action, value }) => ({
    label: (
      <div className="flex items-center gap-2">
        <span className="shrink-0">{getPermissionIcon(action)}</span>
        <span>{t(`permission.${LabelMap[action]}`)}</span>
      </div>
    ),
    value: value.toString(),
  }));

  const clickMember = useCallback(
    (id: string, checked?: boolean) => {
      cacheMemberCheckedItem(id, checked);
    },
    [cacheMemberCheckedItem],
  );

  const memberContextValue = useMemo(
    () => ({ list: memberList, handleClick: clickMember }),
    [clickMember, memberList],
  );
  const groupContextValue = useMemo(
    () => ({ list: groupList, handleClick: handleGroupClick }),
    [groupList, handleGroupClick],
  );
  const groupCheckedList = useMemo(
    () => groupList.filter((item) => item.checked),
    [groupList],
  );
  const checkedList = useMemo(
    () => ({ groupCheckedList, memberCheckedList }),
    [groupCheckedList, memberCheckedList],
  );
  const selectedCollaborators = useMemo(
    () => [...memberCheckedList, ...groupCheckedList],
    [groupCheckedList, memberCheckedList],
  );
  const clickRightItem = useHandleRightItemClick({
    handleMemberClick: clickMember,
    handleGroupClick,
  });

  const handleOk = useCallback(() => {
    onOk?.({
      permission: Number(value),
      member_list: mapCollaboratorId(memberCheckedList),
      group_list: mapCollaboratorId(groupCheckedList),
    });
  }, [groupCheckedList, memberCheckedList, onOk, value]);

  return (
    <Dialog open onOpenChange={hideModal}>
      <DialogContent className="max-h-[calc(100vh-2rem)] max-w-4xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <UserPlus />
            {t('permission.addCollaborator')}
          </DialogTitle>
        </DialogHeader>
        <section className="flex h-[min(58vh,540px)] min-h-[320px] min-w-0 overflow-hidden rounded-lg border divide-x">
          <div className="flex min-h-0 min-w-0 flex-1 flex-col p-4">
            <CheckedListContext.Provider value={checkedList}>
              <MemberContext.Provider value={memberContextValue}>
                <GroupContext.Provider value={groupContextValue}>
                  <LeftPanel selectAll={selectAll} />
                </GroupContext.Provider>
              </MemberContext.Provider>
            </CheckedListContext.Provider>
          </div>
          <div className="min-h-0 min-w-0 flex-1 overflow-hidden p-4">
            <RightPanel list={selectedCollaborators} click={clickRightItem} />
          </div>
        </section>
        <DialogFooter className="min-w-0 gap-2 sm:justify-between sm:space-x-0">
          <div className="min-w-0 flex-1">
            <SelectWithSearch
              options={options}
              onChange={setValue}
              value={value}
              triggerClassName="min-w-0"
            />
          </div>
          <ButtonLoading
            type="submit"
            onClick={handleOk}
            loading={loading}
            disabled={selectedCollaborators.length === 0}
            className="w-full shrink-0 sm:w-auto"
          >
            {t('common.save')}
          </ButtonLoading>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
