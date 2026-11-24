import { Button } from '@/components/ui/button';
import { SearchInput } from '@/components/ui/input';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { InviteDialog } from '@/ee/pages/user-setting/setting-team/team-management/member/invite-dialog';
import {
  useFetchTenantInfo,
  useListTenantUser,
} from '@/hooks/use-user-setting-request';
import { cn } from '@/lib/utils';
import { ShieldCheck, UserGroup, UserPlus, Users } from 'lucide-react';
import { useAddUser } from '@/pages/user-setting/setting-team/hooks';
import { useCallback, useState } from 'react';
import { useLocation } from 'react-router';
import { useTranslation } from 'react-i18next';
import { GroupFormDialog } from './group-form-dialog';
import { GroupTable } from './group-table';
import { MemberTable } from './member-table';
import { RoleTable } from './role-table';
import { TenantRoleFormDialog } from './tenant-role-form-dialog';
import {
  useTenantMyPermissions,
  useTenantRoles,
} from './use-member-role-query';
import { useTenantGroups } from './use-group-query';

const enum TeamTab {
  Member = 'member',
  Group = 'group',
  Role = 'role',
}

const TabTriggerClassName = cn(
  'h-8 flex-none gap-2 rounded-full border-0.5 border-border-button bg-transparent px-4',
  'text-sm font-normal text-text-secondary shadow-none',
  'hover:text-text-primary',
  'data-[state=active]:border-border-default data-[state=active]:bg-bg-input data-[state=active]:text-text-primary data-[state=active]:shadow-none',
);

const TabCountClassName =
  'rounded-full bg-accent-primary/10 px-2 py-0.5 text-xs leading-4 text-accent-primary';

/**
 * Enterprise Team page for members, groups, and tenant roles.
 */
export default function TeamPage() {
  const { t } = useTranslation();
  const location = useLocation();
  const returnState = location.state as {
    teamTab?: string;
    teamKeyword?: string;
  } | null;
  const [activeTab, setActiveTab] = useState<string>(
    returnState?.teamTab ?? TeamTab.Member,
  );
  const [keyword, setKeyword] = useState(returnState?.teamKeyword ?? '');
  const [createGroupModalVisible, setCreateGroupModalVisible] = useState(false);
  const [createRoleModalVisible, setCreateRoleModalVisible] = useState(false);
  const { data: tenantInfo } = useFetchTenantInfo();
  const { data: members } = useListTenantUser(tenantInfo.tenant_id);
  const { data: groups } = useTenantGroups();
  const { data: myPermissions } = useTenantMyPermissions(tenantInfo.tenant_id);
  const canManageMembers = !!myPermissions?.tenant_permissions.manage_members;
  const canManageRoles = !!myPermissions?.tenant_permissions.manage_roles;
  const { data: roles } = useTenantRoles(tenantInfo.tenant_id, canManageRoles);
  const visibleTab =
    activeTab === TeamTab.Role && !canManageRoles ? TeamTab.Member : activeTab;
  const {
    addingTenantModalVisible,
    hideAddingTenantModal,
    showAddingTenantModal,
    handleAddUserOk,
  } = useAddUser();

  const handleTabChange = useCallback((value: string) => {
    setActiveTab(value);
  }, []);

  const handleSearchChange = useCallback(
    (event: React.ChangeEvent<HTMLInputElement>) => {
      setKeyword(event.target.value);
    },
    [],
  );

  const showCreateGroupModal = useCallback(() => {
    setCreateGroupModalVisible(true);
  }, []);

  const hideCreateGroupModal = useCallback(() => {
    setCreateGroupModalVisible(false);
  }, []);

  const showCreateRoleModal = useCallback(() => {
    setCreateRoleModalVisible(true);
  }, []);

  const hideCreateRoleModal = useCallback(() => {
    setCreateRoleModalVisible(false);
  }, []);

  return (
    <section className="size-full flex flex-col min-w-0">
      <h1 className="pb-6 text-2xl font-semibold text-text-primary">
        {t('permission.myTeam')}
      </h1>
      <Tabs value={visibleTab} onValueChange={handleTabChange}>
        <div className="flex items-center justify-between gap-4 pb-4">
          <TabsList className="h-auto gap-2 bg-transparent p-0">
            <TabsTrigger value={TeamTab.Member} className={TabTriggerClassName}>
              <UserGroup className="size-4" />
              {t('permission.member')}
              <span className={TabCountClassName}>{members.length}</span>
            </TabsTrigger>
            <TabsTrigger value={TeamTab.Group} className={TabTriggerClassName}>
              <Users className="size-4" />
              {t('permission.group')}
              <span className={TabCountClassName}>{groups.length}</span>
            </TabsTrigger>
            {canManageRoles && (
              <TabsTrigger value={TeamTab.Role} className={TabTriggerClassName}>
                <ShieldCheck className="size-4" />
                {t('permission.role')}
                <span className={TabCountClassName}>{roles.length}</span>
              </TabsTrigger>
            )}
          </TabsList>

          <div className="flex items-center gap-2">
            {activeTab !== TeamTab.Role && (
              <SearchInput
                value={keyword}
                onChange={handleSearchChange}
                rootClassName="w-[200px]"
              />
            )}
            {activeTab === TeamTab.Member && canManageMembers ? (
              <Button onClick={showAddingTenantModal}>
                <UserPlus />
                {t('setting.invite')}
              </Button>
            ) : activeTab === TeamTab.Group ? (
              <Button className="rounded-lg" onClick={showCreateGroupModal}>
                <UserPlus />
                {t('permission.createGroup')}
              </Button>
            ) : null}
          </div>
        </div>

        <TabsContent value={TeamTab.Member} className="mt-0">
          <MemberTable keyword={keyword} />
        </TabsContent>
        <TabsContent value={TeamTab.Group} className="mt-0">
          <GroupTable keyword={keyword} />
        </TabsContent>
        {canManageRoles && (
          <TabsContent value={TeamTab.Role} className="mt-0">
            <RoleTable
              keyword={keyword}
              tenantId={tenantInfo.tenant_id}
              members={members}
              canManageRoles={canManageRoles}
              onCreate={showCreateRoleModal}
              onKeywordChange={setKeyword}
            />
          </TabsContent>
        )}
      </Tabs>

      {canManageMembers && addingTenantModalVisible && (
        <InviteDialog
          tenantId={tenantInfo.tenant_id}
          hideModal={hideAddingTenantModal}
          onOk={handleAddUserOk}
        />
      )}
      {createGroupModalVisible && (
        <GroupFormDialog hideModal={hideCreateGroupModal} />
      )}
      {canManageRoles && createRoleModalVisible && (
        <TenantRoleFormDialog
          tenantId={tenantInfo.tenant_id}
          onClose={hideCreateRoleModal}
        />
      )}
    </section>
  );
}
