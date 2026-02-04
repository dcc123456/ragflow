'use client';

import { AvatarUpload } from '@/components/avatar-upload';
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
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { useTranslate } from '@/hooks/common-hooks';
import { prefixName } from '@/utils/form';
import { useFormContext } from 'react-hook-form';

interface ChatBasicSettingProps {
  prefix?: string;
}

export default function ChatBasicSetting({
  prefix = '',
}: ChatBasicSettingProps) {
  const { t } = useTranslate('chat');
  const form = useFormContext();

  return (
    <div className="space-y-8">
      <FormField
        control={form.control}
        name={prefixName(prefix, 'icon')}
        render={({ field }) => (
          <div className="space-y-6">
            <FormItem className="w-full">
              <FormLabel>{t('assistantAvatar')}</FormLabel>
              <FormControl>
                <AvatarUpload {...field}></AvatarUpload>
              </FormControl>
              <FormMessage />
            </FormItem>
          </div>
        )}
      />
      <FormField
        control={form.control}
        name={prefixName(prefix, 'name')}
        render={({ field }) => (
          <FormItem>
            <FormLabel required>{t('assistantName')}</FormLabel>
            <FormControl>
              <Input {...field}></Input>
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name={prefixName(prefix, 'description')}
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('description')}</FormLabel>
            <FormControl>
              <Textarea {...field}></Textarea>
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <FormField
        control={form.control}
        name={prefixName(prefix, 'prompt_config.empty_response')}
        render={({ field }) => (
          <FormItem>
            <FormLabel tooltip={t('emptyResponseTip')}>
              {t('emptyResponse')}
            </FormLabel>
            <FormControl>
              <Textarea {...field}></Textarea>
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
              <Textarea {...field}></Textarea>
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <SwitchFormField
        name={prefixName(prefix, 'prompt_config.quote')}
        label={t('quote')}
        tooltip={t('quoteTip')}
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
