import { Button, ButtonLoading } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { SelectWithSearch } from '@/components/originui/select-with-search';
import { ITenantGroup } from '@/ee/interfaces/database/team';
import {
  useFetchTenantInfo,
  useListTenantUser,
} from '@/hooks/use-user-setting-request';
import { IModalProps } from '@/interfaces/common';
import { TenantMembershipStatus } from '@/pages/user-setting/constants';
import { useCallback, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useTransferTenantGroupOwner } from './use-group-query';

export function TransferOwnerDialog({
  group,
  hideModal,
}: IModalProps<unknown> & { group: ITenantGroup }) {
  const { t } = useTranslation();
  const { data: tenantInfo } = useFetchTenantInfo();
  const { data: tenantUsers } = useListTenantUser(tenantInfo.tenant_id, true);
  const { loading, transferOwner } = useTransferTenantGroupOwner(group.id);
  const [selectedUserId, setSelectedUserId] = useState('');

  const candidates = useMemo(() => {
    return tenantUsers.filter(
      (user) =>
        user.user_id &&
        user.status === TenantMembershipStatus.Joined &&
        user.user_id !== group.owner_user_id,
    );
  }, [group.owner_user_id, tenantUsers]);

  const handleSelectedUserChange = useCallback((value: string) => {
    setSelectedUserId(value);
  }, []);

  const handleConfirm = useCallback(async () => {
    if (!selectedUserId) {
      return;
    }

    const code = await transferOwner(selectedUserId);

    if (code === 0) {
      hideModal?.();
    }
  }, [hideModal, selectedUserId, transferOwner]);

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
        <SelectWithSearch
          value={selectedUserId}
          onChange={handleSelectedUserChange}
          options={candidates.map((user) => ({
            label: user.nickname || user.email,
            value: user.user_id,
          }))}
          placeholder={t('permission.selectMember')}
          disabled={candidates.length === 0}
        />
        <DialogFooter className="gap-2">
          <Button variant="outline" onClick={hideModal}>
            {t('common.cancel')}
          </Button>
          <ButtonLoading
            loading={loading}
            disabled={!selectedUserId}
            onClick={handleConfirm}
          >
            {t('common.confirm')}
          </ButtonLoading>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
