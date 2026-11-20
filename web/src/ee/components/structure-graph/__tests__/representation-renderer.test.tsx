import { render } from '@testing-library/react';
import { CompilationTemplateKind } from '@/ee/constants/compilation';
import {
  ViewMode,
  StructureKinds,
  ViewModeGenerateTypeMap,
} from '@/pages/dataset/compilation/constants';
import { GenerateType } from '@/constants/knowledge';
import { CompilationTranslations } from '@/ee/locales/compilation';

import { adaptTimelineToX6Data } from '../adapters';
import { renderRepresentation } from '../representation-renderer';

const mockGraphRender = jest.fn();

jest.mock('../timeline-x6-graph', () => ({
  __esModule: true,
  default: (props: unknown) => {
    mockGraphRender(props);
    return null;
  },
}));
jest.mock('@/components/structure-graph/claim-badge', () => ({
  ClaimBadge: () => null,
}));

const Template = {
  kind: CompilationTemplateKind.Timeline,
  template_id: 'test',
  template_name: 'test',
  entities: [
    { id: 'time', name: '2026', type: 'timestamp' },
    { id: 'event', name: 'Event', source_chunk_ids: ['chunk'] },
  ],
  relations: [
    { from: 'time', to: 'event' },
    { from: 'missing', to: 'event' },
  ],
};

it('retains timeline rendering, timestamp shapes, valid edges, and sourced-node clicks', () => {
  const onNodeClick = jest.fn();
  const { rerender } = render(
    renderRepresentation({ template: Template, onNodeClick }),
  );
  const props = mockGraphRender.mock.calls.at(-1)![0];
  rerender(renderRepresentation({ template: Template, onNodeClick }));
  expect(mockGraphRender.mock.calls.at(-1)![0].onNodeClick).toBe(
    props.onNodeClick,
  );
  expect(props.show).toBe(true);
  expect(props.data.nodes.map((node: { shape: string }) => node.shape)).toEqual(
    ['circle', 'rect'],
  );
  expect(props.data.edges).toHaveLength(1);
  props.onNodeClick({ id: 'time', name: '2026' });
  expect(onNodeClick).not.toHaveBeenCalled();
  props.onNodeClick({
    id: 'event',
    name: 'Event',
    source_chunk_ids: ['chunk'],
  });
  expect(onNodeClick).toHaveBeenCalledWith({
    id: 'event',
    name: 'Event',
    source_chunk_ids: ['chunk'],
  });
});

it('normalizes Timeline labels with the shared entity adapter', () => {
  expect(
    adaptTimelineToX6Data({ ...Template, entities: [{ name: ' Event ' }] })
      .nodes[0],
  ).toMatchObject({ id: ' Event ', label: 'Event' });
});

it('registers Timeline in enterprise dataset compilation', () => {
  expect(StructureKinds).toContain(ViewMode.Timeline);
  expect(ViewModeGenerateTypeMap[ViewMode.Timeline]).toBe(
    GenerateType.Timeline,
  );
});

it('provides the enterprise Timeline translations', () => {
  expect(CompilationTranslations.en).toMatchObject({
    chunk: { representationKinds: { timeline: 'Timeline' } },
    knowledgeCompilation: { noStructureTimeline: 'No timeline yet' },
  });
});

it('leaves shared representations to the shared renderer', () => {
  expect(
    renderRepresentation({ template: { ...Template, kind: 'raptor' } }),
  ).toBeNull();
});
