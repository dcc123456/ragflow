import { PropsWithChildren } from 'react';

export function CollaboratorListContainer({ children }: PropsWithChildren) {
  return <ul className="overflow-auto max-h-[70vh]">{children}</ul>;
}
