import {
  ConfirmDeleteDialog,
  ConfirmDeleteDialogNode,
} from '@/components/confirm-delete-dialog';
import { RAGFlowAvatar } from '@/components/ragflow-avatar';
import { TruncatedText } from '@/components/truncated-text';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { SelectWithSearch } from '@/components/originui/select-with-search';
import {
  GroupMemberStatus,
  GroupPermission,
  GroupPermissionName,
  hasResourcePermission,
} from '@/ee/constants/team';
import {
  ITenantGroup,
  ITenantGroupMember,
} from '@/ee/interfaces/database/team';
import {
  useFetchTenantInfo,
  useFetchUserInfo,
  useListTenantUser,
} from '@/hooks/use-user-setting-request';
import { IModalProps } from '@/interfaces/common';
import { TenantMembershipStatus } from '@/pages/user-setting/constants';
import { ArrowLeftRight, ShieldCheck, Trash2 } from 'lucide-react';
import { useCallback, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  type GrantablePermission,
  grantableGroupPermissions,
} from './group-permissions';
import {
  useAddTenantGroupMember,
  useRemoveTenantGroupMember,
  useSetTenantGroupMemberPermissions,
  useTenantGroupMembers,
} from './use-group-query';
import { TransferOwnerDialog } from './transfer-owner-dialog';

function PermissionOption({
  permission,
  checked,
  onToggle,
}: {
  permission: GrantablePermission;
  checked: boolean;
  onToggle: (value: GroupPermissionName, checked: boolean) => void;
}) {
  const { t } = useTranslation();

  const handleCheckedChange = useCallback(
    (value: boolean | 'indeterminate') => {
      onToggle(permission.value, value === true);
    },
    [onToggle, permission.value],
  );

  return (
    <label
      className={`flex min-h-12 cursor-pointer items-center gap-3 rounded-lg border px-3 py-2.5 transition-colors focus-within:ring-2 focus-within:ring-accent-primary/20 ${
        checked
          ? 'border-accent-primary/50 bg-accent-primary/5'
          : 'border-border-button bg-bg-base hover:border-accent-primary/30 hover:bg-bg-card'
      }`}
    >
      <Checkbox
        className="shrink-0"
        checked={checked}
        onCheckedChange={handleCheckedChange}
      />
      <span
        className={`text-sm ${
          checked ? 'font-medium text-text-primary' : 'text-text-secondary'
        }`}
      >
        {t(permission.labelKey)}
      </span>
    </label>
  );
}

function MemberRow({
  member,
  availablePermissions,
  canRemove,
  canConfigurePermissions,
  canTransferOwner,
  currentUserId,
  savingPermissions,
  onSavePermissions,
  onRemove,
  onTransferOwner,
}: {
  member: ITenantGroupMember;
  availablePermissions: GrantablePermission[];
  canRemove: boolean;
  canConfigurePermissions: boolean;
  canTransferOwner: boolean;
  currentUserId: string;
  savingPermissions: boolean;
  onSavePermissions: (
    userId: string,
    permissions: GroupPermissionName[],
  ) => Promise<number | undefined>;
  onRemove: (userId: string) => Promise<void>;
  onTransferOwner: (member: ITenantGroupMember) => void;
}) {
  const { t } = useTranslation();
  const [editing, setEditing] = useState(false);

  const displayName = member.nickname || member.email;
  const isTenantLeft = member.tenant_status === TenantMembershipStatus.Left;
  const editable =
    !member.is_owner && !isTenantLeft && member.user_id !== currentUserId;
  const showPermissionButton = canConfigurePermissions && editable;
  const showRemoveButton = canRemove && !member.is_owner && !isTenantLeft;
  const showTransferOwnerButton =
    canTransferOwner && !member.is_owner && !isTenantLeft;

  const handleToggleEditing = useCallback(() => {
    setEditing((previous) => !previous);
  }, []);

  const handleRemove = useCallback(async () => {
    await onRemove(member.user_id);
  }, [member.user_id, onRemove]);

  const handleTransferOwner = useCallback(() => {
    onTransferOwner(member);
  }, [member, onTransferOwner]);

  return (
    <div className="border-b border-border-button py-3 last:border-b-0">
      <div className="flex items-center gap-3">
        <RAGFlowAvatar
          isPerson
          className="size-7"
          name={displayName}
          avatar={member.avatar}
        />
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-2">
            <TruncatedText
              as="span"
              className="min-w-0 truncate text-sm text-text-primary"
              tooltip={displayName}
            >
              {displayName}
            </TruncatedText>
            {member.is_owner && (
              <span className="rounded-full bg-accent-primary/10 px-2 py-0.5 text-xs text-accent-primary">
                {t('permission.owner')}
              </span>
            )}
            {isTenantLeft && (
              <Badge variant="secondary" className="shrink-0">
                {t('permission.left')}
              </Badge>
            )}
            {member.status === GroupMemberStatus.Invited && (
              <span className="rounded-full bg-bg-input px-2 py-0.5 text-xs text-text-secondary">
                {t('permission.pending')}
              </span>
            )}
          </div>
          <div className="truncate text-xs text-text-secondary">
            {member.email}
          </div>
        </div>
        <div className="flex items-center gap-1">
          {showPermissionButton && (
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={handleToggleEditing}
              aria-label={t('permission.memberPermissions')}
              title={t('permission.memberPermissions')}
            >
              <ShieldCheck />
            </Button>
          )}
          {showTransferOwnerButton && (
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={handleTransferOwner}
              aria-label={t('permission.transferOwner')}
              title={t('permission.transferOwner')}
            >
              <ArrowLeftRight />
            </Button>
          )}
          {showRemoveButton && (
            <ConfirmDeleteDialog
              onOk={handleRemove}
              title={t('permission.removeMember')}
              content={{
                node: (
                  <ConfirmDeleteDialogNode
                    avatar={{ avatar: member.avatar, name: displayName }}
                    name={displayName}
                  />
                ),
              }}
            >
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={t('permission.removeMember')}
                title={t('permission.removeMember')}
              >
                <Trash2 />
              </Button>
            </ConfirmDeleteDialog>
          )}
        </div>
      </div>
      {editing && (
        <MemberPermissionEditor
          member={member}
          availablePermissions={availablePermissions}
          saving={savingPermissions}
          onSave={onSavePermissions}
          onClose={handleToggleEditing}
        />
      )}
    </div>
  );
}

