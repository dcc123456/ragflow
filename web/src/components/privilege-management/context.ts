import { createContext } from 'react';
import {
  HandleClickType,
  UseOperateDepartmentType,
} from './add-collaborator-dialog/use-select-collaborator';
import { CollaboratorItem } from './interface';

export type CollaboratorContextType = {
  list: CollaboratorItem[];
  handleClick: HandleClickType;
};

export const MemberContext = createContext<CollaboratorContextType>(
  {} as CollaboratorContextType,
);

export type DepartmentContextType = CollaboratorContextType &
  Pick<UseOperateDepartmentType, 'setId' | 'setFetchDepartmentListParams'>;

export const DepartmentContext = createContext<DepartmentContextType>(
  {} as DepartmentContextType,
);

export const GroupContext = createContext<CollaboratorContextType>(
  {} as CollaboratorContextType,
);

export type CheckedList = {
  groupCheckedList: CollaboratorItem[];
  departmentCheckedList: CollaboratorItem[];
  memberCheckedList: CollaboratorItem[];
};

export const CheckedListContext = createContext<CheckedList>({
  groupCheckedList: [],
  departmentCheckedList: [],
  memberCheckedList: [],
});
