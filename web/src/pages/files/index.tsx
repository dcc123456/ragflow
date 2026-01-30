import ListFilterBar from '@/components/list-filter-bar';
import { Segmented } from '@/components/ui/segmented';
import { ChangeEventHandler, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router';
import { EvaluationPage } from './evaluation';
import { FileBreadcrumb } from './file-breadcrumb';
import { FilesManager } from './file-manager';
import { useSelectBreadcrumbItems } from './use-navigate-to-folder';
export enum FileTabs {
  FILE = 'file',
  EVALUATION = 'evaluation',
}

export type FilesInstanceType = {
  searchString?: string;
  onSearchChange?: ChangeEventHandler<HTMLInputElement>;
  showFileUploadModal?: () => void;
  showSyncFileModal?: () => void;
  showFolderCreateModal?: () => void;
};
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
    <section className="p-8">
      <ListFilterBar
        leftPanel={leftPanel}
        searchString={fileInstance.searchString}
        onSearchChange={fileInstance.onSearchChange}
        showFilter={false}
      >
        {activeTab === FileTabs.FILE ? (
          <FilesManager.fileUpload
            showFileUploadModal={fileInstance.showFileUploadModal || (() => {})}
            showSyncFileModal={fileInstance.showSyncFileModal || (() => {})}
            showFolderCreateModal={
              fileInstance.showFolderCreateModal || (() => {})
            }
          />
        ) : (
          <EvaluationPage.fileUpload
            showFileUploadModal={fileInstance.showFileUploadModal || (() => {})}
          />
        )}
      </ListFilterBar>

      {/* Content */}
      {activeTab === FileTabs.FILE ? (
        <FilesManager.root setFileInstance={setFileInstance} />
      ) : (
        <EvaluationPage.root setFileInstance={setFileInstance} />
      )}
    </section>
  );
}
