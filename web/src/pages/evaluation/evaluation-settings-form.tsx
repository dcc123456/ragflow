'use client';

import { Collapse } from '@/components/collapse';
import { SelectWithSearch } from '@/components/originui/select-with-search';
import { RAGFlowFormItem } from '@/components/ragflow-form';
import { SwitchFormField } from '@/components/switch-fom-field';
import { Textarea } from '@/components/ui/textarea';
import { LlmModelType } from '@/constants/knowledge';
import { useFetchAllEvaluationCollection } from '@/hooks/use-evaluation-request';
import { useSelectLlmOptionsByModelType } from '@/hooks/use-llm-request';
import { useTranslation } from 'react-i18next';

type LLmSelectFormItemProps = {
  name: string;
};

function LLmSelectFormItem({ name }: LLmSelectFormItemProps) {
  const allOptions = useSelectLlmOptionsByModelType();
  const { t } = useTranslation();

  return (
    <RAGFlowFormItem name={name} label={t('chat.model')}>
      <SelectWithSearch
        placeholder={t('evaluation.defaultModel')}
        options={allOptions[LlmModelType.Chat]}
      />
    </RAGFlowFormItem>
  );
}

export function EvaluationSettingsForm() {
  const { t } = useTranslation();

  const {
    data: { collections },
  } = useFetchAllEvaluationCollection();

  const options = collections.map((collection) => ({
    value: collection.id,
    label: collection.name,
  }));

  return (
    <>
      <RAGFlowFormItem
        name="collection_id"
        label={t('evaluation.evaluationData')}
        required
      >
        <SelectWithSearch
          placeholder={t('evaluation.selectCollection')}
          options={options}
        />
      </RAGFlowFormItem>

      <Collapse
        title={t('evaluation.relevancy')}
        rightContent={<SwitchFormField name="relevancy.enabled" label="" />}
      >
        <LLmSelectFormItem name="relevancy.model"></LLmSelectFormItem>
      </Collapse>

      <Collapse
        title={t('evaluation.factuality')}
        rightContent={<SwitchFormField name="factuality.enabled" label="" />}
      >
        <LLmSelectFormItem name="factuality.model"></LLmSelectFormItem>
      </Collapse>

      <Collapse
        title={t('evaluation.consistency')}
        rightContent={<SwitchFormField name="consistency.enabled" label="" />}
      >
        <div className="space-y-4">
          <LLmSelectFormItem name="consistency.model"></LLmSelectFormItem>
          <RAGFlowFormItem name="prompt" label={t('evaluation.prompt')}>
            <Textarea
              placeholder={t('evaluation.promptPlaceholder')}
              rows={4}
            />
          </RAGFlowFormItem>
        </div>
      </Collapse>
    </>
  );
}
