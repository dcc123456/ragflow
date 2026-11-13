import { Button, ButtonLoading } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { RAGFlowAvatar } from '@/components/ragflow-avatar';
import {
  ITenantGroup,
  ITenantGroupMember,
} from '@/ee/interfaces/database/team';
import { IModalProps } from '@/interfaces/common';
import { useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { useTransferTenantGroupOwner } from './use-group-query';

export function TransferOwnerDialog({
  group,
  member,
  hideModal,
}: IModalProps<unknown> & {
  group: ITenantGroup;
  member: ITenantGroupMember;
}) {
  const { t } = useTranslation();
  const { loading, transferOwner } = useTransferTenantGroupOwner(group.id);
  const displayName = member.nickname || member.email;

  const handleConfirm = useCallback(async () => {
    const code = await transferOwner(member.user_id);

    if (code === 0) {
      hideModal?.();
    }
  }, [hideModal, member.user_id, transferOwner]);

  return (
    <Dialog open onOpenChange={hideModal}>
      <DialogContent className="sm:max-w-[425px]">
        <DialogHeader>
          <DialogTitle>{t('permission.transferOwner')}</DialogTitle>
          <p className="truncate text-sm text-text-secondary">{group.name}</p>
        </DialogHeader>
        <p className="text-sm text-text-secondary">
          {t('permission.transferOwnerHint')}
        </p>
        <div className="flex items-center gap-3 rounded-lg bg-bg-card p-3">
          <RAGFlowAvatar
            isPerson
            className="size-8"
            name={displayName}
            avatar={member.avatar}
          />
          <div className="min-w-0">
            <div className="truncate text-sm text-text-primary">
              {displayName}
            </div>
            <div className="truncate text-xs text-text-secondary">
              {member.email}
            </div>
          </div>
        </div>
        <DialogFooter className="gap-2">
          <Button variant="outline" onClick={hideModal}>
            {t('common.cancel')}
          </Button>
          <ButtonLoading loading={loading} onClick={handleConfirm}>
            {t('common.confirm')}
          </ButtonLoading>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
