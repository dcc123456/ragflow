import { TeamRole } from '@/ee/constants/team';
import { useContext } from 'react';
import { DepartmentContext } from '@/ee/components/privilege-management/context';
import {
  LeftDepartmentItem,
  LeftMemberItem,
} from '@/ee/components/privilege-management/add-collaborator-dialog/collaborator-item';
import { CollaboratorListContainer } from '@/ee/components/privilege-management/add-collaborator-dialog/collaborator-list-container';
import {
  useSwitchBreadcrumb,
  useWatchBreadcrumbChange,
} from '@/ee/components/privilege-management/add-collaborator-dialog/use-select-collaborator';

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
