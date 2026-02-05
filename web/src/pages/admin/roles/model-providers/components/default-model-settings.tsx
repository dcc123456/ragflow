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

import { SelectWithSearch } from '@/components/originui/select-with-search';

import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip';

import message from '@/components/ui/message';

import { LlmModelType } from '@/constants/knowledge';
import { useTranslate } from '@/hooks/common-hooks';

import { LlmIcon } from '@/components/svg-icon';
import {
  useDefaultModelOptions,
  useRoleDefaultModels,
} from '@/pages/admin/hooks/useLlm';
import { getLLMIconName, getRealModelName } from '@/utils/llm-util';

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

  const { modelOptions } = useDefaultModelOptions();

  const llmList = useMemo(() => {
    return [
      {
        id: 'llm_id',
        type: 'llm',
        label: t('chatModel'),
        required: true,
        options: modelOptions[LlmModelType.Chat],
        tooltip: t('chatModelTip'),
      },
      {
        id: 'embd_id',
        type: 'embedding',
        label: t('embeddingModel'),
        options: modelOptions[LlmModelType.Embedding],
        tooltip: t('embeddingModelTip'),
      },
      {
        id: 'img2txt_id',
        type: 'vlm',
        label: t('img2txtModel'),
        options: modelOptions[LlmModelType.Image2text],
        tooltip: t('img2txtModelTip'),
      },
      {
        id: 'asr_id',
        type: 'asr',
        label: t('sequence2txtModel'),
        options: modelOptions[LlmModelType.Speech2text],
        tooltip: t('sequence2txtModelTip'),
      },
      {
        id: 'rerank_id',
        type: 'rerank',
        label: t('rerankModel'),
        options: modelOptions[LlmModelType.Rerank],
        tooltip: t('rerankModelTip'),
      },
      {
        id: 'tts_id',
        type: 'tts',
        label: t('ttsModel'),
        options: modelOptions[LlmModelType.TTS],
        tooltip: t('ttsModelTip'),
      },
    ] as const;
  }, [modelOptions, t]);

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
                  <SelectWithSearch
                    triggerClassName="w-full flex items-center h-10"
                    allowClear={item.id !== 'llm_id'}
                    value={field.value}
                    options={item.options}
                    onChange={async (value) => {
                      if (value !== field.value) {
                        await setDefaultModel({
                          model_type: item.type,
                          model_id: value,
                        });
                        field.onChange(value);
                        message.success(tMsg('modified'));
                      }
                    }}
                    placeholder={t('selectModelPlaceholder')}
                    emptyData={t('modelEmptyTip')}
                    // @ts-ignore
                    renderOption={({ fid, llm_name }) => (
                      <div className="flex items-center gap-2">
                        <LlmIcon name={getLLMIconName(fid, llm_name)} />
                        <span>{getRealModelName(llm_name)}</span>
                      </div>
                    )}
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
