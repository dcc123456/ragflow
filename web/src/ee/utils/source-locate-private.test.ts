import { supportsSourceLocate } from '@/utils/source-locate';
import type { IHighlight } from 'react-pdf-highlighter';
import { getSourceLocatePageNumber } from './source-locate-private';

const createHighlight = (pageNumber: number): IHighlight => ({
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

describe('CAD citation source locate', () => {
  it.each(['dwg', 'dxf', 'dxfb', 'DWG', 'DxFb'])(
    'opens %s citations in the in-app document preview',
    (extension) => {
      expect(supportsSourceLocate(extension)).toBe(true);
    },
  );

  it('does not change unsupported document behavior', () => {
    expect(supportsSourceLocate('html')).toBe(false);
    expect(supportsSourceLocate()).toBe(false);
  });
});

describe('getSourceLocatePageNumber', () => {
  it('uses the first highlight page', () => {
    expect(
      getSourceLocatePageNumber([createHighlight(3), createHighlight(4)]),
    ).toBe(3);
  });

  it('falls back to the first page without highlights', () => {
    expect(getSourceLocatePageNumber([])).toBe(1);
    expect(getSourceLocatePageNumber()).toBe(1);
  });
});
