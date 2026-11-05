import { useContext } from 'react';
import { GroupContext } from '@/ee/components/privilege-management/context';
import { LeftCollaboratorItem } from '@/ee/components/privilege-management/add-collaborator-dialog/collaborator-item';
import { CollaboratorListContainer } from '@/ee/components/privilege-management/add-collaborator-dialog/collaborator-list-container';

export function GroupList() {
  const { list, handleClick } = useContext(GroupContext);

  return (
    <CollaboratorListContainer>
      {list.map((x) => (
        <LeftCollaboratorItem
          item={x}
          key={x.id}
          click={handleClick}
        ></LeftCollaboratorItem>
      ))}
    </CollaboratorListContainer>
  );
}
