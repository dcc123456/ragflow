import { FormFieldType } from '@/components/dynamic-form';
import SvgIcon from '@/components/svg-icon';
import {
  feishuWikiConstant,
  feishuWikiDefaultValues,
} from '@/ee/pages/user-setting/data-source/feishu-wiki-constant';
import type { TFunction } from 'i18next';
import { BookOpen } from 'lucide-react';
import type { ReactNode } from 'react';

/**
 * Enterprise data-source connectors contributed to the shared data-source
 * page. Open source ships the same module with empty fragments
 * (`web/src/ee-stub/pages/user-setting/data-source/ee-extensions.tsx`).
 *
 * DingTalk is one card with three modes: the AI Table connector
 * (`dingtalk_ai_table`), the enterprise knowledge base connector
 * (`dingtalk_wiki`) and the drive connector (`dingtalk_drive`). The Feishu Wiki
 * card gains a platform switch, because Lark is a separate open platform with
 * its own host and app credentials.
 */

export interface EeDataSourceInfoItem {
  name: string;
  description: string;
  icon: ReactNode;
}

export enum DingTalkMode {
  AITable = 'ai_table',
  Wiki = 'wiki',
  Drive = 'drive',
}

// The keys are literals on purpose: importing the shared enum here would make
// the shared module and this overlay import each other.
const dingtalkAITableKey = 'dingtalk_ai_table';
const dingtalkWikiKey = 'dingtalk_wiki';
const dingtalkDriveKey = 'dingtalk_drive';

const onlyAITable = (values: any) =>
  values?.config?.mode === DingTalkMode.AITable;

const onlyWiki = (values: any) => values?.config?.mode === DingTalkMode.Wiki;

const onlyDrive = (values: any) => values?.config?.mode === DingTalkMode.Drive;

// onlySpaces covers the two modes that sync a DingTalk space: a knowledge base
// and a drive space share the credentials and the filtering fields.
const onlySpaces = (values: any) => onlyWiki(values) || onlyDrive(values);

const dingtalkModeField = (t: TFunction) => ({
  label: t('setting.dataSourceFieldDingTalkType'),
  name: 'config.mode',
  type: FormFieldType.Segmented,
  required: true,
  options: [
    { label: 'AI Table', value: DingTalkMode.AITable },
    { label: 'Wiki', value: DingTalkMode.Wiki },
    {
      label: t('setting.dataSourceFieldDingTalkDriveMode'),
      value: DingTalkMode.Drive,
    },
  ],
});

const dingtalkAITableFields = (t: TFunction) => [
  {
    label: t('setting.dataSourceFieldAccessToken'),
    name: 'config.credentials.access_token',
    type: FormFieldType.Password,
    required: true,
    shouldRender: onlyAITable,
  },
  {
    label: t('setting.dataSourceFieldBaseId'),
    name: 'config.table_id',
    type: FormFieldType.Text,
    required: true,
    shouldRender: onlyAITable,
  },
  {
    label: t('setting.dataSourceFieldOperatorId'),
    name: 'config.operator_id',
    type: FormFieldType.Text,
    required: true,
    shouldRender: onlyAITable,
  },
];

const dingtalkWikiFields = (t: TFunction) => [
  {
    label: t('setting.dataSourceFieldDingTalkAppKey'),
    name: 'config.credentials.app_key',
    type: FormFieldType.Text,
    required: true,
    shouldRender: onlySpaces,
  },
  {
    label: t('setting.dataSourceFieldDingTalkAppSecret'),
    name: 'config.credentials.app_secret',
    type: FormFieldType.Password,
    required: true,
    shouldRender: onlySpaces,
  },
  {
    label: t('setting.dataSourceFieldOperatorId'),
    name: 'config.operator_id',
    type: FormFieldType.Text,
    required: true,
    shouldRender: onlySpaces,
  },
  {
    label: t('setting.dataSourceFieldWikiSpaceId'),
    name: 'config.space_id',
    type: FormFieldType.Text,
    required: true,
    shouldRender: onlyWiki,
  },
  {
    label: t('setting.dataSourceFieldDingTalkDriveSpaceId'),
    name: 'config.space_id',
    type: FormFieldType.Text,
    required: true,
    shouldRender: onlyDrive,
  },
  {
    label: t('setting.dataSourceFieldRootNodeToken'),
    name: 'config.root_node_id',
    type: FormFieldType.Text,
    required: false,
    shouldRender: onlySpaces,
  },
  {
    label: t('setting.dataSourceFieldIncludeExtensions'),
    name: 'config.include_extensions',
    type: FormFieldType.Tag,
    required: false,
    shouldRender: onlySpaces,
  },
  {
    label: t('setting.dataSourceFieldIncludeKeywords'),
    name: 'config.include_keywords',
    type: FormFieldType.Tag,
    required: false,
    shouldRender: onlySpaces,
  },
  {
    label: t('setting.dataSourceFieldExcludeKeywords'),
    name: 'config.exclude_keywords',
    type: FormFieldType.Tag,
    required: false,
    shouldRender: onlySpaces,
  },
  {
    label: t('setting.dataSourceFieldMaxFileSizeBytes'),
    name: 'config.max_file_size_bytes',
    type: FormFieldType.Number,
    required: false,
    shouldRender: onlySpaces,
  },
  {
    label: t('setting.dataSourceFieldBatchSize'),
    name: 'config.batch_size',
    type: FormFieldType.Number,
    required: false,
    shouldRender: onlySpaces,
  },
];

