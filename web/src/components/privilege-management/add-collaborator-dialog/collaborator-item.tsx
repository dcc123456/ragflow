import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { TeamApiAction } from '@/hooks/use-team';
import { IPermission } from '@/interfaces/database/team';
import { useQueryClient } from '@tanstack/react-query';
import { ChevronRight } from 'lucide-react';
import {
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
} from '../context';
import { CollaboratorItem as ICollaboratorItem } from '../interface';
import { PrivilegeAvatar } from '../privilege-avatar';
import { PrivilegeLabel } from '../privilege-label';
import { useSwitchBreadcrumb } from './use-select-collaborator';

type LeftCollaboratorItemProps = {
  item: ICollaboratorItem;
  click: (id: string, value?: boolean) => void;
  showCheckbox?: boolean;
  avatar?: ReactNode;
} & PropsWithChildren;

type CollaboratorItemProps = Pick<
  LeftCollaboratorItemProps,
  'item' | 'showCheckbox' | 'avatar'
>;

function LeftPrivilegeLabel({ item }: { item: ICollaboratorItem }) {
  const queryClient = useQueryClient();
  const data = queryClient.getQueriesData<IPermission[]>({
    queryKey: [TeamApiAction.ListPermission],
  });
  const permissionItems = data.at(0)?.at(1);

  const permissionItem = permissionItems?.find(
    (x) => (x as IPermission)?.id === item.id,
  );

  if (permissionItem) {
    return (
      <PrivilegeLabel
        permissions={(permissionItem as IPermission)?.permissions}
      ></PrivilegeLabel>
    );
  }
  return null;
}

export function CollaboratorItem({
  item,
  showCheckbox,
  avatar,
}: CollaboratorItemProps) {
  return (
    <div className="flex gap-2 items-center">
      {showCheckbox && <Checkbox id={item.id} checked={item.checked} />}
      {avatar || <PrivilegeAvatar avatar={item.avatar}></PrivilegeAvatar>}
      <span className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70">
        {item.label}
      </span>
    </div>
  );
}

type ItemWrapperProps = {
  onClick?(): void;
} & PropsWithChildren;
export function ItemWrapper({ onClick, children }: ItemWrapperProps) {
  return (
    <li
      className="flex justify-between items-center m-0 p-2 hover:bg-slate-50  hover:dark:bg-gray-800 rounded cursor-pointer"
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
}: LeftCollaboratorItemProps) {
  const handleClick = useCallback(() => {
    click(item.id);
  }, [click, item]);

  return (
    <ItemWrapper key={item.id} onClick={handleClick}>
      <CollaboratorItem
        item={item}
        showCheckbox={showCheckbox}
        avatar={avatar}
      ></CollaboratorItem>
      {children ? (
        children
      ) : (
        <LeftPrivilegeLabel item={item}></LeftPrivilegeLabel>
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

  const checkedListMap = useContext(CheckedListContext);
  const checkedList = checkedListMap.memberCheckedList;

  const checked = useMemo(() => {
    const x = checkedList.find((x) => x.id === item.id);
    if (x) {
      return x.checked;
    }
    return false;
    // return item.checked;
  }, [checkedList, item.id]);

  const nextItem = useMemo(() => {
    return { ...item, checked };
  }, [checked, item]);

  const handleClick = useCallback(() => {
    click(item.id, !checked);
  }, [checked, click, item.id]);

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

  const checkedListMap = useContext(CheckedListContext);
  const checkedList = checkedListMap.departmentCheckedList;

  const checked = useMemo(() => {
    const x = checkedList.find((x) => x.id === item.id);
    if (x) {
      return x.checked;
    }
    return false;
    // return item.checked;
  }, [checkedList, item.id]);

  const nextItem = useMemo(() => {
    return { ...item, checked };
  }, [checked, item]);

  const handleClick = useCallback(() => {
    click(item.id, !checked);
  }, [checked, click, item.id]);

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
