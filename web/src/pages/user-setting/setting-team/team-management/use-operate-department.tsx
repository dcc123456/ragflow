import { useSetModalState } from '@/hooks/common-hooks';
import {
  useCreateDepartment,
  useMoveDepartment,
  useUpdateDepartment,
} from '@/hooks/use-team';
import { IDepartment } from '@/interfaces/database/team';
import {
  ICreateDepartmentRequestBody,
  IUpdateDepartmentRequestBody,
} from '@/interfaces/request/team';
import { useCallback, useState } from 'react';
import { RootNodeId } from './constant';
import { useTenantId } from './use-operate-team';

export const useModifyDepartment = () => {
  const [department, setDepartment] = useState<Partial<IDepartment>>(
    {} as Partial<IDepartment>,
  );
  const {
    visible: departmentVisible,
    hideModal: hideDepartmentModal,
    showModal: showDepartmentModal,
  } = useSetModalState();
  const tenantId = useTenantId();
  const { createDepartment, loading } = useCreateDepartment();
  const { updateDepartment } = useUpdateDepartment(tenantId);

  const onDepartmentOk = useCallback(
    async (
      params: ICreateDepartmentRequestBody | IUpdateDepartmentRequestBody,
    ) => {
      let ret: number;
      if ('department_id' in department) {
        ret = await updateDepartment({
          ...(params as IUpdateDepartmentRequestBody),
          department_id: department.department_id!,
        });
      } else {
        ret = await createDepartment({
          ...(params as ICreateDepartmentRequestBody),
          parent_id: (department as IDepartment).parent_id,
        });
      }
      if (ret === 0) {
        hideDepartmentModal();
      }
    },
    [createDepartment, department, hideDepartmentModal, updateDepartment],
  );

  const handleShowDepartmentModal = useCallback(
    async (record?: Partial<IDepartment>) => {
      if (record) {
        setDepartment(record);
      }
      showDepartmentModal();
    },
    [showDepartmentModal],
  );

  return {
    departmentLoading: loading,
    onDepartmentOk,
    departmentVisible,
    department,
    hideDepartmentModal,
    showDepartmentModal: handleShowDepartmentModal,
  };
};

export const useModifyDepartmentMemberMember = () => {
  const [departmentMember, setDepartmentMember] = useState<IDepartment>(
    {} as IDepartment,
  );
  const {
    visible: departmentMemberVisible,
    hideModal: hideDepartmentMemberModal,
    showModal: showDepartmentMemberModal,
  } = useSetModalState();
  const tenantId = useTenantId();
  const { updateDepartment } = useUpdateDepartment(tenantId);

  const onDepartmentMemberOk = useCallback(
    async (params: IUpdateDepartmentRequestBody) => {
      const ret = await updateDepartment(params);

      if (ret === 0) {
        hideDepartmentMemberModal();
      }
    },
    [hideDepartmentMemberModal, updateDepartment],
  );

  const handleShowDepartmentMemberModal = useCallback(
    async (record?: IDepartment) => {
      if (record) {
        setDepartmentMember(record);
      }
      showDepartmentMemberModal();
    },
    [showDepartmentMemberModal],
  );

  return {
    departmentMemberLoading: false,
    onDepartmentMemberOk,
    departmentMemberVisible,
    departmentMember,
    hideDepartmentMemberModal,
    showDepartmentMemberModal: handleShowDepartmentMemberModal,
  };
};

export function useShowMoveDepartmentDialog() {
  const [departmentId, setDepartmentId] = useState('');
  const {
    visible: moveDepartmentVisible,
    hideModal: hideMoveDepartmentModal,
    showModal: showMoveDepartmentModal,
  } = useSetModalState();
  const { moveDepartment, loading } = useMoveDepartment();

  const handleShowMoveDepartmentModal = useCallback(
    (id?: string) => {
      if (id) {
        setDepartmentId(id);
        showMoveDepartmentModal();
      }
    },
    [showMoveDepartmentModal],
  );

  const handleHideMoveDepartmentModal = useCallback(() => {
    setDepartmentId('');
    hideMoveDepartmentModal();
  }, [hideMoveDepartmentModal]);

  const handleOk = useCallback(
    async (selectedId: string) => {
      if (departmentId) {
        const code = await moveDepartment({
          departmentId,
          parentId: selectedId === RootNodeId ? departmentId : selectedId,
        });
        if (code === 0) {
          hideMoveDepartmentModal();
        }
      }
    },
    [departmentId, hideMoveDepartmentModal, moveDepartment],
  );

  return {
    moveDepartmentVisible,
    hideMoveDepartmentModal: handleHideMoveDepartmentModal,
    showMoveDepartmentModal: handleShowMoveDepartmentModal,
    initialDepartmentId: departmentId,
    moveDepartmentLoading: loading,
    onMoveDepartmentOk: handleOk,
  };
}
