import { Modal } from 'antd';
import { useEffect, useState } from 'react';

import pleaseGif from '@/assets/please.gif';
import starImage from '@/assets/star.jpg';
import { Channel } from '@/utils/star-util';
import { CloseOutlined } from '@ant-design/icons';
import styles from './index.module.less';

const StarModal = () => {
  const [isModalOpen, setIsModalOpen] = useState(false);

  const handleOk = () => {
    setIsModalOpen(false);
  };

  const handleCancel = () => {
    setIsModalOpen(false);
  };

  useEffect(() => {
    Channel.getInstance().on('star', (open: boolean) => {
      setIsModalOpen(open);
    });
  }, []);

  return (
    <Modal
      open={isModalOpen}
      onOk={handleOk}
      onCancel={handleCancel}
      width={920}
      footer={null}
      className={styles.starModal}
      maskClosable={false}
      closeIcon={<CloseOutlined className={styles.closeIcon} />}
    >
      <section className={styles.wrapper}>
        <a href="https://github.com/infiniflow/ragflow" target="blank">
          <img src={starImage} alt="" className={styles.star} />
          <img src={pleaseGif} alt="" className={styles.please} />
        </a>
      </section>
    </Modal>
  );
};

export default StarModal;
