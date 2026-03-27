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
import { IDepartment } from '@/interfaces/database/team';
import { useTranslation } from 'react-i18next';
import { CreateDepartmentForm } from './create-department-form';

export function CreateDepartmentDialog({
  hideModal,
  onOk,
  loading,
  initialValues,
}: IModalProps<any> & { initialValues?: Partial<IDepartment> }) {
  const { t } = useTranslation();

  return (
    <Dialog open onOpenChange={hideModal}>
      <DialogContent className="sm:max-w-[425px]">
        <DialogHeader>
          <DialogTitle>
            {initialValues?.name
              ? t('common.edit')
              : t('permission.createSubDepartment')}
          </DialogTitle>
        </DialogHeader>
        <CreateDepartmentForm
          hideModal={hideModal}
          onOk={onOk}
          initialValues={initialValues}
        ></CreateDepartmentForm>
        <DialogFooter>
          <ButtonLoading type="submit" form={TagRenameId} loading={loading}>
            {t('common.save')}
          </ButtonLoading>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
