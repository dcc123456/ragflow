import { Button } from '@/components/ui/button';
import { SearchInput } from '@/components/ui/input';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { InviteDialog } from '@/ee/pages/user-setting/setting-team/team-management/member/invite-dialog';
import {
  useFetchTenantInfo,
  useListTenantUser,
} from '@/hooks/use-user-setting-request';
import { cn } from '@/lib/utils';
import { UserGroup, UserPlus, Users } from 'lucide-react';
import { useAddUser } from '@/pages/user-setting/setting-team/hooks';
import { useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { GroupFormDialog } from './group-form-dialog';
import { GroupTable } from './group-table';
import { MemberTable } from './member-table';
import { useTenantGroups } from './use-group-query';

const enum TeamTab {
  Member = 'member',
  Group = 'group',
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
 * Enterprise team page: member and group lists following the new team design.
 */
export default function TeamPage() {
  const { t } = useTranslation();
  const [activeTab, setActiveTab] = useState<string>(TeamTab.Member);
  const [keyword, setKeyword] = useState('');
  const [createGroupModalVisible, setCreateGroupModalVisible] = useState(false);
  const { data: tenantInfo } = useFetchTenantInfo();
  const { data: members } = useListTenantUser(tenantInfo.tenant_id);
  const { data: groups } = useTenantGroups();
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

  return (
    <section className="size-full flex flex-col min-w-0">
      <h1 className="pb-6 text-2xl font-semibold text-text-primary">
        {t('permission.myTeam')}
      </h1>
      <Tabs value={activeTab} onValueChange={handleTabChange}>
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
          </TabsList>

          <div className="flex items-center gap-2">
            <SearchInput
              value={keyword}
              onChange={handleSearchChange}
              rootClassName="w-[200px]"
            />
            {activeTab === TeamTab.Member ? (
              <Button onClick={showAddingTenantModal}>
                <UserPlus />
                {t('setting.invite')}
              </Button>
            ) : (
              <Button className="rounded-lg" onClick={showCreateGroupModal}>
                <UserPlus />
                {t('permission.createGroup')}
              </Button>
            )}
          </div>
        </div>

        <TabsContent value={TeamTab.Member} className="mt-0">
          <MemberTable keyword={keyword} />
        </TabsContent>
        <TabsContent value={TeamTab.Group} className="mt-0">
          <GroupTable keyword={keyword} />
        </TabsContent>
      </Tabs>

      {addingTenantModalVisible && (
        <InviteDialog
          hideModal={hideAddingTenantModal}
          onOk={handleAddUserOk}
        />
      )}
      {createGroupModalVisible && (
        <GroupFormDialog hideModal={hideCreateGroupModal} />
      )}
    </section>
  );
}
