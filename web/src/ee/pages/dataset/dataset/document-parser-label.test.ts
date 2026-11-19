import { getDocumentParserLabel } from '@/pages/dataset/dataset/utils';

jest.mock('@/locales/config', () => ({
  __esModule: true,
  default: { t: () => 'CAD Drawing' },
}));

describe('CAD document parser label', () => {
  it('uses the CAD Drawing display name without changing the parser id', () => {
    expect(
      getDocumentParserLabel({
        pipeline_id: '',
        pipeline_name: '',
        chunk_method: 'cad',
      }),
    ).toBe('CAD Drawing');
  });

  it('does not replace a custom pipeline title', () => {
    expect(
      getDocumentParserLabel({
        pipeline_id: 'pipeline-1',
        pipeline_name: 'Engineering drawings',
        chunk_method: 'cad',
      }),
    ).toBe('Engineering drawings');
  });

  it('keeps the existing fallback for other built-in parsers', () => {
    expect(
      getDocumentParserLabel({
        pipeline_id: '',
        pipeline_name: '',
        chunk_method: 'paper',
      }),
    ).toBe('Paper');
  });
});
