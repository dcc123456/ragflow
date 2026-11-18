import { Checkbox } from '@/components/ui/checkbox';
import { SearchInput } from '@/components/ui/input';
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { TeamRole } from '@/ee/constants/team';
import { GroupList } from '@/ee/components/privilege-management/add-collaborator-dialog/group-list';
import { MemberList } from '@/ee/components/privilege-management/add-collaborator-dialog/member-list';
import {
  GroupContext,
  MemberContext,
} from '@/ee/components/privilege-management/context';
import { CollaboratorItem } from '@/ee/components/privilege-management/interface';
import { UserGroup, Users } from 'lucide-react';
import { useCallback, useContext, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';

interface LeftPanelProps {
  selectAll(role: TeamRole, value: boolean, ids: string[]): void;
}

const tabs = [
  { role: TeamRole.Member, label: 'member', icon: <UserGroup /> },
  { role: TeamRole.Group, label: 'group', icon: <Users /> },
];

const tabTriggerClassName = [
  'h-8 flex-none gap-2 rounded-full border-0.5 border-border-button bg-transparent px-3',
  'text-sm font-normal text-text-secondary shadow-none hover:text-text-primary',
  'data-[state=active]:border-border-default data-[state=active]:bg-bg-input data-[state=active]:text-text-primary data-[state=active]:shadow-none',
].join(' ');

function filterGroups(groups: CollaboratorItem[], keyword: string) {
  const normalizedKeyword = keyword.trim().toLocaleLowerCase();
  if (!normalizedKeyword) {
    return groups;
  }

  const groupsById = new Map(groups.map((group) => [group.id, group]));
  const visibleIds = new Set<string>();

  groups.forEach((group) => {
    if (!group.label.toLocaleLowerCase().includes(normalizedKeyword)) {
      return;
    }

    let current: CollaboratorItem | undefined = group;
    while (current && !visibleIds.has(current.id)) {
      visibleIds.add(current.id);
      current = current.parentId ? groupsById.get(current.parentId) : undefined;
    }
  });

  return groups.filter((group) => visibleIds.has(group.id));
}

export function LeftPanel({ selectAll }: LeftPanelProps) {
  const { t } = useTranslation();
  const [activeTab, setActiveTab] = useState<TeamRole>(TeamRole.Member);
  const [keyword, setKeyword] = useState('');
  const memberContext = useContext(MemberContext);
  const groupContext = useContext(GroupContext);

  const filteredMembers = useMemo(() => {
    const normalizedKeyword = keyword.trim().toLocaleLowerCase();
    return normalizedKeyword
      ? memberContext.list.filter((item) =>
          item.label.toLocaleLowerCase().includes(normalizedKeyword),
        )
      : memberContext.list;
  }, [keyword, memberContext.list]);
  const filteredGroups = useMemo(
    () => filterGroups(groupContext.list, keyword),
    [groupContext.list, keyword],
  );
  const visibleItems =
    activeTab === TeamRole.Member ? filteredMembers : filteredGroups;
  const selectedCount = visibleItems.filter((item) => item.checked).length;
  const selectAllChecked =
    selectedCount === 0
      ? false
      : selectedCount === visibleItems.length
        ? true
        : 'indeterminate';

  const handleSelectAllChange = useCallback(
    (checked: boolean | 'indeterminate') => {
      selectAll(
        activeTab,
        checked === true,
        visibleItems.map((item) => item.id),
      );
    },
    [activeTab, selectAll, visibleItems],
  );

  const handleTabChange = useCallback((value: string) => {
    if (value === TeamRole.Member || value === TeamRole.Group) {
      setActiveTab(value);
    }
  }, []);

  return (
    <section className="flex h-full min-h-0 min-w-0 flex-col gap-3">
      <div className="flex min-w-0 items-center justify-between gap-2">
        <Tabs value={activeTab} onValueChange={handleTabChange}>
          <TabsList className="h-auto gap-1 bg-transparent p-0">
            {tabs.map((tab) => (
              <TabsTrigger
                key={tab.role}
                value={tab.role}
                className={tabTriggerClassName}
              >
                {tab.icon}
                {t(`permission.${tab.label}`)}
                <span className="rounded-full bg-accent-primary/10 px-2 py-0.5 text-xs leading-4 text-accent-primary">
                  {tab.role === TeamRole.Member
                    ? memberContext.list.length
                    : groupContext.list.length}
                </span>
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
        <label className="flex shrink-0 cursor-pointer items-center gap-2 text-sm text-text-secondary">
          <Checkbox
            checked={selectAllChecked}
            onCheckedChange={handleSelectAllChange}
            disabled={visibleItems.length === 0}
          />
          {t('common.selectAll')}
        </label>
      </div>

      <SearchInput
        value={keyword}
        onChange={(event) => setKeyword(event.target.value)}
        className="h-9"
      />

      <div className="min-h-0 flex-1 overflow-hidden">
        <MemberContext.Provider
          value={{ ...memberContext, list: filteredMembers }}
        >
          <GroupContext.Provider
            value={{ ...groupContext, list: filteredGroups }}
          >
            {activeTab === TeamRole.Member ? (
              filteredMembers.length > 0 ? (
                <MemberList />
              ) : (
                <p className="py-6 text-center text-sm text-text-secondary">
                  {t('common.noData')}
                </p>
              )
            ) : filteredGroups.length > 0 ? (
              <GroupList />
            ) : (
              <p className="py-6 text-center text-sm text-text-secondary">
                {t('common.noData')}
              </p>
            )}
          </GroupContext.Provider>
        </MemberContext.Provider>
      </div>
    </section>
  );
}
