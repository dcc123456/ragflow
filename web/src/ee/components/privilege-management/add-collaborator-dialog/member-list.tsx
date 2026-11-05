import { useContext } from 'react';
import { MemberContext } from '@/ee/components/privilege-management/context';
import { LeftMemberItem } from '@/ee/components/privilege-management/add-collaborator-dialog/collaborator-item';
import { CollaboratorListContainer } from '@/ee/components/privilege-management/add-collaborator-dialog/collaborator-list-container';

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
