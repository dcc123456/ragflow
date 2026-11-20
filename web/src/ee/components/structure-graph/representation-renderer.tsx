import { useCallback } from 'react';
import type { RepresentationRendererProps } from '@/components/structure-graph/representation-renderer';
import { CompilationTemplateKind } from '@/ee/constants/compilation';
import { adaptTimelineToX6Data } from './adapters';
import TimelineX6Graph from './timeline-x6-graph';

function TimelineRepresentation({
  template,
  onNodeClick,
}: RepresentationRendererProps & {
  template: NonNullable<RepresentationRendererProps['template']>;
}) {
  const handleNodeClick = useCallback(
    (node: { id: string; name: string; source_chunk_ids?: string[] }) => {
      if (node.source_chunk_ids?.length) onNodeClick?.(node);
    },
    [onNodeClick],
  );
  return (
    <div className="mt-6 flex-1 min-h-0">
      <TimelineX6Graph
        data={adaptTimelineToX6Data(template)}
        show
        onNodeClick={handleNodeClick}
      />
    </div>
  );
}

export function renderRepresentation(props: RepresentationRendererProps) {
  if (props.template?.kind !== CompilationTemplateKind.Timeline) return null;
  return <TimelineRepresentation {...props} template={props.template} />;
}
