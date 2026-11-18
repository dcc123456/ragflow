import { createContext } from 'react';
import { HandleClickType } from '@/ee/components/privilege-management/add-collaborator-dialog/use-select-collaborator';
import { CollaboratorItem } from '@/ee/components/privilege-management/interface';

export type CollaboratorContextType = {
  list: CollaboratorItem[];
  handleClick: HandleClickType;
};

export const MemberContext = createContext<CollaboratorContextType>(
  {} as CollaboratorContextType,
);

export const GroupContext = createContext<CollaboratorContextType>(
  {} as CollaboratorContextType,
);

export type CheckedList = {
  groupCheckedList: CollaboratorItem[];
  memberCheckedList: CollaboratorItem[];
};

export const CheckedListContext = createContext<CheckedList>({
  groupCheckedList: [],
  memberCheckedList: [],
});
