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

import type { TFunction } from 'i18next';
import { FormFieldType } from '@/components/dynamic-form';
import {
  DingTalkMode,
  eeDataSourceDefaultValues,
  EeDataSourceFeatureVisibility,
  eeDataSourceKeys,
  generateEeDataSourceFormFields,
  generateEeDataSourceInfo,
  resolveEeDataSourceSource,
  shouldDisableEeDataSourceField,
} from '@/ee/pages/user-setting/data-source/ee-extensions';

const translate = ((key: string) => key) as TFunction;
const feishuWikiKey = 'feishu_wiki';

describe('Feishu Wiki enterprise data source', () => {
  it('contributes one enterprise card', () => {
    const info = generateEeDataSourceInfo(translate);

    expect(eeDataSourceKeys).toContain(feishuWikiKey);
    expect(info[feishuWikiKey]).toMatchObject({
      name: 'Feishu / Lark',
      description: 'setting.feishu_wikiDescription',
    });
  });

  it('prepends the platform switch and keeps the upstream fields', () => {
    const fields = generateEeDataSourceFormFields(translate)[feishuWikiKey];

    expect(fields[0]).toMatchObject({
      name: 'config.domain',
      type: FormFieldType.Segmented,
      required: true,
      options: [
        { label: 'Feishu', value: 'feishu' },
        { label: 'Lark', value: 'lark' },
      ],
    });
    expect(fields.map((field) => field.name)).toEqual(
      expect.arrayContaining([
        'config.credentials.app_id',
        'config.credentials.app_secret',
        'config.space_id',
        'config.root_node_token',
        'config.include_extensions',
        'config.include_keywords',
        'config.exclude_keywords',
        'config.max_file_size_bytes',
        'config.batch_size',
      ]),
    );
  });

  it('defaults to the Feishu platform and an empty connection', () => {
    expect(eeDataSourceDefaultValues[feishuWikiKey]).toEqual({
      name: '',
      source: feishuWikiKey,
      config: {
        domain: 'feishu',
        space_id: '',
        root_node_token: '',
        include_extensions: [],
        include_keywords: [],
        exclude_keywords: [],
        max_file_size_bytes: 50 * 1024 * 1024,
        batch_size: 2,
        credentials: {
          app_id: '',
          app_secret: '',
        },
      },
    });
  });

  it('offers the sync-deleted-files switch', () => {
    expect(EeDataSourceFeatureVisibility[feishuWikiKey]).toEqual({
      syncDeletedFiles: true,
    });
  });
});

describe('DingTalk enterprise data source', () => {
  const dingtalkKey = 'dingtalk_ai_table';

  it('contributes one card with three modes', () => {
    const info = generateEeDataSourceInfo(translate);

    expect(eeDataSourceKeys).toContain(dingtalkKey);
    expect(info[dingtalkKey]).toMatchObject({
      name: 'DingTalk',
      description: 'setting.dingtalkDescription',
    });

    const fields = generateEeDataSourceFormFields(translate)[dingtalkKey];
    expect(fields[0]).toMatchObject({
      name: 'config.mode',
      type: FormFieldType.Segmented,
      required: true,
      options: [
        { label: 'AI Table', value: DingTalkMode.AITable },
        { label: 'Wiki', value: DingTalkMode.Wiki },
        {
          label: 'setting.dataSourceFieldDingTalkDriveMode',
          value: DingTalkMode.Drive,
        },
      ],
    });
  });

  it('switches the field groups with the selected mode', () => {
    const fields = generateEeDataSourceFormFields(translate)[dingtalkKey];
    const visibleNames = (mode: DingTalkMode) =>
      fields
        .filter(
          (field) =>
            !field.shouldRender || field.shouldRender({ config: { mode } }),
        )
        .map((field) => field.name);

    expect(visibleNames(DingTalkMode.AITable)).toEqual(
      expect.arrayContaining([
        'config.credentials.access_token',
        'config.table_id',
      ]),
    );
    expect(visibleNames(DingTalkMode.AITable)).not.toContain(
      'config.credentials.app_key',
    );
    expect(visibleNames(DingTalkMode.Wiki)).toEqual(
      expect.arrayContaining([
        'config.credentials.app_key',
        'config.space_id',
        'config.root_node_id',
        'config.include_extensions',
        'config.max_file_size_bytes',
        'config.batch_size',
      ]),
    );
    expect(visibleNames(DingTalkMode.Drive)).toContain('config.space_id');
  });

  it('defaults to the AI Table mode and its backend source', () => {
    const defaults = eeDataSourceDefaultValues[dingtalkKey];

    expect(defaults).toMatchObject({
      source: dingtalkKey,
      config: {
        mode: DingTalkMode.AITable,
        batch_size: 2,
        credentials: {
          access_token: '',
          app_key: '',
          app_secret: '',
        },
      },
    });
  });

  it('maps each mode to its backend source', () => {
    expect(
      resolveEeDataSourceSource({ config: { mode: DingTalkMode.Wiki } }),
    ).toBe('dingtalk_wiki');
    expect(
      resolveEeDataSourceSource({ config: { mode: DingTalkMode.Drive } }),
    ).toBe('dingtalk_drive');
    expect(
      resolveEeDataSourceSource({ config: { mode: DingTalkMode.AITable } }),
    ).toBe(dingtalkKey);
    expect(
      resolveEeDataSourceSource({ source: 'feishu_wiki', config: {} }),
    ).toBe('feishu_wiki');
  });

  it('keeps every DingTalk backend source editable after creation', () => {
    const info = generateEeDataSourceInfo(translate);
    const fields = generateEeDataSourceFormFields(translate);

    for (const key of [
      'dingtalk_ai_table',
      'dingtalk_wiki',
      'dingtalk_drive',
    ]) {
      expect(info[key]).toMatchObject({ name: 'DingTalk' });
      expect(fields[key]?.length).toBeGreaterThan(0);
      expect(eeDataSourceDefaultValues[key]?.config.mode).toBeTruthy();
      expect(EeDataSourceFeatureVisibility[key]).toEqual({
        syncDeletedFiles: true,
      });
    }
  });

  it('locks the mode field on the detail page and offers deletion sync', () => {
    expect(shouldDisableEeDataSourceField('config.mode')).toBe(true);
    expect(shouldDisableEeDataSourceField('config.space_id')).toBe(false);
    expect(EeDataSourceFeatureVisibility[dingtalkKey]).toEqual({
      syncDeletedFiles: true,
    });
  });
});
