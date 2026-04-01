import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from '@/components/ui/breadcrumb';
import { useCallback, useEffect, useMemo } from 'react';
import { createPortal } from 'react-dom';
import { TriggerMemberManagementDialogType } from '../constant';
import { MemberManagementDialog } from '../member-management-dialog';
import {
  useModifyDepartment,
  useModifyDepartmentMemberMember,
  useShowMoveDepartmentDialog,
} from '../use-operate-department';
import { useTenantId } from '../use-operate-team';
import { CreateDepartmentDialog } from './create-department-dialog';
import { DepartmentSidebar } from './department-sidebar';
import { DepartmentTable } from './department-table';
import { MoveDepartmentDialog } from './move-department-dialog';
import { useSwitchBreadcrumb } from './use-switch-breadcrumb';

export function Department() {
  const { breadcrumbs, setBreadcrumbs, switchToHomeBreadcrumb } =
    useSwitchBreadcrumb();
  const {
    departmentVisible,
    hideDepartmentModal,
    showDepartmentModal,
    onDepartmentOk,
    department,
  } = useModifyDepartment();

  const tenantId = useTenantId();

  const {
    hideDepartmentMemberModal,
    showDepartmentMemberModal,
    departmentMemberVisible,
    onDepartmentMemberOk,
  } = useModifyDepartmentMemberMember();

  const {
    showMoveDepartmentModal,
    hideMoveDepartmentModal,
    moveDepartmentVisible,
    onMoveDepartmentOk,
    moveDepartmentLoading,
    initialDepartmentId,
  } = useShowMoveDepartmentDialog();

  const departmentParentId = useMemo(() => {
    return breadcrumbs.at(-1)?.value;
  }, [breadcrumbs]);

  const handleShowDepartmentModal = useCallback(() => {
    showDepartmentModal({ parent_id: departmentParentId });
  }, [departmentParentId, showDepartmentModal]);

  const handleShowMoveDepartmentModal = useCallback(
    (selectedId?: string) => {
      if (typeof selectedId === 'string') {
        showMoveDepartmentModal(selectedId);
      } else {
        showMoveDepartmentModal(departmentParentId);
      }
    },
    [departmentParentId, showMoveDepartmentModal],
  );

  useEffect(() => {
    switchToHomeBreadcrumb();
  }, [switchToHomeBreadcrumb, tenantId]);

  return (
    <section className="space-y-4">
      <Breadcrumb>
        <BreadcrumbList>
          {breadcrumbs.map((x, idx) => {
            return (
              <div key={x.value} className="flex items-center">
                {idx !== 0 && (
                  <BreadcrumbSeparator className="mr-2"></BreadcrumbSeparator>
                )}
                <BreadcrumbItem
                  onClick={() => {
                    setBreadcrumbs((pre) => {
                      return pre.slice(0, idx + 1);
                    });
                  }}
                >
                  {idx === breadcrumbs.length - 1 ? (
                    <BreadcrumbPage>{x.label}</BreadcrumbPage>
                  ) : (
                    <>
                      <BreadcrumbLink>{x.label}</BreadcrumbLink>
                    </>
                  )}
                </BreadcrumbItem>
              </div>
            );
          })}
        </BreadcrumbList>
      </Breadcrumb>
      <div className="flex gap-4">
        <DepartmentTable
          breadcrumbs={breadcrumbs}
          setBreadcrumbs={setBreadcrumbs}
          showDepartmentModal={showDepartmentModal}
          departmentParentId={departmentParentId}
          showMoveDepartmentModal={handleShowMoveDepartmentModal}
        ></DepartmentTable>
      </div>
      {departmentVisible && (
        <CreateDepartmentDialog
          hideModal={hideDepartmentModal}
          onOk={onDepartmentOk}
          initialValues={department}
        ></CreateDepartmentDialog>
      )}
      {departmentMemberVisible && (
        <MemberManagementDialog
          hideModal={hideDepartmentMemberModal}
          initialId={departmentParentId}
          onOk={onDepartmentMemberOk}
          triggerMemberManagementDialogType={
            TriggerMemberManagementDialogType.Department
          }
        ></MemberManagementDialog>
      )}
      {moveDepartmentVisible && (
        <MoveDepartmentDialog
          hideModal={hideMoveDepartmentModal}
          onOk={onMoveDepartmentOk}
          loading={moveDepartmentLoading}
          initialDepartmentId={initialDepartmentId}
        ></MoveDepartmentDialog>
      )}
      {typeof document !== 'undefined' &&
        document.getElementById('department-toolbar') &&
        createPortal(
          <DepartmentSidebar
            showDepartmentModal={handleShowDepartmentModal}
            showDepartmentMemberModal={showDepartmentMemberModal}
            showMoveDepartmentModal={handleShowMoveDepartmentModal}
            initialId={departmentParentId}
            horizontal
          ></DepartmentSidebar>,
          document.getElementById('department-toolbar')!,
        )}
    </section>
  );
}
