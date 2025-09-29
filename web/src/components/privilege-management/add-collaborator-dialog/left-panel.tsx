import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbList,
  BreadcrumbSeparator,
} from '@/components/ui/breadcrumb';
import { Checkbox } from '@/components/ui/checkbox';
import { TeamRole } from '@/constants/team';
import { ChevronRight, Network, User, UsersRound } from 'lucide-react';
import { useCallback } from 'react';
import { LeftCollaboratorItem } from './collaborator-item';
import { DepartmentList } from './department-list';
import { GroupList } from './group-list';
import { MemberList } from './member-list';
import { useSwitchBreadcrumb } from './use-select-collaborator';

const items = [
  { id: TeamRole.Member, label: 'member' },
  { id: TeamRole.Group, label: 'group' },
  { id: TeamRole.Department, label: 'department' },
];

const IconMap = {
  [TeamRole.Department]: <Network />,
  [TeamRole.Group]: <User />,
  [TeamRole.Member]: <UsersRound />,
};

interface LeftPanelProps {
  selectAll(role: TeamRole, value: boolean): void;
}

export function LeftPanel({ selectAll }: LeftPanelProps) {
  const {
    clickBreadcrumb,
    clickCollaborator,
    breadcrumbs,
    secondBreadcrumb,
    latestBreadcrumb,
  } = useSwitchBreadcrumb();

  const handleSelectAllChange = useCallback(
    (checked: boolean) => {
      selectAll(secondBreadcrumb as TeamRole, checked);
    },
    [secondBreadcrumb, selectAll],
  );

  return (
    <section>
      {breadcrumbs.length > 0 && (
        <Breadcrumb className="mb-2">
          <BreadcrumbList>
            {breadcrumbs.map((x, idx) => (
              <div key={x.id} className="flex items-center gap-1">
                <BreadcrumbItem
                  className="cursor-pointer"
                  onClick={() =>
                    idx !== breadcrumbs.length - 1
                      ? clickBreadcrumb(idx)
                      : () => {}
                  }
                >
                  <span>{x.label}</span>
                </BreadcrumbItem>
                {idx !== breadcrumbs.length - 1 && <BreadcrumbSeparator />}
              </div>
            ))}
            {/* <BreadcrumbItem>
            <DropdownMenu>
              <DropdownMenuTrigger className="flex items-center gap-1">
                <BreadcrumbEllipsis className="h-4 w-4" />
                <span className="sr-only">Toggle menu</span>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start">
                <DropdownMenuItem>Documentation</DropdownMenuItem>
                <DropdownMenuItem>Themes</DropdownMenuItem>
                <DropdownMenuItem>GitHub</DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </BreadcrumbItem>
          <BreadcrumbSeparator />
          <BreadcrumbItem>
            <BreadcrumbLink href="/docs/components">Components</BreadcrumbLink>
          </BreadcrumbItem>
          <BreadcrumbSeparator />
          <BreadcrumbItem>
            <BreadcrumbPage>Breadcrumb</BreadcrumbPage>
          </BreadcrumbItem> */}
          </BreadcrumbList>
        </Breadcrumb>
      )}
      {secondBreadcrumb && (
        <div className="flex items-center space-x-2 pl-2 py-2 justify-end">
          <Checkbox id="terms" onCheckedChange={handleSelectAllChange} />
          <label
            htmlFor="terms"
            className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70"
          >
            Select all
          </label>
        </div>
      )}
      <ul>
        {breadcrumbs.length === 0 &&
          items.map((x) => (
            <LeftCollaboratorItem
              item={x}
              key={x.id}
              click={() => clickCollaborator(x)}
              showCheckbox={false}
              avatar={IconMap[x.id]}
            >
              <ChevronRight />
            </LeftCollaboratorItem>
          ))}
        {secondBreadcrumb === TeamRole.Member && <MemberList></MemberList>}
        {secondBreadcrumb === TeamRole.Department && (
          <DepartmentList
            clickCollaborator={clickCollaborator}
            latestBreadcrumb={latestBreadcrumb}
          ></DepartmentList>
        )}
        {secondBreadcrumb === TeamRole.Group && <GroupList></GroupList>}
      </ul>
    </section>
  );
}
