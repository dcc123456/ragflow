import { ButtonLoading } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { TransferList } from '@/components/ui/transfer-list';
import { TagRenameId } from '@/constants/knowledge';
import {
  useFetchDepartmentMemberList,
  useFetchGroupMemberList,
} from '@/hooks/use-team';
import { IModalProps } from '@/interfaces/common';
import { X } from 'lucide-react';
import { useCallback, useEffect, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { TriggerMemberManagementDialogType } from '../constant';
import { useTenantId } from '../use-operate-team';
import {
  useModifyGroupMemberRole,
  useOperateMember,
} from './use-operate-member';

// type OkType = IUpdateDepartmentRequestBody | IUpdateGroupRequestBody;

type IdType = 'group_id' | 'department_id';

export function MemberManagementDialog({
  loading,
  initialId,
  triggerMemberManagementDialogType,
  onOk,
  hideModal,
}: IModalProps<any> & {
  initialId?: string;
  triggerMemberManagementDialogType: TriggerMemberManagementDialogType;
}) {
  const { t } = useTranslation();
  const tenantId = useTenantId();
  const {
    data: departmentMemberList,
    setId: setDepartmentId,
    setTeamId,
  } = useFetchDepartmentMemberList();
  const { data: groupMemberList, setGroupId } =
    useFetchGroupMemberList(tenantId);

  const operationMap = useMemo(() => {
    const OperationMap = {
      [TriggerMemberManagementDialogType.Department]: {
        setId: setDepartmentId,
        data: departmentMemberList,
        id: 'department_id' as IdType,
      },
      [TriggerMemberManagementDialogType.Group]: {
        setId: setGroupId,
        data: groupMemberList,
        id: 'group_id' as IdType,
      },
    };

    return OperationMap[triggerMemberManagementDialogType];
  }, [
    departmentMemberList,
    groupMemberList,
    setDepartmentId,
    setGroupId,
    triggerMemberManagementDialogType,
  ]);

  const initialMemberList = useMemo(() => {
    return operationMap.data;
  }, [operationMap.data]);

  const {
    handleOpenChange,
    items,
    targetKeys,
    handleTransferChange,
    modifiedMemberList,
    // initializeMemberListRef,
    setModifiedMemberList,
  } = useOperateMember({
    data: initialMemberList,
    initialId: initialId,
    setId: operationMap.setId,
    triggerMemberManagementDialogType,
    hideModal,
  });

  const { setGroupMemberRoleAsAdmin, setGroupMemberRoleAsMember } =
    useModifyGroupMemberRole({ setModifiedMemberList });

  const handleOk = useCallback(() => {
    if (initialId) {
      onOk?.({
        [operationMap.id]: initialId,
        member_list: modifiedMemberList.filter(
          // Save the modified member list
          (x) => {
            if (
              x.status === '0' ||
              (x.status === '1' &&
                !initialMemberList.some((y) => y.member_id === x.member_id))
            ) {
              return true;
            }
            const initialItem = initialMemberList.find(
              (y) => y.member_id === x.member_id,
            );
            // modify group member role
            if (initialItem && initialItem.role !== x.role) {
              return true;
            }
            return false;
          },
        ),
      });
    }
  }, [initialId, initialMemberList, modifiedMemberList, onOk, operationMap.id]);

  useEffect(() => {
    if (
      triggerMemberManagementDialogType ===
      TriggerMemberManagementDialogType.Department
    ) {
      setTeamId(tenantId);
    }
  }, [setTeamId, tenantId, triggerMemberManagementDialogType]);

  return (
    <Dialog open onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-4xl">
        <DialogHeader>
          <DialogTitle>{t('permission.manageMember')}</DialogTitle>
        </DialogHeader>
        <TransferList
          items={items}
          targetKeys={targetKeys}
          onChange={handleTransferChange}
        >
          {(item) => {
            if (
              triggerMemberManagementDialogType ===
              TriggerMemberManagementDialogType.Group
            ) {
              const role = modifiedMemberList.find(
                (x) => x.member_id === item.key,
              )?.role;
              switch (role) {
                case 'member':
                  return (
                    <span
                      className=" bg-orange-300 cursor-pointer rounded px-1 invisible group-hover:visible"
                      onClick={() => setGroupMemberRoleAsAdmin(item.key)}
                    >
                      {t('permission.setAsAdministrator')}
                    </span>
                  );
                case 'owner':
                  return (
                    <span className="rounded bg-indigo-400 px-1">
                      {t('permission.owner')}
                    </span>
                  );
                case 'admin':
                  return (
                    <div className="inline-flex items-center gap-1 bg-blue-500 rounded px-1">
                      {t('permission.administrator')}
                      <X
                        className="cursor-pointer size-4"
                        onClick={() => setGroupMemberRoleAsMember(item.key)}
                      />
                    </div>
                  );
                default:
                  return <span>xxx</span>;
              }
            }
            return null;
          }}
        </TransferList>
        <DialogFooter>
          <ButtonLoading
            type="submit"
            form={TagRenameId}
            loading={loading}
            onClick={handleOk}
          >
            {t('common.save')}
          </ButtonLoading>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
