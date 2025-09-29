import { useContext } from 'react';
import { MemberContext } from '../context';
import { LeftMemberItem } from './collaborator-item';
import { CollaboratorListContainer } from './collaborator-list-container';

export function MemberList() {
  const { list, handleClick } = useContext(MemberContext);

  return (
    <CollaboratorListContainer>
      {list.map((x) => (
        <LeftMemberItem
          item={x}
          key={x.id}
          click={handleClick}
        ></LeftMemberItem>
      ))}
    </CollaboratorListContainer>
  );
}
