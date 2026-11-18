import { ConfirmDeleteDialog } from '@/components/confirm-delete-dialog';
import Empty from '@/components/empty/empty';
import { TruncatedText } from '@/components/truncated-text';
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion';
import { Button } from '@/components/ui/button';
import {
  useDeleteModelGroup,
  useModelGroupMembers,
  useRemoveModelGroupMember,
} from '@/ee/hooks/use-model-group-request';
import type {
  IModelGroup,
  IModelGroupMember,
} from '@/ee/interfaces/database/model-group';
import { ModelGroupActiveStatus } from '@/ee/utils/model-group-util';
import { buildModelValue } from '@/utils/llm-util';
import { ChevronDown, Loader2, Plus, Trash2 } from 'lucide-react';
import { useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { AddModelToGroupDialog } from './add-model-to-group-dialog';

const MODEL_GROUP_MEMBER_COLUMNS = [
  {
    key: 'model',
    label: 'setting.modelGroupPage.modelName',
    className: 'w-[28%] px-3 py-2 font-medium',
  },
  {
    key: 'modelType',
    label: 'setting.modelGroupPage.modelType',
    className: 'w-[20%] px-3 py-2 font-medium',
  },
  {
    key: 'instance',
    label: 'setting.modelGroupPage.instanceName',
    className: 'w-[20%] px-3 py-2 font-medium',
  },
  {
    key: 'position',
    label: 'setting.modelGroupPage.position',
    className: 'w-20 px-3 py-2 font-medium',
  },
  {
    key: 'status',
    label: 'setting.modelGroupPage.status',
    className: 'w-20 px-3 py-2 font-medium',
  },
  {
    key: 'actions',
    label: 'common.delete',
    className: 'w-12 px-2 py-2',
  },
] as const;

function ModelGroupMemberRow({
  member,
  removeLoading,
  onRemove,
}: {
  member: IModelGroupMember;
  removeLoading: boolean;
  onRemove: (member: IModelGroupMember) => Promise<void>;
}) {
  const { t } = useTranslation();
  const modelTypes = (member.model_type ?? []).join(', ');
  const handleRemove = useCallback(() => onRemove(member), [member, onRemove]);

  return (
    <tr className="group border-t border-border-button">
      <td className="px-3 py-2">
        <TruncatedText
          as="span"
          className="block truncate"
          tooltip={member.model}
        >
          {member.model}
        </TruncatedText>
      </td>
      <td className="px-3 py-2">
        <TruncatedText
          as="span"
          className="block truncate"
          tooltip={modelTypes}
        >
          {modelTypes}
        </TruncatedText>
      </td>
      <td className="px-3 py-2">
        <TruncatedText
          as="span"
          className="block truncate"
          tooltip={member.instance}
        >
          {member.instance}
        </TruncatedText>
      </td>
      <td className="px-3 py-2">{member.position}</td>
      <td className="px-3 py-2">{member.status}</td>
      <td className="w-12 px-2 py-2">
        <ConfirmDeleteDialog
          title={t('setting.modelGroupPage.removeMemberTitle')}
          content={{ title: member.model }}
          onOk={handleRemove}
          confirmButtonTestId={`confirm-remove-${member.id}`}
        >
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100"
            disabled={removeLoading}
            aria-label={t('common.delete')}
          >
            <Trash2 />
          </Button>
        </ConfirmDeleteDialog>
      </td>
    </tr>
  );
}

function ModelGroupRow({
  group,
  defaultExpanded,
}: {
  group: IModelGroup;
  defaultExpanded: boolean;
}) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(defaultExpanded);
  const [addOpen, setAddOpen] = useState(false);
  const {
    data: members = [],
    loading,
    isError,
    refetch,
  } = useModelGroupMembers(group.group_name, expanded);
  const { loading: deleteLoading, run: deleteGroup } = useDeleteModelGroup(
    group.group_name,
  );
  const { loading: removeLoading, run: removeMember } =
    useRemoveModelGroupMember(group.group_name);

  const handleAccordionChange = useCallback(
    (value: string) => setExpanded(value === group.id),
    [group.id],
  );
  const showAddDialog = useCallback(() => setAddOpen(true), []);
  const hideAddDialog = useCallback(() => setAddOpen(false), []);
  const handleRetryMembers = useCallback(() => {
    void refetch();
  }, [refetch]);

  const handleDelete = useCallback(async () => {
    if (deleteLoading) return;
    try {
      await deleteGroup(undefined);
    } catch {
      // The request layer reports the error; retain the group row.
    }
  }, [deleteGroup, deleteLoading]);

  const handleRemoveMember = useCallback(
    async (member: IModelGroupMember) => {
      if (removeLoading) return;
      const compositeModelName = buildModelValue({
        model_name: member.model,
        model_instance: member.instance,
        model_provider: member.provider,
      });
      try {
        await removeMember({ composite_model_name: compositeModelName });
      } catch {
        // The request layer reports the error; retain the member row.
      }
    },
    [removeLoading, removeMember],
  );

  return (
    <div className="min-w-0 rounded-md border border-border-button">
      <Accordion
        type="single"
        collapsible
        value={expanded ? group.id : ''}
        onValueChange={handleAccordionChange}
      >
        <AccordionItem value={group.id} className="border-b-0">
          <div className="group flex w-full justify-between items-center gap-2 pr-2">
            <div className="min-w-0 flex-1">
              <AccordionTrigger
                hideDownIcon
                className="min-w-0 items-center px-3 hover:no-underline"
              >
                <ChevronDown
                  aria-hidden="true"
                  className="text-muted-foreground pointer-events-none size-4 shrink-0 transition-transform duration-200"
                />

                <span className="flex min-w-0 flex-1 items-center ">
                  <div className="flex min-w-0 items-center gap-1">
                    <TruncatedText
                      as="span"
                      className="block min-w-0 flex-1 truncate"
                      tooltip={group.group_name}
                    >
                      {group.group_name}
                    </TruncatedText>
                    <span
                      role="img"
                      aria-label={group.status}
                      title={group.status}
                      className={`size-1 shrink-0 rounded-full ${
                        group.status === ModelGroupActiveStatus
                          ? 'bg-state-success'
                          : 'bg-state-error'
                      }`}
                    />
                  </div>
                </span>
              </AccordionTrigger>
            </div>
            <div className="group flex shrink-0 items-center gap-2">
              {expanded && (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={loading || isError}
                  onClick={showAddDialog}
                >
                  <Plus />
                  {t('setting.modelGroupPage.addMember')}
                </Button>
              )}
              <ConfirmDeleteDialog
                title={t('setting.modelGroupPage.deleteGroupTitle')}
                content={{
                  title: group.group_name,
                  node: t('setting.modelGroupPage.deleteGroupDescription'),
                }}
                onOk={handleDelete}
                confirmButtonTestId={`confirm-delete-group-${group.id}`}
              >
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className="opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100"
                  disabled={deleteLoading}
                  aria-label={t('common.delete')}
                >
                  <Trash2 />
                </Button>
              </ConfirmDeleteDialog>
            </div>
          </div>
          <AccordionContent className="px-3">
            {loading ? (
              <div role="status" className="flex justify-center py-4">
                <Loader2
                  className="animate-spin"
                  aria-label={t('setting.modelGroupPage.loading')}
                />
              </div>
            ) : isError ? (
              <div
                role="alert"
                className="flex items-center justify-center gap-3 py-4"
              >
                <span className="text-sm text-state-error">
                  {t('setting.modelGroupPage.membersLoadFailed')}
                </span>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={handleRetryMembers}
                >
                  {t('setting.modelGroupPage.retry')}
                </Button>
              </div>
            ) : members.length === 0 ? (
              <Empty className="py-4" />
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full table-fixed text-left text-sm">
                  <thead>
                    <tr>
                      {MODEL_GROUP_MEMBER_COLUMNS.map((column) => {
                        const label = t(column.label);

                        return (
                          <th
                            key={column.key}
                            scope="col"
                            className={column.className}
                          >
                            {column.key === 'actions' ? (
                              <span className="sr-only">{label}</span>
                            ) : (
                              <TruncatedText
                                as="span"
                                className="block truncate"
                                tooltip={label}
                              >
                                {label}
                              </TruncatedText>
                            )}
                          </th>
                        );
                      })}
                    </tr>
                  </thead>
                  <tbody>
                    {members.map((member) => (
                      <ModelGroupMemberRow
                        key={member.id}
                        member={member}
                        removeLoading={removeLoading}
                        onRemove={handleRemoveMember}
                      />
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </AccordionContent>
        </AccordionItem>
      </Accordion>
      {addOpen && (
        <AddModelToGroupDialog
          groupName={group.group_name}
          members={members}
          hideModal={hideAddDialog}
        />
      )}
    </div>
  );
}

export function ModelGroupList({ groups }: { groups: IModelGroup[] }) {
  return (
    <div className="flex min-w-0 min-h-0 flex-1 flex-col gap-2 overflow-y-auto">
      {groups.map((group, index) => (
        <ModelGroupRow
          key={group.id}
          group={group}
          defaultExpanded={index === 0}
        />
      ))}
    </div>
  );
}
