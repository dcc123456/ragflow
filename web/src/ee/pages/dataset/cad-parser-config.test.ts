import { buildDslComponentsByGraph } from '@/pages/agent/utils';
import {
  buildParserConfigFromNodes,
  buildPipelineOperatorNodes,
  getOperatorType,
  transformFormConfigToApi,
  transformSavedParserConfigToForm,
} from '@/utils/pipeline-operator';

jest.mock('@/routes', () => ({ Routes: {} }));

const CadNodeId = 'CADChunker:SpatialSheet';

const CadDsl = {
  graph: {
    nodes: [
      {
        id: 'File',
        type: 'beginNode',
        position: { x: 0, y: 0 },
        data: { label: 'File', name: 'File', form: {} },
      },
      {
        id: CadNodeId,
        type: 'chunkerNode',
        position: { x: 100, y: 0 },
        data: {
          label: 'CADChunker',
          name: 'CAD Chunker',
          form: {
            vision: { enabled: false, llm_id: '', system_prompt: '' },
          },
        },
      },
      {
        id: 'Tokenizer:IndexText',
        type: 'tokenizerNode',
        position: { x: 200, y: 0 },
        data: {
          label: 'Tokenizer',
          name: 'Tokenizer',
          form: { fields: 'text' },
        },
      },
    ],
    edges: [
      {
        id: 'file-cad',
        source: 'File',
        target: CadNodeId,
      },
      {
        id: 'cad-tokenizer',
        source: CadNodeId,
        target: 'Tokenizer:IndexText',
      },
    ],
  },
} as unknown as import('@/interfaces/database/agent').DSL;

const SavedVision = {
  enabled: true,
  llm_id: 'model-B',
  system_prompt: 'Describe visible connections',
};

describe('CAD dataset and document parser_config round trip', () => {
  it('loads a saved document or dataset override over the built-in defaults', () => {
    const saved = {
      [CadNodeId]: { vision: SavedVision },
      'Tokenizer:IndexText': { fields: 'text' },
    };
    const nodes = buildPipelineOperatorNodes(CadDsl, saved);
    const cadNode = nodes.find((node) => node.data.operatorId === CadNodeId);

    expect(cadNode?.data.form).toEqual({ vision: SavedVision });
    const roundTrip = buildParserConfigFromNodes(nodes);
    expect(roundTrip[CadNodeId]).toEqual(saved[CadNodeId]);
    expect(roundTrip['Tokenizer:IndexText']).toMatchObject(
      saved['Tokenizer:IndexText'],
    );
  });

  it('saves the nested vision object unchanged for dataset and document requests', () => {
    const formConfig = transformSavedParserConfigToForm({
      [CadNodeId]: { vision: SavedVision },
    });

    expect(formConfig).toEqual({ [CadNodeId]: { vision: SavedVision } });
    expect(
      transformFormConfigToApi(
        getOperatorType(CadNodeId),
        formConfig?.[CadNodeId],
      ),
    ).toEqual({ vision: SavedVision });
  });

  it('uses built-in defaults when no document or dataset override exists', () => {
    const nodes = buildPipelineOperatorNodes(CadDsl);

    expect(buildParserConfigFromNodes(nodes)).toMatchObject({
      [CadNodeId]: {
        vision: { enabled: false, llm_id: '', system_prompt: '' },
      },
      'Tokenizer:IndexText': { fields: 'text' },
    });
  });

  it('serializes the CAD form into the canvas component params', () => {
    const cadNode = buildPipelineOperatorNodes(CadDsl, {
      [CadNodeId]: { vision: SavedVision },
    }).find((node) => node.data.operatorId === CadNodeId);
    expect(cadNode).toBeDefined();
    if (!cadNode) return;
    cadNode.id = CadNodeId;

    const components = buildDslComponentsByGraph([cadNode], [], {});
    expect(components[CadNodeId].obj).toMatchObject({
      component_name: 'CADChunker',
      params: { vision: SavedVision },
    });
  });
});
