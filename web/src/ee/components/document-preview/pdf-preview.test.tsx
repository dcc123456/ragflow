import PdfPreview from '@/components/document-preview/pdf-preview';
import { render, waitFor } from '@testing-library/react';
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

const mockGetPage = jest.fn();

jest.mock('react-pdf-highlighter', () => ({
  AreaHighlight: () => null,
  Highlight: () => null,
  Popup: () => null,
  PdfHighlighter: () => <div data-testid="pdf-highlighter" />,
  PdfLoader: ({
    children,
  }: {
    children: (pdfDocument: { getPage: typeof mockGetPage }) => JSX.Element;
  }) => <>{children({ getPage: mockGetPage })}</>,
}));

jest.mock('@/components/document-preview/hooks', () => ({
  useCatchDocumentError: () => '',
}));

jest.mock('@/pages/document-viewer/file-error', () => ({
  __esModule: true,
  default: () => null,
}));

jest.mock('@/utils/authorization-util', () => ({
  getAuthorization: () => 'test-token',
}));

const PageSizes: Record<number, { width: number; height: number }> = {
  1: { width: 842, height: 595 },
  3: { width: 842, height: 280 },
  4: { width: 600, height: 900 },
};

const createHighlight = (pageNumber: number): TestHighlight => ({
  id: `highlight-${pageNumber}`,
  comment: { text: '', emoji: '' },
  content: { text: 'CAD chunk' },
  position: {
    pageNumber,
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
});

beforeEach(() => {
  mockGetPage.mockReset();
  mockGetPage.mockImplementation((pageNumber: number) => {
    const size = PageSizes[pageNumber] ?? PageSizes[1];
    return Promise.resolve({
      getViewport: ({ scale }: { scale: number }) => ({
        width: size.width * scale,
        height: size.height * scale,
      }),
    });
  });
});

describe('PdfPreview source locate page', () => {
  it('loads the first page when there are no highlights', async () => {
    const setWidthAndHeight = jest.fn();

    render(
      <PdfPreview
        url="/api/v1/documents/doc-1/preview"
        setWidthAndHeight={setWidthAndHeight}
      />,
    );

    await waitFor(() => expect(mockGetPage).toHaveBeenCalledWith(1));
    await waitFor(() =>
      expect(setWidthAndHeight).toHaveBeenCalledWith(842, 595),
    );
  });

  it('loads the highlighted page and follows a changed selection', async () => {
    const setWidthAndHeight = jest.fn();
    const preview = (highlights: TestHighlight[]) => (
      <PdfPreview
        url="/api/v1/documents/doc-1/preview"
        highlights={highlights}
        setWidthAndHeight={setWidthAndHeight}
      />
    );

    const { rerender } = render(preview([createHighlight(3)]));

    await waitFor(() => expect(mockGetPage).toHaveBeenCalledWith(3));
    await waitFor(() =>
      expect(setWidthAndHeight).toHaveBeenCalledWith(842, 280),
    );

    rerender(preview([createHighlight(4)]));

    await waitFor(() => expect(mockGetPage).toHaveBeenCalledWith(4));
    await waitFor(() =>
      expect(setWidthAndHeight).toHaveBeenCalledWith(600, 900),
    );
  });
});
