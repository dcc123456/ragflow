import { CardContainer } from '@/components/card-container';
import DuplicateDialog from '@/components/duplicate-dialog';
import { EmptyCardType } from '@/components/empty/constant';
import { EmptyAppCard } from '@/components/empty/empty';
import ListFilterBar from '@/components/list-filter-bar';
import { PrivilegeManagementDialog } from '@/components/privilege-management/privilege-management-dialog';
import { RenameDialog } from '@/components/rename-dialog';
import { Button } from '@/components/ui/button';
import { RAGFlowPagination } from '@/components/ui/ragflow-pagination';
import { useFetchNextKnowledgeListByPage } from '@/hooks/use-knowledge-request';
import { useQueryClient } from '@tanstack/react-query';
import { pick } from 'lodash';
import { Plus } from 'lucide-react';
import { useCallback, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router';
import { DatasetCard } from './dataset-card';
import { DatasetCreatingDialog } from './dataset-creating-dialog';
import { useSaveKnowledge } from './hooks';
import useDuplicateDataset from './use-duplicate-dataset';
import { useRenameDataset } from './use-rename-dataset';
import { useSelectOwners } from './use-select-owners';
import { useShowPrivilegeDialog } from './use-show-privilege-dialog';

export default function Datasets() {
  const { t } = useTranslation();
  const {
    visible,
    hideModal,
    showModal,
    onCreateOk,
    loading: creatingLoading,
  } = useSaveKnowledge();

  const {
    kbs,
    total,
    pagination,
    setPagination,
    handleInputChange,
    searchString,
    filterValue,
    handleFilterSubmit,
  } = useFetchNextKnowledgeListByPage();

  const owners = useSelectOwners();

  const {
    datasetRenameLoading,
    initialDatasetName,
    onDatasetRenameOk,
    datasetRenameVisible,
    hideDatasetRenameModal,
    showDatasetRenameModal,
  } = useRenameDataset();

  const {
    isModalVisible: datasetDuplicateModalVisible,
    showModal: showDatasetDuplicateModal,
    hideModal: hideDatasetDuplicateModal,
    initialName: initialDatasetDuplicateName,
    onOk: onDatasetDuplicateOk,
    loading: datasetDuplicateLoading,
  } = useDuplicateDataset();

  const handlePageChange = useCallback(
    (page: number, pageSize?: number) => {
      setPagination({ page, pageSize });
    },
    [setPagination],
  );
  const [searchUrl, setSearchUrl] = useSearchParams();
  const isCreate = searchUrl.get('isCreate') === 'true';
  const queryClient = useQueryClient();
  useEffect(() => {
    if (isCreate) {
      queryClient.invalidateQueries({ queryKey: ['tenantInfo'] });
      showModal();
      searchUrl.delete('isCreate');
      setSearchUrl(searchUrl);
    }
  }, [isCreate, showModal, searchUrl, setSearchUrl, queryClient]);

  const {
    privilegeModal,
    hidePrivilegeModal,
    handShowPrivilegeModal,
    recordWithSourceType,
  } = useShowPrivilegeDialog();

  return (
    <>
      <section className="py-4 flex-1 flex flex-col">
        {kbs?.length || searchString ? (
          <article
            className="size-full flex flex-col"
            data-testid="datasets-list"
          >
            <header className="px-5 pt-8 mb-4">
              <ListFilterBar
                title={t('header.dataset')}
                searchString={searchString}
                onSearchChange={handleInputChange}
                value={filterValue}
                filters={owners}
                onChange={handleFilterSubmit}
                icon={'datasets'}
              >
                <Button onClick={showModal}>
                  <Plus className="size-[1em]" />
                  {t('knowledgeList.createKnowledgeBase')}
                </Button>
              </ListFilterBar>
              {(!kbs?.length || kbs?.length <= 0) && searchString && (
                <div className="flex w-full items-center justify-center h-[calc(100vh-220px)]">
                  <EmptyAppCard
                    showIcon
                    size="large"
                    className="w-[480px] p-14"
                    isSearch={!!searchString}
                    type={EmptyCardType.Dataset}
                    onClick={() => showModal()}
                  />
                </div>
              )}
              <div className="flex-1 ">
                <CardContainer className="h-[calc(100vh-220px)] overflow-auto px-8">
                  {kbs.map((dataset) => {
                    return (
                      <DatasetCard
                        dataset={dataset}
                        key={dataset.id}
                        showDatasetRenameModal={showDatasetRenameModal}
                        showDatasetDuplicateModal={showDatasetDuplicateModal}
                        showPrivilegeModal={handShowPrivilegeModal(dataset)}
                      ></DatasetCard>
                    );
                  })}
                </CardContainer>

                <footer className="mt-4 px-5 pb-5">
                  <RAGFlowPagination
                    {...pick(pagination, 'current', 'pageSize')}
                    total={total}
                    onChange={handlePageChange}
                  />
                </footer>
              </div>
            </header>
          </article>
        ) : (
          <>
            <div className="flex w-full items-center justify-center h-[calc(100vh-164px)]">
              <EmptyAppCard
                showIcon
                size="large"
                className="w-[480px] p-14"
                isSearch={!!searchString}
                type={EmptyCardType.Dataset}
                onClick={showModal}
              />
            </div>
          </>
        )}
        {visible && (
          <DatasetCreatingDialog
            hideModal={hideModal}
            onOk={onCreateOk}
            loading={creatingLoading}
          ></DatasetCreatingDialog>
        )}
        {datasetDuplicateModalVisible && (
          <DuplicateDialog
            hideModal={hideDatasetDuplicateModal}
            onOk={onDatasetDuplicateOk}
            initialName={initialDatasetDuplicateName}
            loading={datasetDuplicateLoading}
          />
        )}
        {datasetRenameVisible && (
          <RenameDialog
            hideModal={hideDatasetRenameModal}
            onOk={onDatasetRenameOk}
            initialName={initialDatasetName}
            loading={datasetRenameLoading}
          ></RenameDialog>
        )}
        {privilegeModal && (
          <PrivilegeManagementDialog
            hideModal={hidePrivilegeModal}
            initialValues={recordWithSourceType}
          ></PrivilegeManagementDialog>
        )}
      </section>
    </>
  );
}
