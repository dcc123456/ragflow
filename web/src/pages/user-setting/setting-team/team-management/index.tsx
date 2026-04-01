import { ConfirmDeleteDialog } from '@/components/confirm-delete-dialog';
import { RoleLabel } from '@/components/privilege-management/add-collaborator-dialog/right-panel';
import { Button } from '@/components/ui/button';
import { RAGFlowSelect } from '@/components/ui/select';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { TeamRole } from '@/constants/team';
import { TenantIdContext } from '@/contexts/teant-context';
import {
  useDeleteTenantUser,
  useFetchTenantInfo,
  useFetchUserInfo,
  useListTenant,
  useListTenantUser,
} from '@/hooks/use-user-setting-request';
import { LogOut, UserPlus, Users } from 'lucide-react';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { TenantRole } from '../../constants';
import { useAddUser } from '../hooks';
import { GroupContext } from './context';
import { Department } from './department';
import { Group } from './group';
import { CreateGroupDialog } from './group/create-group-dialog';
import { InviteDialog } from './member/invite-dialog';
import { MemberTable } from './member/member-table';
import {
  PermissionManagementDialog,
  PermissionManagementDialogContext,
} from './permission-management-dialog';
import { useModifyGroup } from './use-operate-group';

export function TeamManagement() {
  const [activity, setActivity] = useState<TeamRole>(TeamRole.Member);
  const [permissionDialogVisible, setPermissionDialogVisible] = useState(false);
  const [permissionTarget, setPermissionTarget] = useState<{
    id: string;
    name: string;
    avatar?: string;
    email?: string;
    role: TeamRole;
  } | null>(null);
  const { t } = useTranslation();
  const { data: tenantList } = useListTenant();
  const { data: tenantInfo } = useFetchTenantInfo();

  const joinedTeams = useMemo(() => {
    return tenantList
      .filter((x) => x.role !== TenantRole.Invite)
      .map((x) => ({
        label: (
          <div className="flex items-center gap-2">
            <span>{x.nickname}</span>
            {x.role === TenantRole.Owner && (
              <RoleLabel label={t('permission.owner')}></RoleLabel>
            )}
          </div>
        ),
        value: x.tenant_id,
      }));
  }, [t, tenantList]);

  const [currentTeamId, setCurrentTeamId] = useState<string>('');
  const { data: userList } = useListTenantUser(currentTeamId);

  const isMyCreatedTeam = useMemo(() => {
    return tenantInfo.tenant_id === currentTeamId;
  }, [currentTeamId, tenantInfo.tenant_id]);

  const handleChange = useCallback((val: string) => {
    setActivity(val as TeamRole);
  }, []);

  const { showGroupModal, hideGroupModal, groupVisible, onGroupOk, group } =
    useModifyGroup(currentTeamId);

  const { deleteTenantUser } = useDeleteTenantUser();
  const { data: userInfo } = useFetchUserInfo();

  const handleQuitTenantUser = useCallback(async () => {
    const ret = await deleteTenantUser({
      userId: userInfo.id,
      tenantId: currentTeamId,
    });
    if (ret === 0) {
      setCurrentTeamId(tenantInfo.tenant_id);
    }
  }, [currentTeamId, deleteTenantUser, tenantInfo.tenant_id, userInfo.id]);

  const {
    addingTenantModalVisible,
    hideAddingTenantModal,
    showAddingTenantModal,
    handleAddUserOk,
  } = useAddUser();

  const handleShowGroupModal = useCallback(() => {
    showGroupModal();
  }, [showGroupModal]);

  const handleShowPermissionModal = useCallback(
    (target: {
      id: string;
      name: string;
      avatar?: string;
      email?: string;
      role: TeamRole;
    }) => {
      setPermissionTarget(target);
      setPermissionDialogVisible(true);
    },
    [],
  );

  const handleHidePermissionModal = useCallback(() => {
    setPermissionDialogVisible(false);
    setPermissionTarget(null);
  }, []);

  useEffect(() => {
    setCurrentTeamId(tenantInfo.tenant_id);
  }, [tenantInfo.tenant_id]);

  return (
    <section>
      <div className="flex py-4 border-b justify-between mb-4">
        <div className="flex gap-4 items-center ">
          <Users /> {t('permission.teamManagement')}
          <RAGFlowSelect
            options={joinedTeams}
            value={currentTeamId}
            onChange={setCurrentTeamId}
          ></RAGFlowSelect>
        </div>
        <span>
          {userList.length} {t('permission.membersInTotal')}
        </span>
      </div>
      <TenantIdContext.Provider value={currentTeamId}>
        <PermissionManagementDialogContext.Provider
          value={handleShowPermissionModal}
        >
          <Tabs
            defaultValue="account"
            value={activity}
            onValueChange={handleChange}
          >
            <div className="flex justify-between">
              <TabsList className="mb-4">
                <TabsTrigger value={TeamRole.Member}>
                  {t('permission.member')}
                </TabsTrigger>
                <TabsTrigger value={TeamRole.Department}>
                  {t('permission.department')}
                </TabsTrigger>
                <TabsTrigger value={TeamRole.Group}>
                  {t('permission.group')}
                </TabsTrigger>
              </TabsList>
              <div className="flex gap-2 items-center">
                <div id="department-toolbar"></div>
                {isMyCreatedTeam && activity === TeamRole.Group && (
                  <Button onClick={handleShowGroupModal}>
                    <UserPlus /> {t('permission.createGroup')}
                  </Button>
                )}
                {activity === TeamRole.Member &&
                  (isMyCreatedTeam ? (
                    <Button onClick={showAddingTenantModal}>
                      <UserPlus /> {t('setting.invite')}
                    </Button>
                  ) : (
                    <ConfirmDeleteDialog
                      onOk={handleQuitTenantUser}
                      title={t('setting.sureQuit')}
                    >
                      <Button>
                        <LogOut />
                        {t('permission.leaveTheTeam')}
                      </Button>
                    </ConfirmDeleteDialog>
                  ))}
              </div>
            </div>
            <TabsContent value={TeamRole.Member}>
              <MemberTable></MemberTable>
            </TabsContent>
            <TabsContent value={TeamRole.Department}>
              <Department></Department>
            </TabsContent>
            <TabsContent value={TeamRole.Group}>
              <GroupContext.Provider value={showGroupModal}>
                <Group></Group>
              </GroupContext.Provider>
            </TabsContent>
          </Tabs>
        </PermissionManagementDialogContext.Provider>
      </TenantIdContext.Provider>
      {groupVisible && (
        <CreateGroupDialog
          hideModal={hideGroupModal}
          onOk={onGroupOk}
          initialValues={group}
        ></CreateGroupDialog>
      )}
      {addingTenantModalVisible && (
        <InviteDialog
          hideModal={hideAddingTenantModal}
          onOk={handleAddUserOk}
        ></InviteDialog>
      )}
      {permissionDialogVisible && permissionTarget && (
        <PermissionManagementDialog
          hideModal={handleHidePermissionModal}
          initialValues={{
            id: permissionTarget.id,
            name: permissionTarget.name,
            avatar: permissionTarget.avatar,
            email: permissionTarget.email,
            role: permissionTarget.role,
            tenant_id: currentTeamId,
          }}
        ></PermissionManagementDialog>
      )}
    </section>
  );
}
