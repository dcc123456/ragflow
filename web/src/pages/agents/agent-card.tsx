import { HomeCard } from '@/components/home-card';
import { MoreButton } from '@/components/more-button';
import { SharedBadge } from '@/components/shared-badge';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip';
import { AgentCategory } from '@/constants/agent';
import { useNavigatePage } from '@/hooks/logic-hooks/navigate-hooks';
import { useRef, useState } from 'react';
import { AgentListItemType, IFlow } from '@/interfaces/database/agent';
import { CanvasCategoryToFlowType, FlowType, FlowTypeConfig } from './constant';
import { AgentDropdown } from './agent-dropdown';
import { useRenameAgent } from './use-rename-agent';

export type DatasetCardProps = {
  data: IFlow;
} & Pick<ReturnType<typeof useRenameAgent>, 'showAgentRenameModal'> & {
    showPrivilegeModal(): void;
  };

function AgentTypeIcon({
  data,
}: {
  data: IFlow & { type?: AgentListItemType };
}) {
  const flowType =
    data.type === AgentListItemType.CompilationTemplateGroup
      ? FlowType.Compiler
      : CanvasCategoryToFlowType[data.canvas_category ?? ''];

  const icon = flowType ? FlowTypeConfig[flowType].icon : null;

  if (!icon) {
    return null;
  }

  const Icon = icon;

  return (
    <Button variant={'ghost'} size={'sm'}>
      <Icon />
    </Button>
  );
}

function AgentTags({ tags }: { tags?: string }) {
  const list = (tags || '')
    .split(',')
    .map((t) => t.trim())
    .filter(Boolean);
  const containerRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);

  if (list.length === 0) return null;

  const handleOpenChange = (isOpen: boolean) => {
    if (isOpen) {
      const el = containerRef.current;
      setOpen(el ? el.scrollHeight > el.clientHeight : false);
    } else {
      setOpen(false);
    }
  };

  return (
    <Tooltip open={open} onOpenChange={handleOpenChange}>
      <TooltipTrigger asChild>
        <div ref={containerRef} className="line-clamp-2 mt-1">
          {list.map((tag) => (
            <Badge
              key={tag}
              variant="secondary"
              className="text-xs font-normal me-1 mb-1"
            >
              {tag}
            </Badge>
          ))}
        </div>
      </TooltipTrigger>
      <TooltipContent>
        <div className="flex flex-wrap gap-1 max-w-[280px]">
          {list.map((tag) => (
            <Badge
              key={tag}
              variant="secondary"
              className="text-xs font-normal"
            >
              {tag}
            </Badge>
          ))}
        </div>
      </TooltipContent>
    </Tooltip>
  );
}

export function AgentCard({
  data,
  showAgentRenameModal,
  showPrivilegeModal,
}: DatasetCardProps) {
  const { navigateToAgent } = useNavigatePage();

  return (
    <HomeCard
      testId="agent-card"
      data={{
        ...data,
        name: data.title,
        description: data.description || '',
        release_time: data.release_time,
      }}
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
      onClick={
        // data.canvas_category === AgentCategory.DataflowCanvas
        //   ? navigateToDataflow(data.id)
        //   :
        navigateToAgent(data?.id, data.canvas_category as AgentCategory)
      }
      icon={<AgentTypeIcon data={data} />}
      extra={<AgentTags tags={data.tags} />}
      showReleaseTime
    />
  );
}
