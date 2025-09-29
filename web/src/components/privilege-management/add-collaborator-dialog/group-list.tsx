import { useContext } from 'react';
import { GroupContext } from '../context';
import { LeftCollaboratorItem } from './collaborator-item';
import { CollaboratorListContainer } from './collaborator-list-container';

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
