import { ButtonLoading } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { TagRenameId } from '@/constants/knowledge';
import { IModalProps } from '@/interfaces/common';
import { IGroup } from '@/interfaces/database/team';
import { useTranslation } from 'react-i18next';
import { CreateGroupForm } from './create-group-form';

export function CreateGroupDialog({
  hideModal,
  initialValues,
  onOk,
  loading,
}: IModalProps<any> & { initialValues?: IGroup }) {
  const { t } = useTranslation();

  return (
    <Dialog open onOpenChange={hideModal}>
      <DialogContent className="sm:max-w-[425px]">
        <DialogHeader>
          <DialogTitle>{t('common.edit')}</DialogTitle>
        </DialogHeader>
        <CreateGroupForm
          initialValues={initialValues}
          hideModal={hideModal}
          onOk={onOk}
        ></CreateGroupForm>
        <DialogFooter>
          <ButtonLoading type="submit" form={TagRenameId} loading={loading}>
            {t('common.save')}
          </ButtonLoading>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
