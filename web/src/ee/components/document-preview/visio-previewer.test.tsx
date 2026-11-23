/*
 *  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
 *
 *  Licensed under the Apache License, Version 2.0 (the "License");
 *  you may not use this file except in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing, software
 *  distributed under the License is distributed on an "AS IS" BASIS,
 *  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  See the License for the specific language governing permissions and
 *  limitations under the License.
 */

import request from '@/utils/request';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { loadVisio, renderPage } from 'visio-react-viewer';
import { resolveVisioTargetPageIndex, VisioPreviewer } from './visio-previewer';

const mockDispose = jest.fn();

jest.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

jest.mock('@/utils/request', () => ({
  __esModule: true,
  default: jest.fn(),
}));

jest.mock('@/components/ui/spin', () => ({
  Spin: () => <div data-testid="spin" />,
}));

jest.mock('visio-react-viewer', () => ({
  loadVisio: jest.fn(),
  renderPage: jest.fn(),
}));

function buildMockVisioDoc() {
  return {
    pages: [
      {
        id: '99',
        name: 'Background',
        isBackground: true,
        width: 11,
        height: 8.5,
        shapes: [],
      },
      {
        id: '0',
        name: 'Overview',
        isBackground: false,
        width: 11,
        height: 8.5,
        shapes: [],
      },
      {
        id: '4',
        name: 'Microservices',
        isBackground: false,
        width: 11,
        height: 8.5,
        shapes: [],
      },
    ],
    diagnostics: [],
  };
}

describe('resolveVisioTargetPageIndex', () => {
  const pages = [
    { id: '99', isBackground: true },
    { id: '0', isBackground: false },
    { id: '4', isBackground: false },
    { id: '7', isBackground: false },
  ];

  it('matches foreground pages by original Visio page ID even when background pages exist', () => {
    expect(resolveVisioTargetPageIndex(pages, [[2, 4, 0, 0, 0]])).toEqual({
      pageIndex: 2,
    });
    expect(resolveVisioTargetPageIndex(pages, [[1, 0, 0, 0, 0]])).toEqual({
      pageIndex: 1,
    });
  });

  it('falls back to foreground display order when only displayPageNum is present', () => {
    expect(resolveVisioTargetPageIndex(pages, [[3]])).toEqual({
      pageIndex: 3,
    });
  });

  it('reports fallback when a replaced document no longer contains the referenced Visio page ID', () => {
    expect(resolveVisioTargetPageIndex(pages, [[2, 888, 0, 0, 0]])).toEqual({
      pageIndex: 1,
      fallbackReason: 'target_page_not_found',
    });
  });

  it('never targets a background page as default', () => {
    expect(resolveVisioTargetPageIndex(pages)).toEqual({ pageIndex: 1 });
  });
});

describe('VisioPreviewer (read-only SVG renderer)', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    (loadVisio as jest.Mock).mockResolvedValue(buildMockVisioDoc());
    (renderPage as jest.Mock).mockImplementation((_doc, page) => {
      const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
      svg.setAttribute('data-page-id', page.id);
      return { svg, warnings: [], dispose: mockDispose };
    });
  });

  it('fetches authenticated bytes from url, renders read-only SVG, and jumps to referenced foreground page', async () => {
    const payload = new Uint8Array([80, 75, 3, 4]);
    (request as unknown as jest.Mock).mockResolvedValue({
      data: {
        arrayBuffer: async () => payload.buffer,
      },
    });

    render(
      <VisioPreviewer
        url="/api/v1/documents/doc1/preview"
        positions={[[2, 4, 0, 0, 0]]}
      />,
    );

    await waitFor(() => {
      expect(request).toHaveBeenCalledWith('/api/v1/documents/doc1/preview', {
        method: 'GET',
        responseType: 'blob',
      });
      expect(loadVisio).toHaveBeenCalledTimes(1);
      expect(renderPage).toHaveBeenCalled();
    });

    const canvas = screen.getByTestId('visio-svg-canvas');
    expect(canvas.querySelector('svg')).toHaveAttribute('data-page-id', '4');
    // Background page should not be exposed in the foreground page switcher.
    const select = screen.getByLabelText(
      'visioPreview.pageSelect',
    ) as HTMLSelectElement;
    expect(Array.from(select.options).map((o) => o.text)).toEqual([
      '1. Overview',
      '2. Microservices',
    ]);
    expect(screen.getByText('2 / 2')).toBeInTheDocument();
  });

  it('supports previewing a local File directly and navigating between foreground pages and zooming', async () => {
    const file = {
      size: 4,
      arrayBuffer: async () => new Uint8Array([80, 75, 3, 4]).buffer,
    } as unknown as File;

    render(<VisioPreviewer file={file} />);

    await waitFor(() => {
      expect(
        screen.getByTestId('visio-svg-canvas').querySelector('svg'),
      ).toHaveAttribute('data-page-id', '0');
    });
    expect(request).not.toHaveBeenCalled();

    fireEvent.click(screen.getByLabelText('visioPreview.nextPage'));
    await waitFor(() => {
      expect(
        screen.getByTestId('visio-svg-canvas').querySelector('svg'),
      ).toHaveAttribute('data-page-id', '4');
    });

    fireEvent.click(screen.getByLabelText('visioPreview.zoomIn'));
    expect(screen.getByText('125%')).toBeInTheDocument();
    fireEvent.click(screen.getByLabelText('visioPreview.fitToWindow'));
    expect(screen.getByText('visioPreview.fit')).toBeInTheDocument();
  });

  it('shows a manual selection notice when the referenced Visio page ID is missing and dismisses it after manual page navigation', async () => {
    const file = {
      size: 4,
      arrayBuffer: async () => new Uint8Array([80, 75, 3, 4]).buffer,
    } as unknown as File;

    render(<VisioPreviewer file={file} positions={[[2, 999, 0, 0, 0]]} />);

    await waitFor(() => {
      expect(renderPage).toHaveBeenCalled();
    });
    expect(screen.getByRole('status')).toHaveTextContent(
      'visioPreview.targetPageMissing',
    );

    fireEvent.click(screen.getByLabelText('visioPreview.nextPage'));
    await waitFor(() => {
      expect(screen.queryByRole('status')).not.toBeInTheDocument();
    });
  });

  it('shows an error alert when parsing fails', async () => {
    (loadVisio as jest.Mock).mockRejectedValue(
      new Error('Corrupt vsdx archive'),
    );
    const file = {
      size: 4,
      arrayBuffer: async () => new Uint8Array([80, 75, 3, 4]).buffer,
    } as unknown as File;

    render(<VisioPreviewer file={file} />);

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Corrupt vsdx archive',
    );
  });
});
