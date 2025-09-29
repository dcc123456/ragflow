import { TenantIdContext } from '@/contexts/teant-context';
import { useSetModalState } from '@/hooks/common-hooks';
import {
  useCreateGroup,
  useTransferGroupOwner,
  useUpdateGroup,
} from '@/hooks/use-team';
import { IGroup, IMember } from '@/interfaces/database/team';
import {
  ICreateGroupRequestBody,
  ITransferGroupOwnerRequestBody,
  IUpdateGroupRequestBody,
} from '@/interfaces/request/team';
import { isEmpty } from 'lodash';
import { useCallback, useContext, useMemo, useState } from 'react';
import { GroupRole } from './constant';
import { useIsMyCreatedTeam } from './use-operate-team';

export const useModifyGroup = (tenantId: string) => {
  const [group, setGroup] = useState<IGroup>({} as IGroup);
  const {
    visible: groupVisible,
    hideModal: hideGroupModal,
    showModal: showGroupModal,
  } = useSetModalState();
  const { updateGroup, loading } = useUpdateGroup(tenantId);
  const { createGroup, loading: createLoading } = useCreateGroup();

  const onGroupOk = useCallback(
    async (params: IUpdateGroupRequestBody | ICreateGroupRequestBody) => {
      let ret: number;
      if (!isEmpty(group)) {
        ret = await updateGroup(params as IUpdateGroupRequestBody);
      } else {
        ret = await createGroup(params as ICreateGroupRequestBody);
      }

      if (ret === 0) {
        hideGroupModal();
        setGroup({} as IGroup);
      }
    },
    [createGroup, group, hideGroupModal, updateGroup],
  );

  const handleShowGroupModal = useCallback(
    (record?: IGroup) => {
      if (record) {
        setGroup(record);
      }
      showGroupModal();
    },
    [showGroupModal],
  );

  const handleHideGroupModal = useCallback(() => {
    setGroup({} as IGroup);
    hideGroupModal();
  }, [hideGroupModal]);

  return {
    groupLoading: loading || createLoading,
    onGroupOk,
    groupVisible,
    group,
    hideGroupModal: handleHideGroupModal,
    showGroupModal: handleShowGroupModal,
  };
};

export const useModifyGroupMember = () => {
  const [groupMember, setGroupMember] = useState<IGroup>({} as IGroup);
  const {
    visible: groupMemberVisible,
    hideModal: hideGroupMemberModal,
    showModal: showGroupMemberModal,
  } = useSetModalState();
  const tenantId = useContext(TenantIdContext);
  const { updateGroup } = useUpdateGroup(tenantId);

  const onGroupMemberOk = useCallback(
    async (params: IUpdateGroupRequestBody) => {
      const ret = await updateGroup(params);

      if (ret === 0) {
        hideGroupMemberModal();
      }
    },
    [hideGroupMemberModal, updateGroup],
  );

  const handleShowGroupMemberModal = useCallback(
    async (record?: IGroup) => {
      if (record) {
        setGroupMember(record);
      }
      showGroupMemberModal();
    },
    [showGroupMemberModal],
  );

  return {
    groupMemberLoading: false,
    onGroupMemberOk,
    groupMemberVisible,
    groupMember,
    hideGroupMemberModal,
    showGroupMemberModal: handleShowGroupMemberModal,
  };
};

export const useTransferOwner = () => {
  const [groupMember, setGroupMember] = useState<IGroup>({} as IGroup);
  const {
    visible: transferOwnerVisible,
    hideModal: hideTransferOwnerModal,
    showModal: showGroupMemberModal,
  } = useSetModalState();
  const tenantId = useContext(TenantIdContext);
  const { transferGroupOwner, loading } = useTransferGroupOwner(tenantId);

  const onTransferOwnerOk = useCallback(
    async (params: ITransferGroupOwnerRequestBody) => {
      const ret = await transferGroupOwner({
        ...params,
        group_id: groupMember.group_id,
      });

      if (ret === 0) {
        hideTransferOwnerModal();
      }
    },
    [groupMember.group_id, hideTransferOwnerModal, transferGroupOwner],
  );

  const handleShowTransferOwnerModal = useCallback(
    async (record?: IGroup) => {
      if (record) {
        setGroupMember(record);
      }
      showGroupMemberModal();
    },
    [showGroupMemberModal],
  );

  return {
    transferOwnerLoading: loading,
    onTransferOwnerOk,
    transferOwnerVisible,
    groupMember,
    hideTransferOwnerModal,
    showTransferOwnerModal: handleShowTransferOwnerModal,
  };
};

export function useShowPartialDropdownItem(
  userId: string,
  groupMemberList: IMember[],
) {
  const isMyCreatedTeam = useIsMyCreatedTeam();

  // const { data: userInfo } = useFetchUserInfo(); // Writing like this will cause the interface to be called continuously

  const currentUserRole = useMemo(() => {
    const role = groupMemberList.find((x) => x.user_id === userId)?.role;

    return role;
  }, [groupMemberList, userId]);

  const showOwnerTransferAndMemberDeletingDropdownItem = useMemo(() => {
    if (!isMyCreatedTeam) {
      return currentUserRole === GroupRole.Owner;
    }
    return true;
  }, [currentUserRole, isMyCreatedTeam]);

  const showInfoEditingAndMemberManagementDropdownItem = useMemo(() => {
    if (!isMyCreatedTeam) {
      return (
        currentUserRole === GroupRole.Admin ||
        currentUserRole === GroupRole.Owner
      );
    }
    return true;
  }, [currentUserRole, isMyCreatedTeam]);

  return {
    showOwnerTransferAndMemberDeletingDropdownItem,
    showInfoEditingAndMemberManagementDropdownItem,
  };
}
