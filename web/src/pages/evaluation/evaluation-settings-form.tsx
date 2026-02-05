'use client';

import { Collapse } from '@/components/collapse';
import { SelectWithSearch } from '@/components/originui/select-with-search';
import { RAGFlowFormItem } from '@/components/ragflow-form';
import { SwitchFormField } from '@/components/switch-fom-field';
import { LlmModelType } from '@/constants/knowledge';
import { useFetchAllEvaluationCollection } from '@/hooks/use-evaluation-request';
import { useSelectLlmOptionsByModelType } from '@/hooks/use-llm-request';
import { prefixName } from '@/utils/form';
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

const Prefix = 'config_snapshot.metrics.';

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
        rightContent={
          <SwitchFormField
            name={prefixName(Prefix, 'context_relevance.enable')}
            label=""
            shouldStopPropagation
          />
        }
      >
        <LLmSelectFormItem
          name={prefixName(Prefix, 'context_relevance.llm_id')}
        ></LLmSelectFormItem>
      </Collapse>

      <Collapse
        title={t('evaluation.factuality')}
        rightContent={
          <SwitchFormField
            name={prefixName(Prefix, 'faithfulness.enable')}
            label=""
            shouldStopPropagation
          />
        }
      >
        <LLmSelectFormItem
          name={prefixName(Prefix, 'faithfulness.llm_id')}
        ></LLmSelectFormItem>
      </Collapse>

      <Collapse
        title={t('evaluation.consistency')}
        rightContent={
          <SwitchFormField
            name={prefixName(Prefix, 'semantic_similarity.enable')}
            label=""
            shouldStopPropagation
          />
        }
      >
        <div className="space-y-4">
          <LLmSelectFormItem
            name={prefixName(Prefix, 'semantic_similarity.llm_id')}
          ></LLmSelectFormItem>
        </div>
      </Collapse>
    </>
  );
}
