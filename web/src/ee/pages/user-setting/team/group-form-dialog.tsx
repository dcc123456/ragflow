import {
  DynamicForm,
  DynamicFormRef,
  FormFieldConfig,
  FormFieldType,
} from '@/components/dynamic-form';
import { Button, ButtonLoading } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { ITenantGroup } from '@/ee/interfaces/database/team';
import { IModalProps } from '@/interfaces/common';
import { useCallback, useMemo, useRef } from 'react';
import { FieldValues } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { z } from 'zod';
import { useCreateTenantGroup, useUpdateTenantGroup } from './use-group-query';

type GroupFormDialogProps = IModalProps<unknown> & {
  group?: ITenantGroup;
  parentGroup?: ITenantGroup;
};

export function GroupFormDialog({
  group,
  parentGroup,
  hideModal,
}: GroupFormDialogProps) {
  const { t } = useTranslation();
  const editing = !!group;
  const formRef = useRef<DynamicFormRef>(null);
  const { loading: creating, createGroup } = useCreateTenantGroup();
  const { loading: updating, updateGroup } = useUpdateTenantGroup(
    group?.id ?? '',
  );

  const fields = useMemo<FormFieldConfig[]>(() => {
    const formFields: FormFieldConfig[] = [
      {
        name: 'name',
        label: t('common.name'),
        type: FormFieldType.Text,
        placeholder: t('common.namePlaceholder'),
        autoComplete: 'off',
        schema: z
          .string()
          .trim()
          .min(1, { message: t('common.namePlaceholder') })
          .max(100, { message: t('permission.groupNameLength') }),
      },
    ];

    formFields.push({
      name: 'description',
      label: t('permission.description'),
      type: FormFieldType.Textarea,
      placeholder: t('permission.descriptionPlaceholder'),
      fieldConfig: { rows: 3 },
    });

    return formFields;
  }, [t]);

  const defaultValues = useMemo(
    () => ({
      name: group?.name ?? '',
      description: group?.description ?? '',
    }),
    [group?.description, group?.name],
  );

  const onSubmit = useCallback(
    async (values: FieldValues) => {
      const name = String(values.name ?? '').trim();
      const description = String(values.description ?? '').trim();
      const payload = {
        name,
        description: description || undefined,
      };

      const code = editing
        ? await updateGroup(payload)
        : await createGroup({ ...payload, parent_id: parentGroup?.id });

      if (code !== 0) {
        return;
      }
      hideModal?.();
    },
    [createGroup, editing, hideModal, parentGroup?.id, updateGroup],
  );

  const handleSubmit = useCallback(() => {
    formRef.current?.submit();
  }, []);

  const title = editing
    ? t('common.edit')
    : parentGroup
      ? t('permission.createSubgroup')
      : t('permission.createGroup');

  return (
    <Dialog open onOpenChange={hideModal}>
      <DialogContent className="sm:max-w-[425px]">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          {parentGroup && (
            <p className="truncate text-sm text-text-secondary">
              {t('permission.parentGroup')}: {parentGroup.name}
            </p>
          )}
        </DialogHeader>
        <DynamicForm.Root
          ref={formRef}
          fields={fields}
          onSubmit={onSubmit}
          defaultValues={defaultValues}
          resetOptions={{ keepDirtyValues: true }}
        />
        <DialogFooter>
          <Button variant="outline" onClick={hideModal}>
            {t('common.cancel')}
          </Button>
          <ButtonLoading
            loading={editing ? updating : creating}
            onClick={handleSubmit}
          >
            {t('common.save')}
          </ButtonLoading>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
