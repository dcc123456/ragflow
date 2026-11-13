import { FormFieldType } from '@/components/dynamic-form';
import larkIcon from '@/ee/assets/svg/data-source/lark.svg';
import type { TFunction } from 'i18next';
import type { ReactNode } from 'react';

/**
 * Enterprise data-source connectors contributed to the shared data-source
 * page. Open source ships the same module with empty fragments
 * (`web/src/ee/pages/user-setting/data-source/ee-extensions.tsx`).
 */

export interface EeDataSourceInfoItem {
  name: string;
  description: string;
  icon: ReactNode;
}

/** Connector keys that exist only in the enterprise build. */
export const eeDataSourceKeys: string[] = ['lark'];

/** Connector cards shown in the data-source grid. */
export const generateEeDataSourceInfo = (
  t: TFunction,
): Record<string, EeDataSourceInfoItem> => ({
  lark: {
    name: 'Lark',
    description: t('setting.larkDescription'),
    icon: <img src={larkIcon} alt="" width={38} height={38} />,
  },
});

/** Form fields rendered by the add-source dialog. */
export const eeDataSourceFormFields: Record<string, any[]> = {
  lark: [
    {
      label: 'App id',
      name: 'config.credentials.app_id',
      type: FormFieldType.Text,
      required: true,
    },
    {
      label: 'App secret',
      name: 'config.credentials.app_secret',
      type: FormFieldType.Password,
      required: true,
    },
    {
      label: 'Token Type',
      name: 'config.token_type',
      type: FormFieldType.Segmented,
      required: false,
      options: [
        { label: 'Folder', value: 'folder' },
        { label: 'Wiki', value: 'wiki' },
      ],
    },
    {
      label: 'Token',
      name: 'config.token',
      type: FormFieldType.Text,
      required: false,
    },
  ],
};

/** Default connection values used to seed the add-source form. */
export const eeDataSourceDefaultValues: Record<string, Record<string, any>> = {
  lark: {
    name: '',
    source: 'lark',
    config: {
      credentials: {
        app_id: '',
        app_secret: '',
      },
      token_type: 'folder',
      token: '',
    },
  },
};
