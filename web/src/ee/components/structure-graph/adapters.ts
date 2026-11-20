import { getEntityDisplayName } from '@/components/structure-graph/adapters';
import type {
  IStructureGraphEntity,
  IStructureGraphTemplate,
} from '@/interfaces/database/document-structure';

export interface TimelineX6NodeData {
  id: string;
  shape: 'rect' | 'circle';
  width: number;
  height: number;
  label: string;
  data?: IStructureGraphEntity;
  attrs?: Record<string, unknown>;
}

export interface TimelineX6EdgeData {
  id: string;
  shape: 'edge';
  source: string;
  target: string;
  attrs?: Record<string, unknown>;
}

export function adaptTimelineToX6Data(template: IStructureGraphTemplate): {
  nodes: TimelineX6NodeData[];
  edges: TimelineX6EdgeData[];
} {
  const normalized = template.entities
    .map((entity) => ({
      ...entity,
      id: entity.id ?? entity.name ?? '',
      name: getEntityDisplayName(entity),
    }))
    .filter((entity) => entity.id);
  const entityIds = new Set(normalized.map((entity) => entity.id));

  const nodes = normalized.map((entity) => {
    const isTimestamp = entity.type === 'timestamp';
    return {
      id: entity.id,
      shape: isTimestamp ? ('circle' as const) : ('rect' as const),
      width: isTimestamp ? 40 : 200,
      height: isTimestamp ? 40 : 80,
      label: entity.name,
      data: entity,
      attrs: {
        body: {
          fill: isTimestamp ? 'rgb(var(--accent-primary))' : 'transparent',
          stroke: 'rgb(var(--accent-primary))',
          rx: isTimestamp ? 48 : 8,
          ry: isTimestamp ? 48 : 8,
        },
        label: {
          fill: isTimestamp
            ? 'rgb(var(--accent-primary))'
            : 'rgb(var(--text-primary))',
          fontSize: 24,
          ...(isTimestamp && {
            refX: '50%',
            refY: 0,
            refY2: -8,
            textAnchor: 'middle',
            textVerticalAnchor: 'bottom',
          }),
          textWrap: {
            width: isTimestamp ? 80 : 180,
            height: isTimestamp ? 80 : 64,
            ellipsis: true,
          },
        },
      },
    };
  });

  const edges = (template.relations ?? [])
    .filter(
      (relation) => entityIds.has(relation.from) && entityIds.has(relation.to),
    )
    .map((relation, index) => ({
      id: `timeline-edge-${index}`,
      shape: 'edge' as const,
      source: relation.from,
      target: relation.to,
      attrs: {
        line: {
          stroke: '#8c8c8c',
          strokeWidth: 1,
          targetMarker: 'classic',
        },
      },
    }));

  return { nodes, edges };
}
