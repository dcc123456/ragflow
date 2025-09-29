import { TeamRole } from '@/constants/team';
import { useContext } from 'react';
import { DepartmentContext } from '../context';
import { LeftDepartmentItem, LeftMemberItem } from './collaborator-item';
import { CollaboratorListContainer } from './collaborator-list-container';
import {
  useSwitchBreadcrumb,
  useWatchBreadcrumbChange,
} from './use-select-collaborator';

type DepartmentListProps = Pick<
  ReturnType<typeof useSwitchBreadcrumb>,
  'clickCollaborator' | 'latestBreadcrumb'
>;

export function DepartmentList({
  clickCollaborator,
  latestBreadcrumb,
}: DepartmentListProps) {
  const { list, handleClick, setId, setFetchDepartmentListParams } =
    useContext(DepartmentContext);

  useWatchBreadcrumbChange({
    latestBreadcrumb,
    setId,
    setFetchDepartmentListParams,
  });

  return (
    <CollaboratorListContainer>
      {list.map((x) =>
        x.role === TeamRole.Department ? (
          <LeftDepartmentItem
            item={x}
            key={x.id}
            click={handleClick}
            clickCollaborator={clickCollaborator}
          ></LeftDepartmentItem>
        ) : (
          <LeftMemberItem
            item={x}
            key={x.id}
            click={handleClick}
          ></LeftMemberItem>
        ),
      )}
    </CollaboratorListContainer>
  );
}
