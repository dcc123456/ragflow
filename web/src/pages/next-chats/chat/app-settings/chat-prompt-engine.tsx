'use client';

import { CrossLanguageFormField } from '@/components/cross-language-form-field';
import { RerankFormFields } from '@/components/rerank';
import { SimilaritySliderFormField } from '@/components/similarity-slider';
import { SwitchFormField } from '@/components/switch-fom-field';
import { TopNFormField } from '@/components/top-n-item';
import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form';
import { Textarea } from '@/components/ui/textarea';
import { UseKnowledgeGraphFormField } from '@/components/use-knowledge-graph-item';
import { useTranslate } from '@/hooks/common-hooks';
import { prefixName } from '@/utils/form';
import { getDirAttribute } from '@/utils/text-direction';
import { useFormContext } from 'react-hook-form';
import { DynamicVariableForm } from './dynamic-variable';

interface ChatPromptEngineProps {
  prefix?: string;
}

export function ChatPromptEngine({ prefix = '' }: ChatPromptEngineProps) {
  const { t } = useTranslate('chat');
  const form = useFormContext();
  const systemPromptValue = form.watch('prompt_config.system');

  return (
    <div className="space-y-8">
      <FormField
        control={form.control}
        name={prefixName(prefix, 'prompt_config.system')}
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('system')}</FormLabel>
            <FormControl>
              <Textarea
                {...field}
                rows={8}
                placeholder={t('systemPlaceholder')}
                className="overflow-y-auto"
                dir={getDirAttribute(systemPromptValue || '')}
              />
            </FormControl>
            <FormMessage />
          </FormItem>
        )}
      />
      <SimilaritySliderFormField
        isTooltipShown
        similarityName={prefixName(prefix, 'similarity_threshold')}
        vectorSimilarityWeightName={prefixName(
          prefix,
          'vector_similarity_weight',
        )}
      ></SimilaritySliderFormField>
      <TopNFormField name={prefixName(prefix, 'top_n')}></TopNFormField>
      <SwitchFormField
        name={prefixName(prefix, 'prompt_config.refine_multiturn')}
        label={t('multiTurn')}
        tooltip={t('multiTurnTip')}
      ></SwitchFormField>
      <UseKnowledgeGraphFormField
        name={prefixName(prefix, 'prompt_config.use_kg')}
      ></UseKnowledgeGraphFormField>
      <SwitchFormField
        name={prefixName(prefix, 'prompt_config.reasoning')}
        label={t('reasoning')}
        tooltip={t('reasoningTip')}
      ></SwitchFormField>
      <RerankFormFields prefix={prefix}></RerankFormFields>
      <CrossLanguageFormField
        name={prefixName(prefix, 'prompt_config.cross_languages')}
      ></CrossLanguageFormField>
      <DynamicVariableForm
        name={prefixName(prefix, 'prompt_config.parameters')}
      ></DynamicVariableForm>
    </div>
  );
}
