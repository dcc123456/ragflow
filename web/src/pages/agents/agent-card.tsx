import { HomeCard } from '@/components/home-card';
import { MoreButton } from '@/components/more-button';
import { SharedBadge } from '@/components/shared-badge';
import { Button } from '@/components/ui/button';
import { AgentCategory } from '@/constants/agent';
import { useNavigatePage } from '@/hooks/logic-hooks/navigate-hooks';
import { IFlow } from '@/interfaces/database/agent';
import { Route } from 'lucide-react';
import { AgentDropdown } from './agent-dropdown';
import { useRenameAgent } from './use-rename-agent';

export type DatasetCardProps = {
  data: IFlow;
} & Pick<ReturnType<typeof useRenameAgent>, 'showAgentRenameModal'> & {
    showPrivilegeModal(): void;
  };

export function AgentCard({
  data,
  showAgentRenameModal,
  showPrivilegeModal,
}: DatasetCardProps) {
  const { navigateToAgent } = useNavigatePage();

  return (
    <HomeCard
      data={{ ...data, name: data.title, description: data.description || '' }}
      moreDropdown={
        <AgentDropdown
          showAgentRenameModal={showAgentRenameModal}
          agent={data}
          showPrivilegeModal={showPrivilegeModal}
        >
          <MoreButton />
        </AgentDropdown>
      }
      sharedBadge={<SharedBadge>{data.nickname}</SharedBadge>}
      onClick={navigateToAgent(data?.id, data.canvas_category as AgentCategory)}
      icon={
        data.canvas_category === AgentCategory.DataflowCanvas && (
          <Button variant={'ghost'} size={'sm'}>
            <Route />
          </Button>
        )
      }
    />
  );
}
