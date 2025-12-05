import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { LoadingButton } from '@/components/ui/loading-button';
import { TagRenameId } from '@/constants/knowledge';
import { IModalProps } from '@/interfaces/common';
import { IGroup } from '@/interfaces/database/team';
import { useTranslation } from 'react-i18next';
import { PrivilegeAvatar } from '../privilege-avatar';
import { TransferOwnerForm } from './transfer-owner-form';

export function TransferOwnerDialog({
  hideModal,
  onOk,
  loading,
  initialValues,
}: IModalProps<any> & { initialValues?: Partial<IGroup> }) {
  const { t } = useTranslation();

  return (
    <Dialog open onOpenChange={hideModal}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('permission.transferOwner')}</DialogTitle>
        </DialogHeader>
        <div className="flex gap-2 items-center">
          <PrivilegeAvatar
            avatar={initialValues?.avatar}
            className="size-10"
          ></PrivilegeAvatar>
          {initialValues?.name}
        </div>
        <TransferOwnerForm
          hideModal={hideModal}
          onOk={onOk}
          initialValues={initialValues}
        ></TransferOwnerForm>
        <DialogFooter>
          <LoadingButton type="submit" form={TagRenameId} loading={loading}>
            {t('common.save')}
          </LoadingButton>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
