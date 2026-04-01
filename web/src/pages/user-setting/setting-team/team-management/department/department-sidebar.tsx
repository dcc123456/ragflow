import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import { ArrowLeftRight, Plus, Users } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useIsMyCreatedTeam } from '../use-operate-team';

type DepartmentSidebarType = {
  showDepartmentModal(): void;
  showDepartmentMemberModal(): void;
  showMoveDepartmentModal(): void;
  initialId: string | undefined;
  horizontal?: boolean;
};

export function DepartmentSidebar({
  showDepartmentModal,
  showDepartmentMemberModal,
  showMoveDepartmentModal,
  initialId,
  horizontal,
}: DepartmentSidebarType) {
  const { t } = useTranslation();
  const isMyCreatedTeam = useIsMyCreatedTeam();
  return (
    <section className={cn({ 'w-[260px]': !horizontal })}>
      {isMyCreatedTeam && (
        <ul
          className={cn('space-y-4', {
            'flex gap-4 items-center space-y-0': horizontal,
          })}
        >
          {initialId && (
            <>
              <Button onClick={showDepartmentMemberModal} variant={'outline'}>
                <Users className="size-4" />
                <span>{t('permission.manageMember')}</span>
              </Button>
              <Button onClick={showMoveDepartmentModal} variant={'outline'}>
                <ArrowLeftRight className="w-4 h-4" />
                <span>{t('permission.moveDepartment')}</span>
              </Button>
            </>
          )}

          <Button onClick={showDepartmentModal}>
            <Plus className="size-4" />
            <span>{t('permission.createSubDepartment')}</span>
          </Button>
        </ul>
      )}
    </section>
  );
}
