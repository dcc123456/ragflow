import ListFilterBar from '@/components/list-filter-bar';
import { Segmented } from '@/components/ui/segmented';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router';
import { FileBreadcrumb } from '@/pages/files/file-breadcrumb';
import { FilesManager } from '@/ee/pages/files/file-manager';
import { EvaluationPage } from '@/ee/pages/files/evaluation';
import { useSelectBreadcrumbItems } from '@/pages/files/use-navigate-to-folder';
import { FileTabs, FilesInstanceType } from './constant';

/**
 * Enterprise files page: a tab wrapper (files / evaluation) above the file
 * manager. Upstream's `pages/files/index.tsx` stays byte-identical to
 * upstream and its `/files` route is overridden to render this ee page
 * instead (see `routes.tsx`). The shared page must never import from this
 * directory — only ee-forked wiring (routes, navigation hooks) reaches it.
 */
export default function Files() {
  const { t } = useTranslation();
  const [activeTab, setActiveTab] = useState<FileTabs>(FileTabs.FILE);

  const [searchUrl, setSearchUrl] = useSearchParams();
  const isEvaluation = searchUrl.get('type') === FileTabs.EVALUATION;
  useEffect(() => {
    if (isEvaluation) {
      searchUrl.delete('type');
      setSearchUrl(searchUrl);
      setActiveTab(FileTabs.EVALUATION);
    }
  }, [isEvaluation, searchUrl, setSearchUrl]);

  const options = [
    {
      value: FileTabs.FILE,
      label: t('fileManager.files'),
    },
    {
      value: FileTabs.EVALUATION,
      label: t('fileManager.evaluation.evaluation'),
    },
  ];
  const breadcrumbItems = useSelectBreadcrumbItems();

  const leftPanel = (
    <div>
      {breadcrumbItems.length > 0 ? (
        <FileBreadcrumb></FileBreadcrumb>
      ) : (
        // t('fileManager.files')
        <>
          <Segmented
            options={options}
            value={activeTab}
            onChange={(value) => setActiveTab(value as FileTabs)}
          ></Segmented>
        </>
      )}
    </div>
  );

  const [fileInstance, setFileInstance] = useState<FilesInstanceType>({
    searchString: '',
    onSearchChange: undefined,
    showFileUploadModal: undefined,
    showSyncFileModal: undefined,
    showFolderCreateModal: undefined,
  });

  return (
    <section className="p-8 flex flex-col gap-4 h-full">
      <ListFilterBar
        leftPanel={leftPanel}
        searchString={fileInstance.searchString}
        onSearchChange={fileInstance.onSearchChange}
        showFilter={false}
      >
        {activeTab === FileTabs.FILE ? (
          <FilesManager.FileUpload
            showFileUploadModal={fileInstance.showFileUploadModal || (() => {})}
            showSyncFileModal={fileInstance.showSyncFileModal || (() => {})}
            showFolderCreateModal={
              fileInstance.showFolderCreateModal || (() => {})
            }
          />
        ) : (
          <EvaluationPage.FileUpload
            showFileUploadModal={fileInstance.showFileUploadModal || (() => {})}
          />
        )}
      </ListFilterBar>

      {/* Content */}
      {activeTab === FileTabs.FILE ? (
        <FilesManager.Root setFileInstance={setFileInstance} />
      ) : (
        <EvaluationPage.Root setFileInstance={setFileInstance} />
      )}
    </section>
  );
}
