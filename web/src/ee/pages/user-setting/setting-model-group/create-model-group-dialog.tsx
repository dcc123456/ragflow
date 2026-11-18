import { DynamicForm, FormFieldType } from '@/components/dynamic-form';
import type {
  DynamicFormRef,
  FormFieldConfig,
} from '@/components/dynamic-form';
import { Button, ButtonLoading } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { useCreateModelGroup } from '@/ee/hooks/use-model-group-request';
import type { IModelGroup } from '@/ee/interfaces/database/model-group';
import { useCallback, useMemo, useRef } from 'react';
import type { FieldValues } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { z } from 'zod';

export function CreateModelGroupDialog({
  groups,
  hideModal,
}: {
  groups: IModelGroup[];
  hideModal: () => void;
}) {
  const { t } = useTranslation();
  const formRef = useRef<DynamicFormRef>(null);
  const submittingRef = useRef(false);
  const { loading, run } = useCreateModelGroup();
  const existingNames = useMemo(
    () => new Set(groups.map((group) => group.group_name)),
    [groups],
  );
  const fields = useMemo<FormFieldConfig[]>(
    () => [
      {
        name: 'name',
        label: t('setting.modelGroupPage.groupName'),
        type: FormFieldType.Text,
        autoComplete: 'off',
        schema: z
          .string()
          .trim()
          .min(1, t('setting.modelGroupPage.nameRequired'))
          .refine(
            (name) => !existingNames.has(name),
            t('setting.modelGroupPage.nameDuplicate'),
          ),
      },
    ],
    [existingNames, t],
  );

  const handleSubmit = useCallback(
    async (values: FieldValues) => {
      if (submittingRef.current) return;
      submittingRef.current = true;
      try {
        const code = await run({ name: String(values.name).trim() });
        if (code === 0) hideModal();
      } catch {
        // The request layer reports the error; keep the dialog open.
      } finally {
        submittingRef.current = false;
      }
    },
    [hideModal, run],
  );
  const submitForm = useCallback(() => formRef.current?.submit(), []);

  return (
    <Dialog open onOpenChange={hideModal}>
      <DialogContent className="sm:max-w-[425px]" aria-describedby={undefined}>
        <DialogHeader>
          <DialogTitle>{t('setting.modelGroupPage.createTitle')}</DialogTitle>
        </DialogHeader>
        <DynamicForm.Root
          ref={formRef}
          fields={fields}
          onSubmit={handleSubmit}
          defaultValues={{ name: '' }}
          resetOptions={{ keepDirtyValues: true }}
        />
        <DialogFooter>
          <Button variant="outline" onClick={hideModal}>
            {t('common.cancel')}
          </Button>
          <ButtonLoading loading={loading} onClick={submitForm}>
            {t('common.save')}
          </ButtonLoading>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
