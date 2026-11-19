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
import { PrivilegeManagementDialog } from '@/ee/components/privilege-management/privilege-management-dialog';
import { Permission, PermissionResourceType } from '@/ee/constants/team';
import type { IPrivilegeManagementInitialValues } from '@/ee/components/privilege-management/interface';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import {
  useDeleteModelGroup,
  useModelGroupMembers,
  useReorderModelGroupMembers,
  useRemoveModelGroupMember,
  useUpdateModelGroupMemberStatus,
} from '@/ee/hooks/use-model-group-request';
import type {
  IModelGroup,
  IModelGroupMember,
} from '@/ee/interfaces/database/model-group';
import { ModelGroupActiveStatus } from '@/ee/utils/model-group-util';
import { useFetchUserInfo } from '@/hooks/use-user-setting-request';
import { buildModelValue } from '@/utils/llm-util';
import {
  ChevronDown,
  GripVertical,
  KeyRound,
  Loader2,
  Plus,
  Trash2,
} from 'lucide-react';
import { useCallback, useState, type DragEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { AddModelToGroupDialog } from './add-model-to-group-dialog';

const MODEL_GROUP_MEMBER_COLUMNS = [
  {
    key: 'drag',
    label: 'setting.modelGroupPage.reorderModel',
    className: 'w-[2.5%] px-0 py-2',
  },
  {
    key: 'provider',
    label: 'setting.modelGroupPage.providerName',
    className: 'w-[20.5%] px-3 py-2 font-medium',
  },
  {
    key: 'instance',
    label: 'setting.modelGroupPage.instanceName',
    className: 'w-[17%] px-3 py-2 font-medium',
  },
  {
    key: 'model',
    label: 'setting.modelGroupPage.modelName',
    className: 'w-[25%] px-3 py-2 font-medium',
  },
  {
    key: 'modelType',
    label: 'setting.modelGroupPage.modelType',
    className: 'w-[19%] px-3 py-2 font-medium',
  },
  {
    key: 'status',
    label: 'setting.modelGroupPage.status',
    className: 'w-[10%] px-2 py-2 font-medium',
  },
  {
    key: 'actions',
    label: 'common.action',
    className: 'w-[6%] px-1 py-2 text-center font-medium',
  },
] as const;

function ModelGroupMemberRow({
  member,
  removeLoading,
  reorderLoading,
  canManageGroup,
  hasRestrictedMembers,
  dragging,
  onStartDrag,
  onDragOver,
  onDrop,
  onEndDrag,
  onRemove,
  statusLoading,
  onStatusChange,
}: {
  member: IModelGroupMember;
  removeLoading: boolean;
  reorderLoading: boolean;
  canManageGroup: boolean;
  hasRestrictedMembers: boolean;
  dragging: boolean;
  onStartDrag: (memberId: string) => void;
  onDragOver: (memberId: string, event: DragEvent<HTMLTableRowElement>) => void;
  onDrop: (memberId: string, event: DragEvent<HTMLTableRowElement>) => void;
  onEndDrag: () => void;
  onRemove: (member: IModelGroupMember) => Promise<void>;
  statusLoading: boolean;
  onStatusChange: (member: IModelGroupMember, status: string) => Promise<void>;
}) {
  const { t } = useTranslation();
  const modelTypes = (member.model_type ?? []).join(', ');
  const handleRemove = useCallback(() => onRemove(member), [member, onRemove]);
  const handleStatusChange = useCallback(
    (status: string) => onStatusChange(member, status),
    [member, onStatusChange],
  );

  return (
    <tr
      className={`group h-[52px] border-t border-border-button ${dragging ? 'opacity-50' : ''}`}
      onDragOver={(event) => onDragOver(member.id, event)}
      onDrop={(event) => onDrop(member.id, event)}
    >
      <td className="px-0 py-2 text-center">
        {canManageGroup && (
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            className="cursor-grab text-text-disabled active:cursor-grabbing"
            aria-label={t('setting.modelGroupPage.reorderModel')}
            title={t('setting.modelGroupPage.reorderModel')}
            draggable={!reorderLoading && !hasRestrictedMembers}
            disabled={reorderLoading || hasRestrictedMembers}
            onDragStart={(event) => {
              event.dataTransfer.setData('text/plain', member.id);
              event.dataTransfer.effectAllowed = 'move';
              onStartDrag(member.id);
            }}
            onDragEnd={onEndDrag}
          >
            <GripVertical aria-hidden="true" />
          </Button>
        )}
      </td>
      <td className="px-3 py-2">
        {!member.restricted && (
          <TruncatedText
            as="span"
            className="block min-w-0 truncate"
            tooltip={member.provider}
          >
            {member.provider}
          </TruncatedText>
        )}
      </td>
      <td className="px-3 py-2">
        {!member.restricted && (
          <TruncatedText
            as="span"
            className="block truncate"
            tooltip={member.instance}
          >
            {member.instance}
          </TruncatedText>
        )}
      </td>
      <td className="px-3 py-2">
        <div className="flex min-w-0 items-center gap-2">
          {member.restricted ? (
            <span className="text-xs text-text-secondary">
              {t('setting.modelGroupPage.memberNoAccess')}
            </span>
          ) : (
            <>
              <TruncatedText
                as="span"
                className="block min-w-0 truncate"
                tooltip={member.model}
              >
                {member.model}
              </TruncatedText>
              {member.unavailable && (
                <span className="shrink-0 text-xs text-state-error">
                  {t('setting.modelGroupPage.memberUnavailable')}
                </span>
              )}
            </>
          )}
        </div>
      </td>
      <td className="px-3 py-2">
        {!member.restricted && (
          <TruncatedText
            as="span"
            className="block truncate"
            tooltip={modelTypes}
          >
            {modelTypes}
          </TruncatedText>
        )}
      </td>
      <td className="px-2 py-2">
        {member.restricted ? (
          <span className="px-2 text-text-secondary">—</span>
        ) : canManageGroup ? (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                type="button"
                variant="ghost"
                className="h-8 w-fit max-w-[140px] justify-start truncate px-2"
                disabled={
                  statusLoading ||
                  member.unavailable === true ||
                  member.restricted === true
                }
                aria-label={t('setting.modelGroupPage.status')}
              >
                {member.status}
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent>
              <DropdownMenuItem
                onClick={() => handleStatusChange(ModelGroupActiveStatus)}
              >
                active
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => handleStatusChange('inactive')}>
                inactive
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        ) : (
          <span className="px-2">{member.status}</span>
        )}
      </td>
      <td className="px-1 py-2 text-center">
        {canManageGroup && (
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
              disabled={removeLoading || member.restricted === true}
              aria-label={t('common.delete')}
            >
              <Trash2 />
            </Button>
          </ConfirmDeleteDialog>
        )}
      </td>
    </tr>
  );
}

