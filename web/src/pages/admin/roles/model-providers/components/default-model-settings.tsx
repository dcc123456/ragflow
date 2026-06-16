import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';

import { useMemo } from 'react';
import { useForm } from 'react-hook-form';
import { useParams } from 'react-router';

import { LucideCircleQuestionMark } from 'lucide-react';

import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
} from '@/components/ui/form';

import { ModelTreeSelect, ModelTypeMap } from '@/components/model-tree-select';

import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip';

import message from '@/components/ui/message';

import { useTranslate } from '@/hooks/common-hooks';

import { useRoleDefaultModels } from '@/pages/admin/hooks/useLlm';
import { getCachedLlmList } from '@/utils/llm-cache';
import { getTenantModelId, parseModelValue } from '@/utils/llm-util';

const schema = z.object({
  llm_id: z.string().nonempty(),
  embd_id: z.string().optional(),
  img2txt_id: z.string().optional(),
  asr_id: z.string().optional(),
  rerank_id: z.string().optional(),
  tts_id: z.string().optional(),
});

type SchemaType = z.infer<typeof schema>;

export default function DefaultModelsSettings() {
  const { roleName } = useParams();
  const { t } = useTranslate('setting');
  const { t: tMsg } = useTranslate('message');

  const { defaultModels, setDefaultModel } = useRoleDefaultModels(
    roleName as string,
  );

  const form = useForm<SchemaType>({
    resolver: zodResolver(schema),
    values: {
      llm_id: defaultModels.llm?.model_id ?? '',
      embd_id: defaultModels.embedding?.model_id ?? '',
      img2txt_id: defaultModels.vlm?.model_id ?? '',
      asr_id: defaultModels.asr?.model_id ?? '',
      rerank_id: defaultModels.rerank?.model_id ?? '',
      tts_id: defaultModels.tts?.model_id ?? '',
    },
  });

  const llmList = useMemo(() => {
    return [
      {
        id: 'llm_id',
        type: 'llm',
        label: t('chatModel'),
        required: true,
        tooltip: t('chatModelTip'),
      },
      {
        id: 'embd_id',
        type: 'embedding',
        label: t('embeddingModel'),
        tooltip: t('embeddingModelTip'),
      },
      {
        id: 'img2txt_id',
        type: 'vlm',
        label: t('img2txtModel'),
        tooltip: t('img2txtModelTip'),
      },
      {
        id: 'asr_id',
        type: 'asr',
        label: t('sequence2txtModel'),
        tooltip: t('sequence2txtModelTip'),
      },
      {
        id: 'rerank_id',
        type: 'rerank',
        label: t('rerankModel'),
        tooltip: t('rerankModelTip'),
      },
      {
        id: 'tts_id',
        type: 'tts',
        label: t('ttsModel'),
        tooltip: t('ttsModelTip'),
      },
    ] as const;
  }, [t]);

  return (
    <div className="grid grid-cols-[minmax(max-content,1fr)_3fr] items-center gap-6">
      <Form {...form}>
        {llmList.map((item) => (
          <FormField
            key={item.id}
            control={form.control}
            name={item.id as keyof SchemaType}
            render={({ field }) => (
              <FormItem className="contents">
                <FormLabel
                  // @ts-ignore
                  required={item.required}
                  className="flex items-center text-sm font-normal"
                >
                  {item.label}
                  {item.tooltip && (
                    <Tooltip>
                      <TooltipTrigger>
                        <LucideCircleQuestionMark className="size-[1em] ml-1" />
                      </TooltipTrigger>
                      <TooltipContent>{item.tooltip}</TooltipContent>
                    </Tooltip>
                  )}
                </FormLabel>

                <FormControl>
                  <ModelTreeSelect
                    modelTypes={
                      ModelTypeMap[item.id as keyof typeof ModelTypeMap] ?? [
                        'chat',
                      ]
                    }
                    value={field.value}
                    onChange={async (value) => {
                      if (value === field.value) return;
                      const parsed = parseModelValue(value);
                      const model_id = parsed
                        ? getTenantModelId(
                            getCachedLlmList() ?? {},
                            parsed.model_name,
                            parsed.model_provider,
                          ) || value
                        : '';
                      await setDefaultModel({
                        model_type: item.type,
                        model_id,
                      });
                      field.onChange(value);
                      message.success(tMsg('modified'));
                    }}
                    placeholder={t('selectModelPlaceholder')}
                    allowClear={item.id !== 'llm_id'}
                  />
                </FormControl>
              </FormItem>
            )}
          />
        ))}
      </Form>
    </div>
  );
}
