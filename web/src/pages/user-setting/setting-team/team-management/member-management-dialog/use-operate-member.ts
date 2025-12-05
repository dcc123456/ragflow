import {
  TransferListMoveDirection,
  TransferListProps,
} from '@/components/ui/transfer-list';
import { TenantIdContext } from '@/contexts/teant-context';
import { useListTenantUser } from '@/hooks/use-user-setting-request';
import { IModalProps } from '@/interfaces/common';
import { IMember } from '@/interfaces/database/team';
import {
  IUpdateDepartmentMemberListRequest,
  IUpdateDepartmentRequestBody,
  IUpdateGroupRequestBody,
} from '@/interfaces/request/team';
import {
  Dispatch,
  SetStateAction,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import { GroupRole, TriggerMemberManagementDialogType } from '../constant';

type UseOperateMemberType = {
  data: IMember[];
  initialId?: string;
  setId(id: string): void;
} & IModalProps<IUpdateDepartmentRequestBody | IUpdateGroupRequestBody> & {
    initialId?: string;
    triggerMemberManagementDialogType: TriggerMemberManagementDialogType;
  };

export function useOperateMember({
  data,
  initialId,
  hideModal,
  setId,
}: UseOperateMemberType) {
  const tenantId = useContext(TenantIdContext);
  const { data: list } = useListTenantUser(tenantId, true);
  const [targetKeys, setTargetKeys] = useState<string[]>([]);
  const initializeMemberListRef = useRef<string[]>();

  const items = useMemo(() => {
    return list.map((x) => ({
      key: x.id,
      label: x.nickname,
      disabled:
        data.find((y) => y.member_id === x.id)?.role === GroupRole.Owner,
    }));
  }, [data, list]);

  const [modifiedMemberList, setModifiedMemberList] = useState<
    IUpdateDepartmentMemberListRequest[]
  >([]);

  const handleTransferChange = useCallback<
    Required<TransferListProps>['onChange']
  >((targetKeys, direction, moveKeys) => {
    setTargetKeys(targetKeys);
    setModifiedMemberList((list) => {
      const nextList = [...list];
      moveKeys.forEach((x) => {
        const item = nextList.find((y) => y.member_id === x);

        if (item) {
          item['status'] =
            direction === TransferListMoveDirection.Right ? '1' : '0';
        }
      });
      return nextList;
    });
  }, []);

  const handleOpenChange = useCallback(() => {
    hideModal?.();
    initializeMemberListRef.current = [];
  }, [hideModal]);

  useEffect(() => {
    const list = data.map((x) => x.member_id);
    setTargetKeys(list);
    initializeMemberListRef.current = list;
  }, [data]);

  useEffect(() => {
    if (initialId) {
      setId(initialId);
    }
  }, [initialId, setId]);

  useEffect(() => {
    setModifiedMemberList(
      list.map((x) => {
        const role = data.find((y) => y.member_id === x.id)?.role;
        return { member_id: x.id, role: role ? role : 'member' };
      }),
    );
  }, [data, list]);

  return {
    items,
    handleTransferChange,
    targetKeys,
    handleOpenChange,
    modifiedMemberList,
    initializeMemberListRef,
    setModifiedMemberList,
  };
}

type UseModifyGroupMemberRole = {
  setModifiedMemberList: Dispatch<
    SetStateAction<IUpdateDepartmentMemberListRequest[]>
  >;
};

export function useModifyGroupMemberRole({
  setModifiedMemberList,
}: UseModifyGroupMemberRole) {
  const modifyGroupMemberRole = useCallback(
    (role: string, id: string) => {
      setModifiedMemberList((pre) => {
        return pre.map((x) => {
          return { ...x, role: x.member_id === id ? role : x.role };
        });
      });
    },
    [setModifiedMemberList],
  );

  const setGroupMemberRoleAsAdmin = useCallback(
    (id: string) => {
      modifyGroupMemberRole(GroupRole.Admin, id);
    },
    [modifyGroupMemberRole],
  );

  const setGroupMemberRoleAsMember = useCallback(
    (id: string) => {
      modifyGroupMemberRole(GroupRole.Member, id);
    },
    [modifyGroupMemberRole],
  );

  return { setGroupMemberRoleAsAdmin, setGroupMemberRoleAsMember };
}
