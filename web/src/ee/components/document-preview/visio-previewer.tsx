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

import { Spin } from '@/components/ui/spin';
import request from '@/utils/request';
import classNames from 'classnames';
import {
  ChevronLeft,
  ChevronRight,
  Maximize2,
  ZoomIn,
  ZoomOut,
} from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  loadVisio,
  renderPage,
  type VisioDocument,
  type VisioPage,
} from 'visio-react-viewer';

const MAX_VISIO_BYTES = 32 * 1024 * 1024;
const VISIO_DPI = 96;
const MIN_ZOOM = 0.25;
const MAX_ZOOM = 4;
const ZOOM_STEP = 0.25;

export interface VisioPreviewerProps {
  className?: string;
  url?: string;
  file?: File | Blob;
  /** [[displayPageNum(1-based), visioPageID(int), 0, 0, 0]] */
  positions?: number[][];
}

export interface VisioTargetPageResolution {
  pageIndex: number;
  fallbackReason?: 'target_page_not_found' | 'target_page_out_of_range';
}

interface NavigableVisioPage {
  docIndex: number;
  page: VisioPage;
}

type VisioPreviewError =
  | { kind: 'key'; value: string }
  | { kind: 'raw'; value: string };

/**
 * Resolves the 0-based `VisioDocument.pages` index for a chunk's `positions`
 * tuple. `visio-react-viewer` / `ooxml-core` indexes `pages` including
 * background pages, whereas RAGFlow indexes foreground pages 1-based
 * (`displayPageNum`) plus the raw Visio `Page.ID` (`visioPageID`).
 */
export function resolveVisioTargetPageIndex(
  pages: readonly Pick<VisioPage, 'id' | 'isBackground'>[],
  positions?: number[][],
): VisioTargetPageResolution {
  if (pages.length === 0) {
    return { pageIndex: 0 };
  }
  const firstForegroundIndex = pages.findIndex((page) => !page.isBackground);
  const safeDefaultIndex = firstForegroundIndex >= 0 ? firstForegroundIndex : 0;

  const firstPos = positions?.[0];
  if (!firstPos || firstPos.length === 0) {
    return { pageIndex: safeDefaultIndex };
  }

  const displayPageNum = Math.trunc(firstPos[0] ?? 0);
  const hasVisioPageId = firstPos.length >= 2 && Number.isFinite(firstPos[1]);
  const visioPageId = hasVisioPageId ? String(Math.trunc(firstPos[1])) : '';

  if (visioPageId !== '') {
    const byId = pages.findIndex(
      (page) => !page.isBackground && page.id === visioPageId,
    );
    if (byId >= 0) {
      return { pageIndex: byId };
    }
    return {
      pageIndex: safeDefaultIndex,
      fallbackReason: 'target_page_not_found',
    };
  }

  if (displayPageNum >= 1) {
    let foregroundCount = 0;
    for (let i = 0; i < pages.length; i++) {
      if (!pages[i].isBackground) {
        foregroundCount++;
        if (foregroundCount === displayPageNum) {
          return { pageIndex: i };
        }
      }
    }
    return {
      pageIndex: safeDefaultIndex,
      fallbackReason: 'target_page_out_of_range',
    };
  }

  return { pageIndex: safeDefaultIndex };
}

