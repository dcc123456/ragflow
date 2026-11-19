import { render, screen } from '@testing-library/react';
type TestHighlight = {
  id: string;
  comment: { text: string; emoji: string };
  content: { text: string };
  position: {
    pageNumber: number;
    boundingRect: {
      x1: number;
      x2: number;
      y1: number;
      y2: number;
      width: number;
      height: number;
    };
    rects: never[];
  };
};
import { CadPreview } from './cad-preview';

jest.mock('@/components/document-preview/pdf-preview', () => ({
  __esModule: true,
  default: ({
    className,
    url,
    highlights,
    setWidthAndHeight,
  }: {
    className?: string;
    url: string;
    highlights?: { position: { pageNumber: number } }[];
    setWidthAndHeight?: (width: number, height: number) => void;
  }) => (
    <div
      className={className}
      data-testid="pdf-preview"
      data-url={url}
      data-highlight-pages={highlights
        ?.map((highlight) => highlight.position.pageNumber)
        .join(',')}
      data-has-size-callback={Boolean(setWidthAndHeight)}
    ></div>
  ),
}));

const HighlightAtThirdPage: TestHighlight = {
  id: 'highlight-3',
  comment: { text: '', emoji: '' },
  content: { text: 'CAD chunk' },
  position: {
    pageNumber: 3,
    boundingRect: {
      x1: 10,
      x2: 20,
      y1: 15,
      y2: 25,
      width: 72,
      height: 72,
    },
    rects: [],
  },
};

describe('CadPreview', () => {
  it.each(['dwg', 'dxf', 'dxfb', 'DWG', 'DxFb'])(
    'renders the generated PDF for %s documents',
    (fileType) => {
      const url = `/api/v1/documents/${fileType}/preview`;

      render(
        <CadPreview
          className="cad-document-preview"
          fileType={fileType}
          url={url}
        />,
      );

      expect(screen.getByTestId('pdf-preview')).toHaveAttribute(
        'data-url',
        url,
      );
      expect(screen.getByTestId('pdf-preview')).toHaveClass(
        'cad-document-preview',
      );
    },
  );

  it('forwards chunk highlights and the page-size callback', () => {
    const setWidthAndHeight = jest.fn();

    render(
      <CadPreview
        fileType="dwg"
        url="/api/v1/documents/doc-1/preview"
        highlights={[HighlightAtThirdPage]}
        setWidthAndHeight={setWidthAndHeight}
      />,
    );

    expect(screen.getByTestId('pdf-preview')).toHaveAttribute(
      'data-highlight-pages',
      '3',
    );
    expect(screen.getByTestId('pdf-preview')).toHaveAttribute(
      'data-has-size-callback',
      'true',
    );
  });

  it.each(['pdf', 'docx', ''])(
    'leaves %s documents to the existing preview dispatch',
    (fileType) => {
      const { container } = render(
        <CadPreview
          fileType={fileType}
          url="/api/v1/documents/doc-1/preview"
        />,
      );

      expect(container).toBeEmptyDOMElement();
      expect(screen.queryByTestId('pdf-preview')).not.toBeInTheDocument();
    },
  );
});