function ModelGroupRow({
  group,
  currentUserId,
  defaultExpanded,
}: {
  group: IModelGroup;
  currentUserId: string;
  defaultExpanded: boolean;
}) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(defaultExpanded);
  const [addOpen, setAddOpen] = useState(false);
  const [permissionTarget, setPermissionTarget] =
    useState<IPrivilegeManagementInitialValues | null>(null);
  const [draggedMemberId, setDraggedMemberId] = useState<string | null>(null);
  const [optimisticOrder, setOptimisticOrder] = useState<{
    source: IModelGroupMember[];
    members: IModelGroupMember[];
  } | null>(null);
  const {
    data: members = [],
    loading,
    isError,
    refetch,
  } = useModelGroupMembers(group.id, expanded);
  const { loading: deleteLoading, run: deleteGroup } = useDeleteModelGroup(
    group.id,
  );
  const { loading: removeLoading, run: removeMember } =
    useRemoveModelGroupMember(group.id);
  const { loading: reorderLoading, run: reorderMembers } =
    useReorderModelGroupMembers(group.id);
  const displayedMembers =
    optimisticOrder?.source === members ? optimisticOrder.members : members;
  const hasRestrictedMembers = displayedMembers.some(
    (member) => member.restricted,
  );
  const { loading: statusLoading, run: updateMemberStatus } =
    useUpdateModelGroupMemberStatus(group.id);
  const canManageGroup =
    ((group.operator_permission ?? 0) &
      (Permission.Manage | Permission.Owner)) !==
    0;

  const handleAccordionChange = useCallback(
    (value: string) => setExpanded(value === group.id),
    [group.id],
  );
  const showAddDialog = useCallback(() => setAddOpen(true), []);
  const hideAddDialog = useCallback(() => setAddOpen(false), []);
  const showPermissionDialog = useCallback(() => {
    setPermissionTarget({
      id: group.id,
      tenant_id: group.tenant_id,
      name: group.group_name,
      operator_permission: group.operator_permission,
      resourceType: PermissionResourceType.ModelGroup,
    });
  }, [group.group_name, group.id, group.operator_permission, group.tenant_id]);
  const hidePermissionDialog = useCallback(() => setPermissionTarget(null), []);
  const handleDragOver = useCallback(
    (memberId: string, event: DragEvent<HTMLTableRowElement>) => {
      if (draggedMemberId && draggedMemberId !== memberId) {
        event.preventDefault();
        event.dataTransfer.dropEffect = 'move';
      }
    },
    [draggedMemberId],
  );
  const handleDrop = useCallback(
    async (targetMemberId: string, event: DragEvent<HTMLTableRowElement>) => {
      event.preventDefault();
      const sourceMemberId = draggedMemberId;
      setDraggedMemberId(null);
      if (
        !sourceMemberId ||
        sourceMemberId === targetMemberId ||
        reorderLoading
      ) {
        return;
      }
      const sourceIndex = displayedMembers.findIndex(
        (member) => member.id === sourceMemberId,
      );
      const targetIndex = displayedMembers.findIndex(
        (member) => member.id === targetMemberId,
      );
      if (sourceIndex < 0 || targetIndex < 0) return;

      const nextMembers = [...displayedMembers];
      const [movedMember] = nextMembers.splice(sourceIndex, 1);
      nextMembers.splice(targetIndex, 0, movedMember);
      setOptimisticOrder({ source: members, members: nextMembers });
      try {
        const code = await reorderMembers({
          model_ids: nextMembers.map((member) => member.model_id),
        });
        if (code !== 0) setOptimisticOrder(null);
      } catch {
        setOptimisticOrder(null);
      }
    },
    [
      displayedMembers,
      draggedMemberId,
      members,
      reorderMembers,
      reorderLoading,
    ],
  );
  const handleMemberStatusChange = useCallback(
    async (member: IModelGroupMember, status: string) => {
      if (member.unavailable) return;
      const compositeModelName = buildModelValue({
        model_name: member.model,
        model_instance: member.instance,
        model_provider: member.provider,
      });
      try {
        await updateMemberStatus({
          composite_model_name: compositeModelName,
          status,
        });
      } catch {
        // The request layer reports the error; the query retains the old status.
      }
    },
    [updateMemberStatus],
  );
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
      if (member.unavailable) {
        try {
          await removeMember({ model_id: member.model_id });
        } catch {
          // The request layer reports the error; retain the member row.
        }
        return;
      }
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
                    {group.user_id &&
                      group.user_id !== currentUserId &&
                      group.nickname && (
                        <TruncatedText
                          as="span"
                          className="block max-w-[120px] shrink-0 truncate text-xs text-text-secondary"
                          tooltip={group.nickname}
                        >
                          {group.nickname}
                        </TruncatedText>
                      )}
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
              {expanded && canManageGroup && (
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
              {canManageGroup && (
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  aria-label={t('permission.permissionManagement')}
                  title={t('permission.permissionManagement')}
                  onClick={showPermissionDialog}
                >
                  <KeyRound />
                </Button>
              )}
              {canManageGroup && (
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
                    <Trash2 className="text-state-error" />
                  </Button>
                </ConfirmDeleteDialog>
              )}
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
            ) : displayedMembers.length === 0 ? (
              <Empty className="py-4" />
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full min-w-[960px] table-fixed text-left text-sm">
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
                            {column.key === 'drag' ? (
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
                    {displayedMembers.map((member) => (
                      <ModelGroupMemberRow
                        key={member.id}
                        member={member}
                        removeLoading={removeLoading}
                        reorderLoading={reorderLoading}
                        canManageGroup={canManageGroup}
                        hasRestrictedMembers={hasRestrictedMembers}
                        dragging={draggedMemberId === member.id}
                        onStartDrag={setDraggedMemberId}
                        onDragOver={handleDragOver}
                        onDrop={handleDrop}
                        onEndDrag={() => setDraggedMemberId(null)}
                        onRemove={handleRemoveMember}
                        statusLoading={statusLoading}
                        onStatusChange={handleMemberStatusChange}
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
          groupId={group.id}
          members={displayedMembers}
          hideModal={hideAddDialog}
        />
      )}
      {permissionTarget && (
        <PrivilegeManagementDialog
          hideModal={hidePermissionDialog}
          initialValues={permissionTarget}
        />
      )}
    </div>
  );
}

export function ModelGroupList({ groups }: { groups: IModelGroup[] }) {
  const { data: userInfo } = useFetchUserInfo();
  const currentUserId = userInfo?.id ?? '';

  return (
    <div className="flex min-w-0 min-h-0 flex-1 flex-col gap-2 overflow-y-auto">
      {groups.map((group, index) => (
        <ModelGroupRow
          key={group.id}
          group={group}
          currentUserId={currentUserId}
          defaultExpanded={index === 0}
        />
      ))}
    </div>
  );
}