export const VisioPreviewer: React.FC<VisioPreviewerProps> = ({
  className,
  url,
  file,
  positions,
}) => {
  const { t } = useTranslation();

  const canvasHostRef = useRef<HTMLDivElement | null>(null);
  const [visioDoc, setVisioDoc] = useState<VisioDocument | null>(null);
  const [pageOverride, setPageOverride] = useState<{
    doc: VisioDocument;
    positionsKey: string;
    docIndex: number;
  } | null>(null);
  // `null` means responsive fit-to-viewport scaling.
  const [zoomState, setZoomState] = useState<{
    doc: VisioDocument;
    positionsKey: string;
    value: number | null;
  } | null>(null);
  const [loading, setLoading] = useState(true);
  const [errorState, setErrorState] = useState<VisioPreviewError | null>(null);

  const positionsKey = useMemo(
    () => JSON.stringify(positions ?? null),
    [positions],
  );

  useEffect(() => {
    let cancelled = false;

    const loadDrawing = async () => {
      if (!file && !url) {
        setVisioDoc(null);
        setLoading(false);
        return;
      }
      setLoading(true);
      setErrorState(null);
      try {
        let bytes: Uint8Array;
        if (file) {
          if (file.size > MAX_VISIO_BYTES) {
            setVisioDoc(null);
            setErrorState({ kind: 'key', value: 'visioPreview.fileTooLarge' });
            return;
          }
          bytes = new Uint8Array(await file.arrayBuffer());
        } else {
          const res = await request(url!, {
            method: 'GET',
            responseType: 'blob',
          });
          if (!res?.data) {
            setVisioDoc(null);
            setErrorState({ kind: 'key', value: 'visioPreview.fetchFailed' });
            return;
          }
          bytes = new Uint8Array(await res.data.arrayBuffer());
          if (bytes.byteLength > MAX_VISIO_BYTES) {
            setVisioDoc(null);
            setErrorState({ kind: 'key', value: 'visioPreview.fileTooLarge' });
            return;
          }
        }
        const parsed = await loadVisio(bytes);
        if (cancelled) {
          return;
        }
        setVisioDoc(parsed);
      } catch (err) {
        if (!cancelled) {
          setVisioDoc(null);
          setErrorState(
            err instanceof Error
              ? { kind: 'raw', value: err.message }
              : { kind: 'key', value: 'visioPreview.loadFailed' },
          );
        }
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    };

    void loadDrawing();
    return () => {
      cancelled = true;
    };
  }, [file, url]);

  const navigablePages = useMemo<NavigableVisioPage[]>(() => {
    if (!visioDoc?.pages?.length) {
      return [];
    }
    const foreground = visioDoc.pages
      .map((page, docIndex) => ({ page, docIndex }))
      .filter(({ page }) => !page.isBackground);
    if (foreground.length > 0) {
      return foreground;
    }
    return visioDoc.pages.map((page, docIndex) => ({ page, docIndex }));
  }, [visioDoc]);

  const targetResolution = useMemo(() => {
    if (!visioDoc?.pages?.length) {
      return null;
    }
    return resolveVisioTargetPageIndex(visioDoc.pages, positions);
  }, [positions, visioDoc]);

  const hasManualPageSelection = Boolean(
    pageOverride &&
    pageOverride.doc === visioDoc &&
    pageOverride.positionsKey === positionsKey,
  );

  const activeDocIndex = hasManualPageSelection
    ? pageOverride!.docIndex
    : (targetResolution?.pageIndex ?? 0);

  const zoom =
    zoomState &&
    zoomState.doc === visioDoc &&
    zoomState.positionsKey === positionsKey
      ? zoomState.value
      : null;

  const currentNavPosition = useMemo(() => {
    if (navigablePages.length === 0) {
      return -1;
    }
    const exact = navigablePages.findIndex(
      (item) => item.docIndex === activeDocIndex,
    );
    return exact >= 0 ? exact : 0;
  }, [activeDocIndex, navigablePages]);

  const activePage =
    currentNavPosition >= 0 ? navigablePages[currentNavPosition].page : null;

  const renderFailedMessage = t('visioPreview.renderFailed');

  useEffect(() => {
    const host = canvasHostRef.current;
    if (!host) {
      return;
    }
    if (!visioDoc || !activePage) {
      host.replaceChildren();
      return;
    }

    let rendered: ReturnType<typeof renderPage> | null = null;
    try {
      rendered = renderPage(visioDoc, activePage, { static: true });
      const { svg } = rendered;
      svg.style.display = 'block';
      svg.style.width = '100%';
      svg.style.height = '100%';
      host.replaceChildren(svg);
    } catch (err) {
      const alertNode = host.ownerDocument.createElement('div');
      alertNode.setAttribute('role', 'alert');
      alertNode.className = 'p-4 text-sm text-state-error break-words';
      alertNode.textContent =
        err instanceof Error ? err.message : renderFailedMessage;
      host.replaceChildren(alertNode);
    }

    return () => {
      rendered?.dispose();
      host.replaceChildren();
    };
  }, [activePage, renderFailedMessage, visioDoc]);

  const errorMessage = errorState
    ? errorState.kind === 'key'
      ? t(errorState.value)
      : errorState.value
    : null;

  const selectDocIndex = (docIndex: number) => {
    if (!visioDoc) {
      return;
    }
    setPageOverride({ doc: visioDoc, positionsKey, docIndex });
  };

  const goToNavPosition = (nextPos: number) => {
    if (nextPos < 0 || nextPos >= navigablePages.length) {
      return;
    }
    selectDocIndex(navigablePages[nextPos].docIndex);
  };

  const updateZoom = (nextValue: number | null) => {
    if (!visioDoc) {
      return;
    }
    setZoomState({ doc: visioDoc, positionsKey, value: nextValue });
  };

  const handleZoomOut = () => {
    const base = zoom ?? 1;
    updateZoom(Math.max(MIN_ZOOM, Number((base - ZOOM_STEP).toFixed(2))));
  };

  const handleZoomIn = () => {
    const base = zoom ?? 1;
    updateZoom(Math.min(MAX_ZOOM, Number((base + ZOOM_STEP).toFixed(2))));
  };

  const zoomLabel =
    zoom === null ? t('visioPreview.fit') : `${Math.round(zoom * 100)}%`;

  const canvasSizeStyle = useMemo(() => {
    if (zoom === null || !activePage) {
      return undefined;
    }
    const baseWidth = Math.max(activePage.width || 8.5, 1) * VISIO_DPI;
    const baseHeight = Math.max(activePage.height || 11, 1) * VISIO_DPI;
    return {
      width: `${Math.round(baseWidth * zoom)}px`,
      height: `${Math.round(baseHeight * zoom)}px`,
    };
  }, [activePage, zoom]);

  return (
    <div
      className={classNames(
        'relative flex flex-col w-full h-full bg-background-paper border border-border-normal rounded-md overflow-hidden visio-previewer',
        className,
      )}
    >
      <div
        role="toolbar"
        aria-label={t('visioPreview.controlsLabel')}
        className="flex items-center justify-between gap-2 px-3 py-1.5 border-b border-border-normal bg-background-paper text-xs shrink-0"
      >
        <div className="flex items-center gap-1.5 min-w-0">
          <button
            type="button"
            aria-label={t('visioPreview.previousPage')}
            onClick={() => goToNavPosition(currentNavPosition - 1)}
            disabled={currentNavPosition <= 0 || loading}
            className="p-1 rounded hover:bg-background-card disabled:opacity-40 disabled:pointer-events-none"
          >
            <ChevronLeft className="w-4 h-4" />
          </button>
          <select
            aria-label={t('visioPreview.pageSelect')}
            value={
              currentNavPosition >= 0
                ? navigablePages[currentNavPosition].docIndex
                : 0
            }
            onChange={(e) => selectDocIndex(Number(e.target.value))}
            disabled={navigablePages.length === 0 || loading}
            className="max-w-[220px] px-2 py-1 rounded border border-border-normal bg-background-paper text-xs truncate disabled:opacity-50"
          >
            {navigablePages.length === 0 ? (
              <option value={0}>—</option>
            ) : (
              navigablePages.map(({ page, docIndex }, idx) => (
                <option key={`${page.id}-${docIndex}`} value={docIndex}>
                  {`${idx + 1}. ${page.name || t('visioPreview.pageFallback', { index: idx + 1 })}`}
                </option>
              ))
            )}
          </select>
          <button
            type="button"
            aria-label={t('visioPreview.nextPage')}
            onClick={() => goToNavPosition(currentNavPosition + 1)}
            disabled={
              currentNavPosition < 0 ||
              currentNavPosition >= navigablePages.length - 1 ||
              loading
            }
            className="p-1 rounded hover:bg-background-card disabled:opacity-40 disabled:pointer-events-none"
          >
            <ChevronRight className="w-4 h-4" />
          </button>
          {navigablePages.length > 0 && (
            <span className="text-text-secondary whitespace-nowrap ml-1">
              {currentNavPosition + 1} / {navigablePages.length}
            </span>
          )}
        </div>

        <div className="flex items-center gap-1 shrink-0">
          <button
            type="button"
            aria-label={t('visioPreview.zoomOut')}
            onClick={handleZoomOut}
            disabled={!activePage || (zoom !== null && zoom <= MIN_ZOOM)}
            className="p-1 rounded hover:bg-background-card disabled:opacity-40 disabled:pointer-events-none"
          >
            <ZoomOut className="w-4 h-4" />
          </button>
          <button
            type="button"
            aria-label={t('visioPreview.fitToWindow')}
            onClick={() => updateZoom(null)}
            disabled={!activePage}
            className="inline-flex items-center gap-1 px-2 py-0.5 rounded border border-border-normal hover:bg-background-card disabled:opacity-40 disabled:pointer-events-none"
            title={t('visioPreview.fitToWindow')}
          >
            <Maximize2 className="w-3 h-3" />
            <span>{zoomLabel}</span>
          </button>
          <button
            type="button"
            aria-label={t('visioPreview.zoomIn')}
            onClick={handleZoomIn}
            disabled={!activePage || (zoom !== null && zoom >= MAX_ZOOM)}
            className="p-1 rounded hover:bg-background-card disabled:opacity-40 disabled:pointer-events-none"
          >
            <ZoomIn className="w-4 h-4" />
          </button>
        </div>
      </div>

      {targetResolution?.fallbackReason &&
        !hasManualPageSelection &&
        !errorMessage && (
          <div
            role="status"
            className="px-3 py-1.5 text-xs bg-amber-500/10 text-amber-700 dark:text-amber-300 border-b border-border-normal shrink-0"
          >
            {t('visioPreview.targetPageMissing')}
          </div>
        )}

      {errorMessage && (
        <div role="alert" className="p-4 text-sm text-state-error break-words">
          {errorMessage}
        </div>
      )}

      {loading && !errorMessage && (
        <div className="absolute inset-0 z-10 flex items-center justify-center bg-background-paper/70">
          <Spin />
        </div>
      )}

      <div className="flex-1 min-h-0 overflow-auto p-4 flex items-center justify-center bg-neutral-100 dark:bg-neutral-900">
        <div
          ref={canvasHostRef}
          data-testid="visio-svg-canvas"
          style={canvasSizeStyle}
          className={classNames(
            'bg-white shadow-sm rounded border border-border-normal',
            zoom === null ? 'w-full h-full' : 'shrink-0',
            !activePage && 'hidden',
          )}
        />
      </div>
    </div>
  );
};

export default VisioPreviewer;
