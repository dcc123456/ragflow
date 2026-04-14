'use client';

import { AvatarNameDescription } from '@/components/avatar-name-description';
import { KnowledgeBaseFormField } from '@/components/knowledge-base-item';
import { MetadataFilter } from '@/components/metadata-filter';
import { SwitchFormField } from '@/components/switch-fom-field';
import { TavilyFormField } from '@/components/tavily-form-field';
import { TOCEnhanceFormField } from '@/components/toc-enhance-form-field';
import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form';
import { Textarea } from '@/components/ui/textarea';
import { useTranslate } from '@/hooks/common-hooks';
import { prefixName } from '@/utils/form';
import { getDirAttribute } from '@/utils/text-direction';
import { useFormContext } from 'react-hook-form';

interface ChatBasicSettingProps {
  prefix?: string;
  option?: Record<string, any>;
  hideName?: boolean;
}

export default function ChatBasicSetting({
  prefix = '',
  option,
  hideName = false,
}: ChatBasicSettingProps) {
  const { t } = useTranslate('chat');
  const form = useFormContext();
  const emptyResponseValue = form.watch('prompt_config.empty_response');
  const prologueValue = form.watch('prompt_config.prologue');

  return (
    <div className="space-y-8">
      <AvatarNameDescription />
      <FormField
        control={form.control}
        name={'prompt_config.empty_response'}
        render={({ field }) => (
          <FormItem>
            <FormLabel tooltip={t('emptyResponseTip')}>
              {t('emptyResponse')}
            </FormLabel>
            <FormControl>
              <Textarea
                {...field}
                placeholder={t('emptyResponsePlaceholder')}
                dir={getDirAttribute(emptyResponseValue || '')}
              ></Textarea>
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name={prefixName(prefix, 'prompt_config.prologue')}
        render={({ field }) => (
          <FormItem>
            <FormLabel tooltip={t('setAnOpenerTip')}>
              {t('setAnOpener')}
            </FormLabel>
            <FormControl>
              <Textarea
                {...field}
                dir={getDirAttribute(prologueValue || '')}
              ></Textarea>
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <SwitchFormField
        name={prefixName(prefix, 'prompt_config.quote')}
        label={t('quote')}
        tooltip={t('quoteTip')}
        disabled={option?.['prompt_config.quote'].disabled || false}
      ></SwitchFormField>
      <SwitchFormField
        name={prefixName(prefix, 'prompt_config.keyword')}
        label={t('keyword')}
        tooltip={t('keywordTip')}
      ></SwitchFormField>
      <SwitchFormField
        name={prefixName(prefix, 'prompt_config.tts')}
        label={t('tts')}
        tooltip={t('ttsTip')}
      ></SwitchFormField>
      <TOCEnhanceFormField
        name={prefixName(prefix, 'prompt_config.toc_enhance')}
      ></TOCEnhanceFormField>
      <TavilyFormField
        name={prefixName(prefix, 'prompt_config.tavily_api_key')}
      ></TavilyFormField>
      <KnowledgeBaseFormField
        name={prefixName(prefix, 'kb_ids')}
      ></KnowledgeBaseFormField>
      <MetadataFilter prefix={prefix}></MetadataFilter>
    </div>
  );
}