function MemberPermissionEditor({
  member,
  availablePermissions,
  saving,
  onSave,
  onClose,
}: {
  member: ITenantGroupMember;
  availablePermissions: GrantablePermission[];
  saving: boolean;
  onSave: (
    userId: string,
    permissions: GroupPermissionName[],
  ) => Promise<number | undefined>;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const options = useMemo(() => {
    const optionsByValue = new Map(
      availablePermissions.map((option) => [option.value, option]),
    );

    // Keep grants the editor's user cannot hand out themselves visible, so
    // saving the form never revokes a permission it did not show.
    grantableGroupPermissions(member.permission).forEach((option) => {
      if (!optionsByValue.has(option.value)) {
        optionsByValue.set(option.value, option);
      }
    });

    return Array.from(optionsByValue.values());
  }, [availablePermissions, member.permission]);
  const [selected, setSelected] = useState<GroupPermissionName[]>(() =>
    options
      .filter((option) => hasResourcePermission(member.permission, option.mask))
      .map((option) => option.value),
  );

  const handleToggle = useCallback(
    (value: GroupPermissionName, checked: boolean) => {
      setSelected((previous) =>
        checked
          ? [...previous, value]
          : previous.filter((permission) => permission !== value),
      );
    },
    [],
  );

  const handleSave = useCallback(async () => {
    const code = await onSave(member.user_id, selected);

    if (code === 0) {
      onClose();
    }
  }, [member.user_id, onClose, onSave, selected]);

  return (
    <div className="mt-3 overflow-hidden rounded-xl border border-border-button bg-bg-base shadow-sm">
      <div className="flex items-center gap-2 border-b border-border-button px-4 py-3">
        <ShieldCheck className="size-4 text-accent-primary" />
        <span className="text-sm font-medium text-text-primary">
          {t('permission.memberPermissions')}
        </span>
      </div>
      <div className="grid gap-2 p-3 sm:grid-cols-2">
        {options.map((permission) => (
          <PermissionOption
            key={permission.value}
            permission={permission}
            checked={selected.includes(permission.value)}
            onToggle={handleToggle}
          />
        ))}
      </div>
      <div className="flex items-center justify-end gap-2 border-t border-border-button bg-bg-card px-4 py-3">
        <Button variant="ghost" size="sm" onClick={onClose}>
          {t('common.cancel')}
        </Button>
        <Button size="sm" loading={saving} onClick={handleSave}>
          {t('common.save')}
        </Button>
      </div>
    </div>
  );
}

export function GroupMembersDialog({
  group,
  hideModal,
}: IModalProps<unknown> & { group: ITenantGroup }) {
  const { t } = useTranslation();
  const { data: tenantInfo } = useFetchTenantInfo();
  const { data: userInfo } = useFetchUserInfo();
  const { data: members, loading } = useTenantGroupMembers(group.id, true);
  const { data: parentMembers, loading: loadingParentMembers } =
    useTenantGroupMembers(group.parent_id ?? '', !!group.parent_id);
  const { data: tenantUsers, loading: loadingTenantUsers } = useListTenantUser(
    tenantInfo.tenant_id,
    true,
  );
  const { loading: adding, addMember } = useAddTenantGroupMember(group.id);
  const { removeMember } = useRemoveTenantGroupMember(group.id);
  const { loading: savingPermissions, setMemberPermissions } =
    useSetTenantGroupMemberPermissions(group.id);
  const [selectedUserId, setSelectedUserId] = useState('');
  const [memberToTransfer, setMemberToTransfer] =
    useState<ITenantGroupMember | null>(null);

  const canAddMember = hasResourcePermission(
    group.permission,
    GroupPermission.AddMember,
  );
  const canRemoveMember = hasResourcePermission(
    group.permission,
    GroupPermission.RemoveMember,
  );
  const canConfigurePermissions = hasResourcePermission(
    group.permission,
    GroupPermission.ConfigureMemberPermissions,
  );
  const canTransferOwner = hasResourcePermission(
    group.permission,
    GroupPermission.Owner,
  );
  const grantablePermissions = useMemo(
    () => grantableGroupPermissions(group.permission),
    [group.permission],
  );

  const invitableUsers = useMemo(() => {
    const sourceMembers = group.parent_id
      ? parentMembers.filter(
          (member) =>
            member.status === GroupMemberStatus.Joined &&
            member.tenant_status === TenantMembershipStatus.Joined,
        )
      : tenantUsers.filter(
          (user) => user.status === TenantMembershipStatus.Joined,
        );

    return sourceMembers.filter(
      (user) =>
        user.user_id &&
        !members.some(
          (member) =>
            member.user_id === user.user_id &&
            member.status === GroupMemberStatus.Joined,
        ),
    );
  }, [group.parent_id, members, parentMembers, tenantUsers]);

  const loadingInvitableUsers = group.parent_id
    ? loadingParentMembers
    : loadingTenantUsers;

  // Explain why the root-group picker is empty when Tenant invitations are pending.
  const pendingTenantUsers = useMemo(() => {
    if (group.parent_id) {
      return [];
    }

    return tenantUsers.filter(
      (user) =>
        user.user_id &&
        user.status === TenantMembershipStatus.Invited &&
        !members.some((member) => member.user_id === user.user_id),
    );
  }, [group.parent_id, members, tenantUsers]);

  const handleSelectedUserChange = useCallback((value: string) => {
    setSelectedUserId(value);
  }, []);

  const handleAddMember = useCallback(async () => {
    if (!selectedUserId) {
      return;
    }

    const code = await addMember(selectedUserId);

    if (code === 0) {
      setSelectedUserId('');
    }
  }, [addMember, selectedUserId]);

  const handleSavePermissions = useCallback(
    async (userId: string, memberPermissions: GroupPermissionName[]) => {
      return await setMemberPermissions({
        userId,
        permissions: memberPermissions,
      });
    },
    [setMemberPermissions],
  );

  const handleRemoveMember = useCallback(
    async (userId: string) => {
      await removeMember(userId);
    },
    [removeMember],
  );

  const handleHideTransferOwner = useCallback(() => {
    setMemberToTransfer(null);
  }, []);

  return (
    <Dialog open onOpenChange={hideModal}>
      <DialogContent className="sm:max-w-[600px]">
        <DialogHeader>
          <DialogTitle>{t('permission.manageMember')}</DialogTitle>
          <p className="truncate text-sm text-text-secondary">{group.name}</p>
        </DialogHeader>

        {canAddMember && (
          <div className="flex items-center gap-2">
            <SelectWithSearch
              value={selectedUserId}
              onChange={handleSelectedUserChange}
              options={invitableUsers.map((user) => ({
                label: user.nickname || user.email,
                value: user.user_id,
              }))}
              placeholder={t('permission.selectMember')}
              disabled={loadingInvitableUsers || invitableUsers.length === 0}
              triggerClassName="flex-1"
            />
            <Button
              disabled={!selectedUserId}
              loading={adding}
              onClick={handleAddMember}
            >
              {t('permission.addMember')}
            </Button>
          </div>
        )}

        {canAddMember &&
          !loadingInvitableUsers &&
          invitableUsers.length === 0 && (
            <p className="text-sm text-text-secondary">
              {pendingTenantUsers.length > 0
                ? t('permission.pendingInviteMembers', {
                    count: pendingTenantUsers.length,
                  })
                : t('permission.noInvitableMembers')}
            </p>
          )}

        <div className="max-h-[46vh] overflow-y-auto pr-1">
          {members.length === 0
            ? !loading && (
                <p className="py-6 text-center text-sm text-text-secondary">
                  {t('common.noData')}
                </p>
              )
            : members.map((member) => (
                <MemberRow
                  key={member.member_id}
                  member={member}
                  availablePermissions={grantablePermissions}
                  canRemove={canRemoveMember}
                  canConfigurePermissions={canConfigurePermissions}
                  canTransferOwner={canTransferOwner}
                  currentUserId={userInfo.id}
                  savingPermissions={savingPermissions}
                  onSavePermissions={handleSavePermissions}
                  onRemove={handleRemoveMember}
                  onTransferOwner={setMemberToTransfer}
                />
              ))}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={hideModal}>
            {t('common.close')}
          </Button>
        </DialogFooter>
      </DialogContent>
      {memberToTransfer && (
        <TransferOwnerDialog
          group={group}
          member={memberToTransfer}
          hideModal={handleHideTransferOwner}
        />
      )}
    </Dialog>
  );
}
