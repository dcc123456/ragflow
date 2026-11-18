import { useContext, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { GroupContext } from '@/ee/components/privilege-management/context';
import { LeftCollaboratorItem } from '@/ee/components/privilege-management/add-collaborator-dialog/collaborator-item';
import { CollaboratorItem } from '@/ee/components/privilege-management/interface';

type GroupNode = {
  item: CollaboratorItem;
  children: GroupNode[];
};

function buildGroupTree(groups: CollaboratorItem[]) {
  const nodes = new Map(
    groups.map((item) => [item.id, { item, children: [] as GroupNode[] }]),
  );
  const roots: GroupNode[] = [];

  for (const node of nodes.values()) {
    const parent = node.item.parentId ? nodes.get(node.item.parentId) : null;
    if (parent && parent !== node) {
      parent.children.push(node);
    } else {
      roots.push(node);
    }
  }

  const sortNodes = (items: GroupNode[]) => {
    items.sort((a, b) => a.item.label.localeCompare(b.item.label));
    items.forEach((node) => sortNodes(node.children));
  };
  sortNodes(roots);

  return roots;
}

type GroupTreeItemProps = {
  node: GroupNode;
  collapsedIds: Set<string>;
  onToggle: (groupId: string) => void;
  onSelect: (groupId: string) => void;
  expandLabel: string;
  collapseLabel: string;
};

function GroupTreeItem({
  node,
  collapsedIds,
  onToggle,
  onSelect,
  expandLabel,
  collapseLabel,
}: GroupTreeItemProps) {
  const hasChildren = node.children.length > 0;
  const expanded = hasChildren && !collapsedIds.has(node.item.id);
  const nestedItems = expanded ? (
    <ul className="m-0 ml-2 list-none space-y-0.5 border-l border-border-button py-0.5 pl-1.5">
      {node.children.map((child) => (
        <GroupTreeItem
          key={child.item.id}
          node={child}
          collapsedIds={collapsedIds}
          onToggle={onToggle}
          onSelect={onSelect}
          expandLabel={expandLabel}
          collapseLabel={collapseLabel}
        />
      ))}
    </ul>
  ) : undefined;

  return (
    <LeftCollaboratorItem
      item={node.item}
      click={onSelect}
      hasChildren={hasChildren}
      expanded={expanded}
      expandLabel={expandLabel}
      collapseLabel={collapseLabel}
      onToggleExpanded={() => onToggle(node.item.id)}
      nestedItems={nestedItems}
      compact
    />
  );
}

export function GroupList() {
  const { list, handleClick } = useContext(GroupContext);
  const { t } = useTranslation();
  const [collapsedIds, setCollapsedIds] = useState<Set<string>>(
    () => new Set(),
  );
  const tree = useMemo(() => buildGroupTree(list), [list]);

  const toggleExpanded = (groupId: string) => {
    setCollapsedIds((current) => {
      const next = new Set(current);
      if (next.has(groupId)) {
        next.delete(groupId);
      } else {
        next.add(groupId);
      }
      return next;
    });
  };

  return (
    <ul className="m-0 h-full min-h-0 min-w-0 list-none space-y-0.5 overflow-y-auto p-0">
      {tree.map((node) => (
        <GroupTreeItem
          key={node.item.id}
          node={node}
          collapsedIds={collapsedIds}
          onToggle={toggleExpanded}
          onSelect={handleClick}
          expandLabel={t('permission.expand')}
          collapseLabel={t('permission.collapse')}
        />
      ))}
    </ul>
  );
}
