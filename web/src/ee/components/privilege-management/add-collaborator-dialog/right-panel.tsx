import { RAGFlowAvatar } from '@/components/ragflow-avatar';
import { TeamRole } from '@/ee/constants/team';
import { X } from 'lucide-react';
import { CollaboratorItem } from '@/ee/components/privilege-management/interface';

type RightPanelProps = {
  list: CollaboratorItem[];
  click(item: CollaboratorItem): void;
};

export function RoleLabel({ label }: { label: string }) {
  return (
    <span className="text-sm bg-lime-200 dark:bg-lime-800 px-1 rounded-md">
      {label}
    </span>
  );
}

export function RightPanel({ list, click }: RightPanelProps) {
  return (
    <ul className="space-y-2 overflow-auto max-h-[70vh]">
      {list.map((x) => (
        <li
          key={x.id}
          className="m-0 flex min-w-0 items-center justify-between gap-2 rounded-md p-2 hover:bg-bg-card"
          onClick={() => click(x)}
        >
          <div className="flex min-w-0 flex-1 items-center gap-2">
            <RAGFlowAvatar
              isPerson={x.role === TeamRole.Member}
              className="size-7 shrink-0"
              name={x.label}
              avatar={x.avatar}
            />
            <span className="min-w-0 truncate">{x.label}</span>
            <RoleLabel label={x.role as string}></RoleLabel>
          </div>
          <X className="size-4 shrink-0 cursor-pointer" />
        </li>
      ))}
    </ul>
  );
}
