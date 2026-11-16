import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { hasResourceOwnerPermission } from '@/ee/constants/team';
import { TeamApiAction } from '@/ee/hooks/use-team';
import { IPermission } from '@/ee/interfaces/database/team';
import { cn } from '@/lib/utils';
import { useQueryClient } from '@tanstack/react-query';
import { ChevronDown, ChevronRight } from 'lucide-react';
import {
  MouseEvent,
  MouseEventHandler,
  PropsWithChildren,
  ReactNode,
  useCallback,
  useContext,
  useMemo,
} from 'react';
import {
  CheckedListContext,
  DepartmentContext,
  MemberContext,
} from '@/ee/components/privilege-management/context';
import { CollaboratorItem as ICollaboratorItem } from '@/ee/components/privilege-management/interface';
import { PrivilegeAvatar } from '@/ee/components/privilege-management/privilege-avatar';
import { PrivilegeLabel } from '@/ee/components/privilege-management/privilege-label';
import { useSwitchBreadcrumb } from '@/ee/components/privilege-management/add-collaborator-dialog/use-select-collaborator';

type LeftCollaboratorItemProps = {
  item: ICollaboratorItem;
  click: (id: string, value?: boolean) => void;
  showCheckbox?: boolean;
  avatar?: ReactNode;
  hasChildren?: boolean;
  expanded?: boolean;
  expandLabel?: string;
  collapseLabel?: string;
  onToggleExpanded?: () => void;
  nestedItems?: ReactNode;
  compact?: boolean;
} & PropsWithChildren;

type CollaboratorItemProps = Pick<
  LeftCollaboratorItemProps,
  'item' | 'showCheckbox' | 'avatar'
>;

function usePermissionItem(itemId: string) {
  const queryClient = useQueryClient();
  const data = queryClient.getQueriesData<IPermission[]>({
    queryKey: [TeamApiAction.ListPermission],
  });
  const permissionItems = data.at(0)?.at(1) as IPermission[] | undefined;

  return permissionItems?.find((x) => x.id === itemId);
}

function LeftPrivilegeLabel({ item }: { item: ICollaboratorItem }) {
  const permissionItem = usePermissionItem(item.id);

  if (permissionItem) {
    return (
      <PrivilegeLabel permissions={permissionItem.permissions}></PrivilegeLabel>
    );
  }
  return null;
}

export function CollaboratorItem({
  item,
  showCheckbox,
  avatar,
}: CollaboratorItemProps) {
  const permissionItem = usePermissionItem(item.id);
  const isOwner = hasResourceOwnerPermission(permissionItem?.permissions);

  return (
    <div className="flex min-w-0 items-center gap-2">
      {showCheckbox &&
        (isOwner ? (
          <span className="inline-block h-4 w-4 shrink-0" />
        ) : (
          <Checkbox id={item.id} checked={item.checked} />
        ))}
      {avatar ?? <PrivilegeAvatar avatar={item.avatar} className="shrink-0" />}
      <span className="min-w-0 truncate text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70">
        {item.label}
      </span>
    </div>
  );
}

type ItemWrapperProps = {
  onClick?(): void;
  className?: string;
} & PropsWithChildren;
export function ItemWrapper({
  onClick,
  children,
  className,
}: ItemWrapperProps) {
  return (
    <li
      className={cn(
        'm-0 flex cursor-pointer items-center justify-between rounded-md p-2 hover:bg-bg-card',
        className,
      )}
      onClick={onClick}
    >
      {children}
    </li>
  );
}

