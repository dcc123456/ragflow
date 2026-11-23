import { render, screen } from '@testing-library/react';
import { ParserFormContent } from '.';

jest.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

function buildParserNode(
  setups: unknown,
): import('@/interfaces/database/agent').RAGFlowNodeType {
  return {
    id: 'Parser:PaperTrail',
    type: 'parserNode',
    position: { x: 0, y: 0 },
    data: { label: 'Parser', form: { setups } },
  } as import('@/interfaces/database/agent').RAGFlowNodeType;
}

describe('ParserFormContent', () => {
  it('replaces the empty CAD-only parser controls with a no-config message', () => {
    render(
      <ParserFormContent node={buildParserNode([{ fileFormat: 'cad' }])}>
        <span>Parser controls</span>
      </ParserFormContent>,
    );

    expect(screen.getByText('flow.cadParserNoConfig')).toBeInTheDocument();
    expect(screen.queryByText('Parser controls')).not.toBeInTheDocument();
  });

  it('replaces the empty Visio-only parser controls with a no-config message', () => {
    render(
      <ParserFormContent node={buildParserNode([{ fileFormat: 'visio' }])}>
        <span>Parser controls</span>
      </ParserFormContent>,
    );

    expect(screen.getByText('flow.visioParserNoConfig')).toBeInTheDocument();
    expect(screen.queryByText('Parser controls')).not.toBeInTheDocument();
  });

  it.each([
    [[{ fileFormat: 'cad' }, { fileFormat: 'pdf' }]],
    [[{ fileFormat: 'pdf' }]],
    [[]],
    [undefined],
  ])('preserves controls for non-CAD-only setups %#', (setups) => {
    render(
      <ParserFormContent node={buildParserNode(setups)}>
        <span>Parser controls</span>
      </ParserFormContent>,
    );

    expect(screen.getByText('Parser controls')).toBeInTheDocument();
    expect(
      screen.queryByText('flow.cadParserNoConfig'),
    ).not.toBeInTheDocument();
  });
});
