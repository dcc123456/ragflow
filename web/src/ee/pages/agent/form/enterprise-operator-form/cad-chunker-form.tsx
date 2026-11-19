import {
  ModelTreeSelectFormField,
  ModelTypeMap,
} from '@/components/model-tree-select';
import { useSyncExternalFormErrors } from '@/components/pipeline-operator-tabs/use-sync-external-form-errors';
import { RAGFlowFormItem } from '@/components/ragflow-form';
import { Form } from '@/components/ui/form';
import { Switch } from '@/components/ui/switch';
import { Textarea } from '@/components/ui/textarea';
import { ModelTypeToField } from '@/constants/llm';
import { useFetchDefaultModelDictionary } from '@/hooks/use-llm-request';
import { zodResolver } from '@hookform/resolvers/zod';
import { useCallback, useMemo } from 'react';
import { useForm, useFormContext, useWatch } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { z } from 'zod';
import { FormWrapper } from '@/pages/agent/form/components/form-wrapper';
import { useFormChangeCallback } from '@/pages/agent/hooks/use-form-change-callback';
import { useWatchFormChange } from '@/pages/agent/hooks/use-watch-form-change';
import type { INextOperatorForm } from '@/pages/agent/interface';

const FormSchema = z.object({
  vision: z.object({
    enabled: z.boolean(),
    llm_id: z.string(),
    system_prompt: z.string(),
  }),
});

type CadChunkerFormValues = z.infer<typeof FormSchema>;

const DefaultCadChunkerValues: CadChunkerFormValues = {
  vision: {
    enabled: false,
    llm_id: '',
    system_prompt: '',
  },
};

type CadVisionSwitchProps = {
  enabled: boolean;
  onEnabledChange: (value: boolean) => void;
};

function CadVisionSwitch({ enabled, onEnabledChange }: CadVisionSwitchProps) {
  const form = useFormContext<CadChunkerFormValues>();
  const defaultModelDictionary = useFetchDefaultModelDictionary();

  const handleCheckedChange = useCallback(
    (checked: boolean) => {
      onEnabledChange(checked);
      if (checked && !form.getValues('vision.llm_id')) {
        const modelId = defaultModelDictionary[ModelTypeToField.vision];
        if (modelId) {
          form.setValue('vision.llm_id', modelId, {
            shouldDirty: true,
            shouldValidate: true,
          });
        }
      }
    },
    [defaultModelDictionary, form, onEnabledChange],
  );

  return <Switch checked={enabled} onCheckedChange={handleCheckedChange} />;
}

export default function CadChunkerForm({
  node,
  onValuesChange,
  externalErrors,
}: INextOperatorForm) {
  const { t } = useTranslation();
  const defaultValues = useMemo<CadChunkerFormValues>(() => {
    const saved = node?.data?.form as Partial<CadChunkerFormValues> | undefined;
    return {
      vision: {
        ...DefaultCadChunkerValues.vision,
        ...saved?.vision,
      },
    };
  }, [node?.data?.form]);
  const form = useForm<CadChunkerFormValues>({
    defaultValues,
    resolver: zodResolver(FormSchema),
    mode: 'onChange',
  });
  const enabled = useWatch({ control: form.control, name: 'vision.enabled' });

  useSyncExternalFormErrors(form, externalErrors);
  useWatchFormChange(node?.id, form);
  useFormChangeCallback(form, onValuesChange);

  return (
    <Form {...form}>
      <FormWrapper>
        <RAGFlowFormItem
          name="vision.enabled"
          label={t('flow.enableVisionEnhancement')}
          tooltip={t('flow.cadVisionEnhancementTip')}
          horizontal
          labelClassName="w-full"
          valueClassName="w-8"
        >
          {(field) => (
            <CadVisionSwitch
              enabled={field.value}
              onEnabledChange={field.onChange}
            />
          )}
        </RAGFlowFormItem>
        {enabled && (
          <ModelTreeSelectFormField
            name="vision.llm_id"
            label={t('chat.model')}
            modelTypes={ModelTypeMap.img2txt_id}
            allowClear
          />
        )}
        {enabled && (
          <RAGFlowFormItem
            name="vision.system_prompt"
            label={t('flow.systemPrompt')}
            tooltip={t('flow.visionEnhancementPromptTip')}
          >
            <Textarea placeholder={t('flow.systemPromptPlaceholder')} />
          </RAGFlowFormItem>
        )}
      </FormWrapper>
    </Form>
  );
}
