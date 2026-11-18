import { PropsWithChildren } from 'react';

export function CollaboratorListContainer({ children }: PropsWithChildren) {
  return <ul className="h-full min-h-0 overflow-y-auto">{children}</ul>;
}