export function LeftCollaboratorItem({
  item,
  click,
  children,
  showCheckbox = true,
  avatar,
  hasChildren = false,
  expanded = false,
  expandLabel = 'Expand group',
  collapseLabel = 'Collapse group',
  onToggleExpanded,
  nestedItems,
  compact = false,
}: LeftCollaboratorItemProps) {
  const permissionItem = usePermissionItem(item.id);
  const isOwner = hasResourceOwnerPermission(permissionItem?.permissions);

  const handleClick = useCallback(() => {
    if (isOwner) {
      return;
    }
    click(item.id);
  }, [click, isOwner, item.id]);

  return (
    <ItemWrapper
      key={item.id}
      onClick={handleClick}
      className={cn(
        nestedItems ? 'flex-col items-stretch !p-0 hover:bg-transparent' : '',
        compact && !nestedItems && 'p-1',
      )}
    >
      <div
        className={cn(
          'flex min-w-0 items-center justify-between gap-2',
          compact && 'gap-1',
          nestedItems &&
            cn('rounded-md hover:bg-bg-card', compact ? 'p-1' : 'p-2'),
        )}
      >
        <div className="flex min-w-0 items-center gap-1">
          {hasChildren ? (
            <Button
              variant="ghost"
              size="icon-sm"
              className={cn('size-6 shrink-0', compact && 'size-5')}
              aria-label={expanded ? collapseLabel : expandLabel}
              aria-expanded={expanded}
              onClick={(event: MouseEvent<HTMLButtonElement>) => {
                event.stopPropagation();
                onToggleExpanded?.();
              }}
            >
              {expanded ? <ChevronDown /> : <ChevronRight />}
            </Button>
          ) : (
            <span className={cn('size-6 shrink-0', compact && 'size-5')} />
          )}
          <CollaboratorItem
            item={item}
            showCheckbox={showCheckbox}
            avatar={avatar}
          ></CollaboratorItem>
        </div>
        {children ?? <LeftPrivilegeLabel item={item}></LeftPrivilegeLabel>}
      </div>
      {nestedItems && (
        <div className="w-full" onClick={(event) => event.stopPropagation()}>
          {nestedItems}
        </div>
      )}
    </ItemWrapper>
  );
}

export function LeftMemberItem({
  item,
  children,
  showCheckbox = true,
}: LeftCollaboratorItemProps) {
  const { handleClick: click } = useContext(MemberContext);
  const permissionItem = usePermissionItem(item.id);
  const isOwner = hasResourceOwnerPermission(permissionItem?.permissions);

  const checkedListMap = useContext(CheckedListContext);
  const checkedList = checkedListMap.memberCheckedList;

  const checked = useMemo(() => {
    const x = checkedList.find((x) => x.id === item.id);
    if (x) {
      return x.checked;
    }
    return false;
  }, [checkedList, item.id]);

  const nextItem = useMemo(() => {
    return { ...item, checked };
  }, [checked, item]);

  const handleClick = useCallback(() => {
    if (isOwner) {
      return;
    }
    click(item.id, !checked);
  }, [checked, click, isOwner, item.id]);

  return (
    <ItemWrapper key={item.id} onClick={handleClick}>
      <CollaboratorItem
        item={nextItem}
        showCheckbox={showCheckbox}
      ></CollaboratorItem>
      {children ? (
        children
      ) : (
        <LeftPrivilegeLabel item={item}></LeftPrivilegeLabel>
      )}
    </ItemWrapper>
  );
}

type LeftDepartmentItemProps = LeftCollaboratorItemProps &
  Pick<ReturnType<typeof useSwitchBreadcrumb>, 'clickCollaborator'>;

export function LeftDepartmentItem({
  item,
  clickCollaborator,
}: LeftDepartmentItemProps) {
  const { handleClick: click } = useContext(DepartmentContext);
  const permissionItem = usePermissionItem(item.id);
  const isOwner = hasResourceOwnerPermission(permissionItem?.permissions);

  const checkedListMap = useContext(CheckedListContext);
  const checkedList = checkedListMap.departmentCheckedList;

  const checked = useMemo(() => {
    const x = checkedList.find((x) => x.id === item.id);
    if (x) {
      return x.checked;
    }
    return false;
  }, [checkedList, item.id]);

  const nextItem = useMemo(() => {
    return { ...item, checked };
  }, [checked, item]);

  const handleClick = useCallback(() => {
    if (isOwner) {
      return;
    }
    click(item.id, !checked);
  }, [checked, click, isOwner, item.id]);

  const handleArrowClick: MouseEventHandler<HTMLButtonElement> = useCallback(
    (e) => {
      e.stopPropagation();
      clickCollaborator(item);
    },
    [clickCollaborator, item],
  );

  return (
    <ItemWrapper onClick={handleClick}>
      <CollaboratorItem item={nextItem} showCheckbox></CollaboratorItem>
      <div className="flex items-center">
        <LeftPrivilegeLabel item={item}></LeftPrivilegeLabel>
        <Button variant="ghost" size={'icon'} onClick={handleArrowClick}>
          <ChevronRight />
        </Button>
      </div>
    </ItemWrapper>
  );
}
