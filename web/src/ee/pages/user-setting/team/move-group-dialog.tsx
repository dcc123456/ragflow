import { Button, ButtonLoading } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover';
import { TreeDataItem, TreeView } from '@/components/ui/tree-view';
import { ITenantGroup } from '@/ee/interfaces/database/team';
import { IModalProps } from '@/interfaces/common';
import { ChevronDown } from 'lucide-react';
import { useCallback, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useMoveTenantGroup, useTenantGroups } from './use-group-query';

const ROOT_PARENT = '__root__';

function buildGroupParentTree(
  groups: ITenantGroup[],
  rootName: string,
): TreeDataItem[] {
  const groupIds = new Set(groups.map((item) => item.id));
  const childrenByParent = new Map<string | null, ITenantGroup[]>();

  groups.forEach((group) => {
    const parentId =
      group.parent_id && groupIds.has(group.parent_id) ? group.parent_id : null;
    const children = childrenByParent.get(parentId) ?? [];
    children.push(group);
    childrenByParent.set(parentId, children);
  });

  const buildChildren = (
    parentId: string | null,
    ancestors: ReadonlySet<string>,
  ): TreeDataItem[] =>
    (childrenByParent.get(parentId) ?? [])
      .filter((group) => !ancestors.has(group.id))
      .map((group) => {
        const nextAncestors = new Set(ancestors);
        nextAncestors.add(group.id);

        return {
          id: group.id,
          name: group.name || group.id,
          children: buildChildren(group.id, nextAncestors),
        };
      });

  return [
    {
      id: ROOT_PARENT,
      name: rootName,
      children: buildChildren(null, new Set()),
    },
  ];
}

function findTreeItem(
  items: TreeDataItem[],
  id: string,
): TreeDataItem | undefined {
  for (const item of items) {
    if (item.id === id) {
      return item;
    }

    const child = item.children && findTreeItem(item.children, id);
    if (child) {
      return child;
    }
  }
}

function GroupParentSelector({
  tree,
  value,
  onChange,
  disabled,
}: {
  tree: TreeDataItem[];
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const selectedLabel =
    findTreeItem(tree, value)?.name ?? t('common.pleaseSelect');

  const handleOpenChange = useCallback((nextOpen: boolean) => {
    setOpen(nextOpen);
  }, []);

  const handleSelect = useCallback(
    (item: TreeDataItem | undefined) => {
      if (item) {
        onChange(item.id);
        setOpen(false);
      }
    },
    [onChange],
  );

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="outline"
          className="w-full justify-between font-normal"
          disabled={disabled}
        >
          <span className="min-w-0 truncate">{selectedLabel}</span>
          <ChevronDown className="size-4 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="w-[var(--radix-popover-trigger-width)] p-1"
      >
        <div className="max-h-72 overflow-y-auto">
          <TreeView
            key={value}
            data={tree}
            initialSelectedItemId={value}
            onSelectChange={handleSelect}
            expandOnRowClick={false}
          />
        </div>
      </PopoverContent>
    </Popover>
  );
}

export function MoveGroupDialog({
  group,
  hideModal,
}: IModalProps<unknown> & { group: ITenantGroup }) {
  const { t } = useTranslation();
  const { data: groups, loading: groupsLoading } = useTenantGroups();
  const { loading, moveGroup } = useMoveTenantGroup(group.id);
  const currentParentId = group.parent_id || ROOT_PARENT;
  const [selectedParentId, setSelectedParentId] = useState(currentParentId);

  const parentCandidates = useMemo(() => {
    const childrenByParent = new Map<string, string[]>();

    groups.forEach((item) => {
      if (!item.parent_id) {
        return;
      }

      const children = childrenByParent.get(item.parent_id) ?? [];
      children.push(item.id);
      childrenByParent.set(item.parent_id, children);
    });

    const descendants = new Set<string>();
    const collectDescendants = (id: string) => {
      (childrenByParent.get(id) ?? []).forEach((childId) => {
        if (descendants.has(childId)) {
          return;
        }
        descendants.add(childId);
        collectDescendants(childId);
      });
    };
    collectDescendants(group.id);

    return groups.filter(
      (item) => item.id !== group.id && !descendants.has(item.id),
    );
  }, [group.id, groups]);

  const parentTree = useMemo(
    () => buildGroupParentTree(parentCandidates, t('permission.rootGroup')),
    [parentCandidates, t],
  );

  const handleParentChange = useCallback((value: string) => {
    setSelectedParentId(value);
  }, []);

  const handleConfirm = useCallback(async () => {
    if (selectedParentId === currentParentId) {
      hideModal?.();
      return;
    }

    const code = await moveGroup(
      selectedParentId === ROOT_PARENT ? null : selectedParentId,
    );

    if (code === 0) {
      hideModal?.();
    }
  }, [currentParentId, hideModal, moveGroup, selectedParentId]);

  return (
    <Dialog open onOpenChange={hideModal}>
      <DialogContent className="sm:max-w-[425px]">
        <DialogHeader className="min-w-0">
          <DialogTitle>{t('common.move')}</DialogTitle>
          <p className="truncate text-sm text-text-secondary">{group.name}</p>
        </DialogHeader>
        <div className="space-y-2">
          <div className="text-sm font-medium">
            {t('permission.parentGroup')}
          </div>
          <GroupParentSelector
            tree={parentTree}
            value={selectedParentId}
            onChange={handleParentChange}
            disabled={groupsLoading}
          />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={hideModal}>
            {t('common.cancel')}
          </Button>
          <ButtonLoading
            loading={loading}
            disabled={groupsLoading || selectedParentId === currentParentId}
            onClick={handleConfirm}
          >
            {t('common.move')}
          </ButtonLoading>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