const dingtalkDefaultValues = (source: string, mode: DingTalkMode) => ({
  name: '',
  source,
  config: {
    mode,
    table_id: '',
    operator_id: '',
    space_id: '',
    root_node_id: '',
    include_extensions: [],
    include_keywords: [],
    exclude_keywords: [],
    max_file_size_bytes: 50 * 1024 * 1024,
    batch_size: 2,
    credentials: {
      access_token: '',
      app_key: '',
      app_secret: '',
    },
  },
});

const dingtalkIcon = (
  <SvgIcon name={'data-source/dingtalk-ai-table'} width={38} />
);

const feishuWikiKey = 'feishu_wiki';

const feishuPlatformField = (t: TFunction) => ({
  label: t('setting.dataSourceFieldFeishuPlatform'),
  name: 'config.domain',
  type: FormFieldType.Segmented,
  required: true,
  options: [
    { label: 'Feishu', value: 'feishu' },
    { label: 'Lark', value: 'lark' },
  ],
});

const dingtalkCard = (t: TFunction): EeDataSourceInfoItem => ({
  name: 'DingTalk',
  description: t('setting.dingtalkDescription'),
  icon: dingtalkIcon,
});

const dingtalkFormFields = (t: TFunction) => [
  dingtalkModeField(t),
  ...dingtalkAITableFields(t),
  ...dingtalkWikiFields(t),
];

/** Connector keys that exist only in the enterprise build. */
export const eeDataSourceKeys: string[] = [feishuWikiKey, dingtalkAITableKey];

/** Connector cards shown in the data-source grid. */
export const generateEeDataSourceInfo = (
  t: TFunction,
): Record<string, EeDataSourceInfoItem> => ({
  [feishuWikiKey]: {
    name: 'Feishu / Lark',
    description: t('setting.feishu_wikiDescription'),
    icon: <BookOpen className="text-text-primary" size={22} />,
  },
  [dingtalkAITableKey]: dingtalkCard(t),
  [dingtalkWikiKey]: dingtalkCard(t),
  [dingtalkDriveKey]: dingtalkCard(t),
});

/** Form fields rendered by the add-source dialog. */
export const generateEeDataSourceFormFields = (
  t: TFunction,
): Record<string, any[]> => ({
  [feishuWikiKey]: [feishuPlatformField(t), ...feishuWikiConstant(t)],
  [dingtalkAITableKey]: dingtalkFormFields(t),
  [dingtalkWikiKey]: dingtalkFormFields(t),
  [dingtalkDriveKey]: dingtalkFormFields(t),
});

/** Default connection values used to seed the add-source form. */
export const eeDataSourceDefaultValues: Record<string, Record<string, any>> = {
  [feishuWikiKey]: {
    ...feishuWikiDefaultValues,
    config: { ...feishuWikiDefaultValues.config, domain: 'feishu' },
  },
  [dingtalkAITableKey]: dingtalkDefaultValues(
    dingtalkAITableKey,
    DingTalkMode.AITable,
  ),
  [dingtalkWikiKey]: dingtalkDefaultValues(dingtalkWikiKey, DingTalkMode.Wiki),
  [dingtalkDriveKey]: dingtalkDefaultValues(
    dingtalkDriveKey,
    DingTalkMode.Drive,
  ),
};

/** Feature switches contributed by enterprise connectors. */
export const EeDataSourceFeatureVisibility: Record<string, any> = {
  [feishuWikiKey]: { syncDeletedFiles: true },
  [dingtalkAITableKey]: { syncDeletedFiles: true },
  [dingtalkWikiKey]: { syncDeletedFiles: true },
  [dingtalkDriveKey]: { syncDeletedFiles: true },
};

/** Fields the detail page must lock once a connector has been created. */
export const shouldDisableEeDataSourceField = (fieldName: string): boolean =>
  fieldName === 'config.mode';

// resolveEeDataSourceSource maps the selected DingTalk mode to the backend
// source, because each mode is a separate connector behind one form.
export const resolveEeDataSourceSource = (values: any): string | undefined => {
  const mode = values?.config?.mode;
  if (mode === DingTalkMode.Wiki) {
    return dingtalkWikiKey;
  }
  if (mode === DingTalkMode.Drive) {
    return dingtalkDriveKey;
  }
  if (mode === DingTalkMode.AITable) {
    return dingtalkAITableKey;
  }
  return values?.source;
};
