import { Spin } from '@/components/ui/spin';
import { isVisioDocumentExtension } from '@/ee/utils/visio-document';
import { lazy, Suspense } from 'react';
import type { VisioPreviewerProps } from './visio-previewer';

const LazyVisioPreviewer = lazy(() => import('./visio-previewer'));

export type VisioPreviewProps = VisioPreviewerProps & {
  fileType?: string;
};

export function VisioPreview({ fileType, ...previewProps }: VisioPreviewProps) {
  if (!isVisioDocumentExtension(fileType) && !previewProps.file) {
    return null;
  }

  return (
    <section className="h-full">
      <Suspense
        fallback={
          <div className="flex items-center justify-center h-full">
            <Spin />
          </div>
        }
      >
        <LazyVisioPreviewer {...previewProps} />
      </Suspense>
    </section>
  );
}
