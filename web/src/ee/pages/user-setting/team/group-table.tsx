import { RAGFlowAvatar } from '@/components/ragflow-avatar';
import { TableEmpty, TableSkeleton } from '@/components/table-skeleton';
import { TruncatedText } from '@/components/truncated-text';
import { AvatarGroup } from '@/components/ui/avatar';
import { Button } from '@/components/ui/button';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { ITenantGroup } from '@/ee/interfaces/database/team';
import { TenantMembershipStatus } from '@/pages/user-setting/constants';
import { ChevronDown, ChevronUp } from 'lucide-react';
import { useCallback, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { GroupRowActions } from './group-row-actions';
import { useTenantGroupMembers, useTenantGroups } from './use-group-query';

const INDENT_WIDTH = 24;
const GUIDE_OFFSET = 10;
const MEMBER_PREVIEW_LIMIT = 3;
const MEMBER_AVATAR_CLASS = 'size-6';
const MEMBER_AVATAR_STYLE: React.CSSProperties = {
  boxShadow: '0 0 0 2px var(--bg-base)',
};
const OWNER_AVATAR_STYLE: React.CSSProperties = {
  boxShadow: '0 0 0 2px rgb(var(--accent-primary))',
};

type GroupRow = {
  group: ITenantGroup;
  depth: number;
  hasChildren: boolean;
  expanded: boolean;
};

type GroupTree = {
  childrenMap: Map<string, ITenantGroup[]>;
  roots: ITenantGroup[];
};

const compareGroupNames = (left: ITenantGroup, right: ITenantGroup) =>
  (left.name ?? '').localeCompare(right.name ?? '', undefined, {
    sensitivity: 'base',
  }) || left.id.localeCompare(right.id);

const buildGroupTree = (groups: ITenantGroup[]): GroupTree => {
  const groupIds = new Set(groups.map((group) => group.id));
  const childrenMap = new Map<string, ITenantGroup[]>();

  groups.forEach((group) => {
    const parentId =
      group.parent_id && groupIds.has(group.parent_id)
        ? group.parent_id
        : undefined;

    if (!parentId) {
      return;
    }

    const children = childrenMap.get(parentId) ?? [];
    children.push(group);
    childrenMap.set(parentId, children);
  });

  childrenMap.forEach((children) => children.sort(compareGroupNames));
  const roots = groups
    .filter((group) => !group.parent_id || !groupIds.has(group.parent_id))
    .sort(compareGroupNames);

  return { childrenMap, roots };
};

function GroupMemberStack({ group }: { group: ITenantGroup }) {
  const { data: members } = useTenantGroupMembers(
    group.id,
    group.member_count > 0,
  );

  if (members.length === 0) {
    return null;
  }

  if (members.length === 1) {
    const [member] = members;

    return (
      <RAGFlowAvatar
        name={member.nickname || member.email}
        avatar={member.avatar}
        isPerson
        className={`${MEMBER_AVATAR_CLASS} ${
          member.tenant_status === TenantMembershipStatus.Left
            ? 'opacity-50'
            : ''
        }`}
        style={{
          ...(member.is_owner ? OWNER_AVATAR_STYLE : {}),
        }}
      />
    );
  }

  return (
    <AvatarGroup
      className="justify-end ps-2"
      {...(members.length > MEMBER_PREVIEW_LIMIT
        ? { max: MEMBER_PREVIEW_LIMIT }
        : {})}
    >
      {members.map((member, index) => (
        <RAGFlowAvatar
          key={member.member_id}
          name={member.nickname || member.email}
          avatar={member.avatar}
          isPerson
          className={`${MEMBER_AVATAR_CLASS} ${
            member.tenant_status === TenantMembershipStatus.Left
              ? 'opacity-50'
              : ''
          }`}
          style={{
            ...(member.is_owner ? OWNER_AVATAR_STYLE : MEMBER_AVATAR_STYLE),
            zIndex: members.length - index,
          }}
        />
      ))}
    </AvatarGroup>
  );
}

export function GroupTable({ keyword }: { keyword: string }) {
  const { t } = useTranslation();
  const { data: groups, loading } = useTenantGroups();
  const [expandedIds, setExpandedIds] = useState<ReadonlySet<string>>(
    new Set(),
  );

  const { childrenMap, roots } = useMemo(
    () => buildGroupTree(groups),
    [groups],
  );

  const groupsById = useMemo(
    () => new Map(groups.map((group) => [group.id, group])),
    [groups],
  );

  const getParentPermission = useCallback(
    (group: ITenantGroup) => {
      const parent = group.parent_id
        ? groupsById.get(group.parent_id)
        : undefined;

      return parent?.permission ?? 0;
    },
    [groupsById],
  );

  const normalizedKeyword = keyword.trim().toLowerCase();

  const visibleGroupIds = useMemo(() => {
    if (!normalizedKeyword) {
      return null;
    }

    const visibleIds = new Set<string>();

    const collectVisible = (group: ITenantGroup): boolean => {
      let visible = (group.name ?? '')
        .toLowerCase()
        .includes(normalizedKeyword);
      const children = childrenMap.get(group.id) ?? [];

      children.forEach((child) => {
        if (collectVisible(child)) {
          visible = true;
        }
      });

      if (visible) {
        visibleIds.add(group.id);
      }

      return visible;
    };

    roots.forEach(collectVisible);

    return visibleIds;
  }, [childrenMap, normalizedKeyword, roots]);

  const rows = useMemo(() => {
    const result: GroupRow[] = [];

    const visit = (group: ITenantGroup, depth: number) => {
      if (visibleGroupIds && !visibleGroupIds.has(group.id)) {
        return;
      }

      const children = childrenMap.get(group.id) ?? [];
      const expanded = !!normalizedKeyword || expandedIds.has(group.id);

      result.push({
        group,
        depth,
        hasChildren: children.length > 0,
        expanded,
      });

      if (expanded) {
        children.forEach((child) => visit(child, depth + 1));
      }
    };

    roots.forEach((root) => visit(root, 0));

    return result;
  }, [childrenMap, expandedIds, normalizedKeyword, roots, visibleGroupIds]);

  const handleToggleGroup = useCallback(
    (event: React.MouseEvent<HTMLButtonElement>) => {
      const groupId = event.currentTarget.value;

      if (!groupId) {
        return;
      }

      setExpandedIds((previous) => {
        const next = new Set(previous);

        if (next.has(groupId)) {
          next.delete(groupId);
        } else {
          next.add(groupId);
        }

        return next;
      });
    },
    [],
  );

  return (
    <Table rootClassName="bg-transparent max-h-[76vh]" className="table-fixed">
      <TableHeader className="bg-transparent">
        <TableRow className="hover:bg-transparent">
          <TableHead className="w-[45%]">{t('common.name')}</TableHead>
          <TableHead className="w-[240px]" />
          <TableHead className="w-[190px] text-right">
            {t('common.action')}
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {loading ? (
          <TableSkeleton columnsLength={3} />
        ) : rows.length ? (
          rows.map((row) => (
            <TableRow
              key={row.group.id}
              className="group border-b-0 hover:bg-transparent"
            >
              <TableCell className="relative w-[45%] max-w-0 transition-colors group-hover:rounded-l-lg group-hover:bg-bg-card">
                {Array.from({ length: row.depth }, (_unused, level) => (
                  <span
                    key={level}
                    aria-hidden="true"
                    className="absolute inset-y-0 w-px bg-border-button"
                    style={{
                      left: (level + 1) * INDENT_WIDTH + GUIDE_OFFSET,
                    }}
                  />
                ))}
                <div
                  className="flex min-w-0 items-center gap-2"
                  style={{ paddingLeft: row.depth * INDENT_WIDTH }}
                >
                  <RAGFlowAvatar
                    isPerson
                    className="size-5"
                    name={row.group.name}
                    avatar={row.group.avatar}
                  />
                  <TruncatedText
                    as="span"
                    className="min-w-0 truncate"
                    tooltip={row.group.name}
                  >
                    {row.group.name}
                  </TruncatedText>
                  {row.hasChildren && !normalizedKeyword && (
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      className="-ml-1 text-text-secondary"
                      value={row.group.id}
                      onClick={handleToggleGroup}
                      aria-label={
                        row.expanded
                          ? t('permission.collapse')
                          : t('permission.expand')
                      }
                      title={
                        row.expanded
                          ? t('permission.collapse')
                          : t('permission.expand')
                      }
                    >
                      {row.expanded ? <ChevronUp /> : <ChevronDown />}
                    </Button>
                  )}
                </div>
              </TableCell>
              <TableCell className="w-[240px] transition-colors group-hover:bg-bg-card">
                <GroupMemberStack group={row.group} />
              </TableCell>
              <TableCell className="w-[190px] transition-colors group-hover:rounded-r-lg group-hover:bg-bg-card">
                <GroupRowActions
                  group={row.group}
                  parentPermission={getParentPermission(row.group)}
                />
              </TableCell>
            </TableRow>
          ))
        ) : (
          <TableEmpty columnsLength={3} />
        )}
      </TableBody>
    </Table>
  );
}
