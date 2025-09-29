import { IGroup } from '@/interfaces/database/team';
import { createContext } from 'react';

export const GroupContext = createContext<null | ((record?: IGroup) => void)>(
  null,
);
