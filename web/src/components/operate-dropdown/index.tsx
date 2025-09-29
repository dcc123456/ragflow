import { useShowDeleteConfirm } from '@/hooks/common-hooks';
import { DeleteOutlined, MoreOutlined } from '@ant-design/icons';
import { Dropdown, MenuProps, Space } from 'antd';
import { useTranslation } from 'react-i18next';

import { IConfirmDeletePermission } from '@/interfaces/request/team';
import React, { useMemo } from 'react';
import { DeletePrivilegeConfirmContent } from '../privilege/delete-privilege-confirm-content';
import styles from './index.less';

interface IProps {
  deleteItem: () => Promise<any> | void;
  iconFontSize?: number;
  iconFontColor?: string;
  items?: MenuProps['items'];
  height?: number;
  needsDeletionValidation?: boolean;
  showDeleteItems?: boolean;
  showDeleteContent?: boolean;
  confirmDeletePermissionParams?: IConfirmDeletePermission;
}

const OperateDropdown = ({
  deleteItem,
  children,
  iconFontSize = 30,
  iconFontColor = 'gray',
  items: otherItems = [],
  height = 24,
  needsDeletionValidation = true,
  showDeleteItems = true,
  showDeleteContent,
  confirmDeletePermissionParams,
}: React.PropsWithChildren<IProps>) => {
  const { t } = useTranslation();
  const showDeleteConfirm = useShowDeleteConfirm();

  const handleDelete = () => {
    if (needsDeletionValidation) {
      showDeleteConfirm({
        onOk: deleteItem,
        content: showDeleteContent ? (
          <DeletePrivilegeConfirmContent
            params={confirmDeletePermissionParams!}
          ></DeletePrivilegeConfirmContent>
        ) : null,
      });
    } else {
      deleteItem();
    }
  };

  const handleDropdownMenuClick: MenuProps['onClick'] = ({ domEvent, key }) => {
    domEvent.preventDefault();
    domEvent.stopPropagation();
    if (key === '1') {
      handleDelete();
    }
  };

  const items: MenuProps['items'] = useMemo(() => {
    const items = [];

    if (showDeleteItems) {
      items.push({
        key: '1',
        label: (
          <Space>
            <DeleteOutlined />
            {t('common.delete')}
          </Space>
        ),
      });
    }

    return [...items, ...otherItems];
  }, [showDeleteItems, otherItems, t]);

  return (
    <Dropdown
      menu={{
        items,
        onClick: handleDropdownMenuClick,
      }}
    >
      {children || (
        <span className={styles.delete}>
          <MoreOutlined
            rotate={90}
            style={{
              fontSize: iconFontSize,
              color: iconFontColor,
              cursor: 'pointer',
              height,
            }}
          />
        </span>
      )}
    </Dropdown>
  );
};

export default OperateDropdown;
