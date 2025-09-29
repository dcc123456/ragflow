import { useInfiniteFetchKnowledgeList } from '@/hooks/knowledge-hooks';
import { useFetchUserInfo } from '@/hooks/user-setting-hooks';
import { PlusOutlined, SearchOutlined } from '@ant-design/icons';
import {
  Button,
  Divider,
  Empty,
  Flex,
  Input,
  Skeleton,
  Space,
  Spin,
} from 'antd';
import { useTranslation } from 'react-i18next';
import InfiniteScroll from 'react-infinite-scroll-component';
import { useSaveKnowledge } from './hooks';
import KnowledgeCard from './knowledge-card';
import KnowledgeCreatingModal from './knowledge-creating-modal';

import { PrivilegeManagementDialog } from '@/components/privilege-management/privilege-management-dialog';
import { useSetModalState } from '@/hooks/common-hooks';
import { useKnowledgeWithSourceType } from '@/hooks/logic-hooks/use-knowledge';
import { IKnowledge } from '@/interfaces/database/knowledge';
import { useCallback, useMemo, useState } from 'react';
import styles from './index.less';

const KnowledgeList = () => {
  const { data: userInfo } = useFetchUserInfo();
  const { t } = useTranslation('translation', { keyPrefix: 'knowledgeList' });
  const {
    visible,
    hideModal,
    showModal,
    onCreateOk,
    loading: creatingLoading,
  } = useSaveKnowledge();
  const {
    fetchNextPage,
    data,
    hasNextPage,
    searchString,
    handleInputChange,
    loading,
  } = useInfiniteFetchKnowledgeList();
  const {
    visible: privilegeModal,
    hideModal: hidePrivilegeModal,
    showModal: showPrivilegeModal,
  } = useSetModalState();

  const [record, setRecord] = useState<IKnowledge>({} as IKnowledge);
  const handShowPrivilegeModal = useCallback(
    (item: IKnowledge) => () => {
      setRecord(item);
      showPrivilegeModal();
    },
    [showPrivilegeModal],
  );

  const recordWithSourceType = useKnowledgeWithSourceType(record);

  const nextList = useMemo(() => {
    const list =
      data?.pages?.flatMap((x) => (Array.isArray(x.kbs) ? x.kbs : [])) ?? [];
    return list;
  }, [data?.pages]);

  const total = useMemo(() => {
    return data?.pages.at(-1).total ?? 0;
  }, [data?.pages]);

  return (
    <Flex className={styles.knowledge} vertical flex={1} id="scrollableDiv">
      <div className={styles.topWrapper}>
        <div>
          <span className={styles.title}>
            {t('welcome')}, {userInfo.nickname}
          </span>
          <p className={styles.description}>{t('description')}</p>
        </div>
        <Space size={'large'}>
          <Input
            placeholder={t('searchKnowledgePlaceholder')}
            value={searchString}
            style={{ width: 220 }}
            allowClear
            onChange={handleInputChange}
            prefix={<SearchOutlined />}
          />

          <Button
            type="primary"
            icon={<PlusOutlined />}
            onClick={showModal}
            className={styles.topButton}
          >
            {t('createKnowledgeBase')}
          </Button>
        </Space>
      </div>
      <Spin spinning={loading}>
        <InfiniteScroll
          dataLength={nextList?.length ?? 0}
          next={fetchNextPage}
          hasMore={hasNextPage}
          loader={<Skeleton avatar paragraph={{ rows: 1 }} active />}
          endMessage={!!total && <Divider plain>{t('noMoreData')} 🤐</Divider>}
          scrollableTarget="scrollableDiv"
        >
          <Flex
            gap={'large'}
            wrap="wrap"
            className={styles.knowledgeCardContainer}
          >
            {nextList?.length > 0 ? (
              nextList.map((item: any, index: number) => {
                return (
                  <KnowledgeCard
                    item={item}
                    key={`${item?.name}-${index}`}
                    showPrivilegeModal={handShowPrivilegeModal(item)}
                  ></KnowledgeCard>
                );
              })
            ) : (
              <Empty className={styles.knowledgeEmpty}></Empty>
            )}
          </Flex>
        </InfiniteScroll>
      </Spin>
      <KnowledgeCreatingModal
        loading={creatingLoading}
        visible={visible}
        hideModal={hideModal}
        onOk={onCreateOk}
      ></KnowledgeCreatingModal>
      {privilegeModal && (
        <PrivilegeManagementDialog
          hideModal={hidePrivilegeModal}
          initialValues={recordWithSourceType}
        ></PrivilegeManagementDialog>
      )}
    </Flex>
  );
};

export default KnowledgeList;
