import { Move, Plus, Users } from 'lucide-react';
import { PropsWithChildren } from 'react';
import { useTranslation } from 'react-i18next';
import { useIsMyCreatedTeam } from '../use-operate-team';

type SideItemProps = PropsWithChildren & {
  onClick?: () => void;
};

function SideItem({ children, onClick }: SideItemProps) {
  return (
    <li
      onClick={onClick}
      className="flex gap-2 items-center hover:bg-slate-100 dark:hover:bg-slate-800 cursor-pointer p-2 rounded-md shadow-sm"
    >
      {children}
    </li>
  );
}

type DepartmentSidebarType = {
  showDepartmentModal(): void;
  showDepartmentMemberModal(): void;
  showMoveDepartmentModal(): void;
  initialId: string | undefined;
};

export function DepartmentSidebar({
  showDepartmentModal,
  showDepartmentMemberModal,
  showMoveDepartmentModal,
  initialId,
}: DepartmentSidebarType) {
  const { t } = useTranslation();
  const isMyCreatedTeam = useIsMyCreatedTeam();
  return (
    <section className="w-[260px]">
      {isMyCreatedTeam && (
        <ul className="space-y-4">
          <SideItem onClick={showDepartmentModal}>
            <Plus className="size-4" />
            <span>{t('permission.createSubDepartment')}</span>
          </SideItem>

          {initialId && (
            <>
              <SideItem onClick={showDepartmentMemberModal}>
                <Users className="size-4" />
                <span>{t('permission.manageMember')}</span>
              </SideItem>
              <SideItem onClick={showMoveDepartmentModal}>
                <Move className="size-4" />
                <span>{t('permission.moveDepartment')}</span>
              </SideItem>
              {/* <SideItem>
              <Trash2 className="size-4" />
              <span>Delete department</span>
            </SideItem> */}
            </>
          )}
        </ul>
      )}
    </section>
  );
}
