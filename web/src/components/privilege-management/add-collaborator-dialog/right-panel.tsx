import { X } from 'lucide-react';
import { CollaboratorItem } from '../interface';
import { PrivilegeAvatar } from '../privilege-avatar';

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
          className="hover:bg-slate-100 hover:dark:bg-gray-800 rounded p-2 flex justify-between items-center !m-0"
          onClick={() => click(x)}
        >
          <div className="flex gap-2 items-center">
            <PrivilegeAvatar avatar={x.avatar}></PrivilegeAvatar>
            {x.label}
            <RoleLabel label={x.role as string}></RoleLabel>
          </div>
          <X className="size-4 cursor-pointer" />
        </li>
      ))}
    </ul>
  );
}
