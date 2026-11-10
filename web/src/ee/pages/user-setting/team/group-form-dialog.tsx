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
import {
  useCreateTenantGroup,
  useMoveTenantGroup,
  useTenantGroups,
  useUpdateTenantGroup,
} from './use-group-query';

const ROOT_PARENT = '__root__';

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
  const { data: groups } = useTenantGroups();
  const { loading: creating, createGroup } = useCreateTenantGroup();
  const { loading: updating, updateGroup } = useUpdateTenantGroup(
    group?.id ?? '',
  );
  const { loading: moving, moveGroup } = useMoveTenantGroup(group?.id ?? '');

  const parentCandidates = useMemo(() => {
    if (!group) {
      return [];
    }

    const childrenMap = new Map<string, string[]>();

    groups.forEach((item) => {
      if (!item.parent_id) {
        return;
      }

      const children = childrenMap.get(item.parent_id) ?? [];
      children.push(item.id);
      childrenMap.set(item.parent_id, children);
    });

    const descendants = new Set<string>();
    const collectDescendants = (id: string) => {
      (childrenMap.get(id) ?? []).forEach((childId) => {
        descendants.add(childId);
        collectDescendants(childId);
      });
    };
    collectDescendants(group.id);

    return groups.filter(
      (item) => item.id !== group.id && !descendants.has(item.id),
    );
  }, [group, groups]);

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

    if (editing) {
      formFields.push({
        name: 'parentId',
        label: t('permission.parentGroup'),
        type: FormFieldType.Select,
        options: [
          { label: t('permission.rootGroup'), value: ROOT_PARENT },
          ...parentCandidates.map((item) => ({
            label: item.name || item.id,
            value: item.id,
          })),
        ],
      });
    }

    formFields.push({
      name: 'description',
      label: t('permission.description'),
      type: FormFieldType.Textarea,
      placeholder: t('permission.descriptionPlaceholder'),
      fieldConfig: { rows: 3 },
    });

    return formFields;
  }, [editing, parentCandidates, t]);

  const defaultValues = useMemo(
    () => ({
      name: group?.name ?? '',
      description: group?.description ?? '',
      parentId: group?.parent_id || ROOT_PARENT,
    }),
    [group?.description, group?.name, group?.parent_id],
  );

  const onSubmit = useCallback(
    async (values: FieldValues) => {
      const name = String(values.name ?? '').trim();
      const description = String(values.description ?? '').trim();
      const parentId = String(values.parentId ?? ROOT_PARENT);
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

      if (editing && parentId !== (group?.parent_id || ROOT_PARENT)) {
        const moveCode = await moveGroup(
          parentId === ROOT_PARENT ? null : parentId,
        );

        if (moveCode !== 0) {
          return;
        }
      }

      hideModal?.();
    },
    [
      createGroup,
      editing,
      group?.parent_id,
      hideModal,
      moveGroup,
      parentGroup?.id,
      updateGroup,
    ],
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
            loading={editing ? updating || moving : creating}
            onClick={handleSubmit}
          >
            {t('common.save')}
          </ButtonLoading>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
