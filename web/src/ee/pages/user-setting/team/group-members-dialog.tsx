import {
  ConfirmDeleteDialog,
  ConfirmDeleteDialogNode,
} from '@/components/confirm-delete-dialog';
import { RAGFlowAvatar } from '@/components/ragflow-avatar';
import { TruncatedText } from '@/components/truncated-text';
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
import { ShieldCheck, Trash2 } from 'lucide-react';
import { useCallback, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  type GrantablePermission,
  grantableGroupPermissions,
} from './group-permissions';
import {
  useInviteTenantGroupMember,
  useRemoveTenantGroupMember,
  useSetTenantGroupMemberPermissions,
  useTenantGroupMembers,
} from './use-group-query';

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
    <label className="flex cursor-pointer items-center gap-2 text-sm text-text-secondary">
      <Checkbox checked={checked} onCheckedChange={handleCheckedChange} />
      {t(permission.labelKey)}
    </label>
  );
}

function MemberRow({
  member,
  availablePermissions,
  canRemove,
  canConfigurePermissions,
  currentUserId,
  savingPermissions,
  onSavePermissions,
  onRemove,
}: {
  member: ITenantGroupMember;
  availablePermissions: GrantablePermission[];
  canRemove: boolean;
  canConfigurePermissions: boolean;
  currentUserId: string;
  savingPermissions: boolean;
  onSavePermissions: (
    userId: string,
    permissions: GroupPermissionName[],
  ) => Promise<number | undefined>;
  onRemove: (userId: string) => Promise<void>;
}) {
  const { t } = useTranslation();
  const [editing, setEditing] = useState(false);

  const displayName = member.nickname || member.email;
  const editable = !member.is_owner && member.user_id !== currentUserId;
  const showPermissionButton = canConfigurePermissions && editable;
  const showRemoveButton = canRemove && !member.is_owner;

  const handleToggleEditing = useCallback(() => {
    setEditing((previous) => !previous);
  }, []);

  const handleRemove = useCallback(async () => {
    await onRemove(member.user_id);
  }, [member.user_id, onRemove]);

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
    <div className="mt-3 space-y-3 rounded-lg bg-bg-card p-3">
      <div className="flex flex-wrap gap-x-6 gap-y-2">
        {options.map((permission) => (
          <PermissionOption
            key={permission.value}
            permission={permission}
            checked={selected.includes(permission.value)}
            onToggle={handleToggle}
          />
        ))}
      </div>
      <div className="flex items-center justify-end gap-2">
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
  const { data: tenantUsers } = useListTenantUser(tenantInfo.tenant_id, true);
  const { loading: inviting, inviteMember } = useInviteTenantGroupMember(
    group.id,
  );
  const { removeMember } = useRemoveTenantGroupMember(group.id);
  const { loading: savingPermissions, setMemberPermissions } =
    useSetTenantGroupMemberPermissions(group.id);
  const [selectedUserId, setSelectedUserId] = useState('');

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
  const grantablePermissions = useMemo(
    () => grantableGroupPermissions(group.permission),
    [group.permission],
  );

  const invitableUsers = useMemo(() => {
    return tenantUsers.filter(
      (user) =>
        user.user_id &&
        user.status === TenantMembershipStatus.Joined &&
        !members.some((member) => member.user_id === user.user_id),
    );
  }, [members, tenantUsers]);

  // The group API only accepts Tenant members who already joined, so explain
  // why the picker stays empty when everyone else is still invited.
  const pendingTenantUsers = useMemo(() => {
    return tenantUsers.filter(
      (user) =>
        user.user_id &&
        user.status === TenantMembershipStatus.Invited &&
        !members.some((member) => member.user_id === user.user_id),
    );
  }, [members, tenantUsers]);

  const handleSelectedUserChange = useCallback((value: string) => {
    setSelectedUserId(value);
  }, []);

  const handleInvite = useCallback(async () => {
    if (!selectedUserId) {
      return;
    }

    const code = await inviteMember(selectedUserId);

    if (code === 0) {
      setSelectedUserId('');
    }
  }, [inviteMember, selectedUserId]);

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
              disabled={invitableUsers.length === 0}
              triggerClassName="flex-1"
            />
            <Button
              disabled={!selectedUserId}
              loading={inviting}
              onClick={handleInvite}
            >
              {t('permission.addMember')}
            </Button>
          </div>
        )}

        {canAddMember && invitableUsers.length === 0 && (
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
                  currentUserId={userInfo.id}
                  savingPermissions={savingPermissions}
                  onSavePermissions={handleSavePermissions}
                  onRemove={handleRemoveMember}
                />
              ))}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={hideModal}>
            {t('common.close')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
