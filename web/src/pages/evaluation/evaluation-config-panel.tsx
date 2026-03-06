import { SelectWithSearch } from '@/components/originui/select-with-search';
import { RAGFlowFormItem } from '@/components/ragflow-form';
import { ButtonLoading } from '@/components/ui/button';
import { Form } from '@/components/ui/form';
import { Separator } from '@/components/ui/separator';
import { useSetModalState } from '@/hooks/common-hooks';
import { useFetchVersionList } from '@/hooks/use-agent-request';
import { useFetchAllEvaluationCollection } from '@/hooks/use-evaluation-request';
import { PanelRightClose, Settings } from 'lucide-react';
import { useCallback } from 'react';
import { UseFormReturn, useWatch } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import ChatBasicSetting from '../next-chats/chat/app-settings/chat-basic-settings';
import { ChatModelSettings } from '../next-chats/chat/app-settings/chat-model-settings';
import { ChatPromptEngine } from '../next-chats/chat/app-settings/chat-prompt-engine';
import { EvaluationType } from './constants';
import { EvaluationSettingsFormType } from './evaluation-schemas';
import { EvaluationSettingsDialog } from './evaluation-settings-dialog';
import { EvaluationSettingsForm } from './evaluation-settings-form';
import { useInitializeSettingsOnMount } from './use-initialize-settings';
import { useSubmitSettings } from './use-submit-settings';

type EvaluationConfigPanelProps = {
  type: EvaluationType;
  visible: boolean;
  onClose: () => void;
  form: UseFormReturn<EvaluationSettingsFormType>;
};

function AgentEvaluationConfig() {
  const { t } = useTranslation();
  const { data } = useFetchVersionList();
  const agentVersions = data.map((item) => ({
    label: item.title,
    value: item.id,
  }));

  return (
    <>
      <EvaluationSettingsForm />
      <RAGFlowFormItem name="agent" label={t('evaluation.agent')} required>
        <SelectWithSearch options={agentVersions} />
      </RAGFlowFormItem>
    </>
  );
}

const ChatPrefix = 'config_snapshot.target.';

function useFindCollectionName() {
  const {
    data: { collections },
  } = useFetchAllEvaluationCollection();

  const findCollectionName = useCallback(
    (collectionId: string) => {
      return collections.find((collection) => collection.id === collectionId)
        ?.name;
    },
    [collections],
  );

  return findCollectionName;
}

function ChatEvaluationConfig({
  form,
}: {
  form: UseFormReturn<EvaluationSettingsFormType>;
}) {
  const { t } = useTranslation();

  const collectionId = useWatch({ name: 'collection_id' });
  const formState = form.formState;
  const collectionIdError = formState.errors.collection_id;

  const {
    visible: showDialog,
    showModal: showSettingsDialog,
    hideModal: hideSettingsDialog,
  } = useSetModalState();

  const findCollectionName = useFindCollectionName();

  return (
    <>
      <section>
        <div className="bg-bg-input border-border-default border-0.5 flex items-center justify-between rounded-md p-1">
          <div className="space-x-2">
            <span>{t('evaluation.title')}</span>
            <span className="text-text-secondary">
              {findCollectionName(collectionId) ||
                t('evaluation.notConfigured')}
            </span>
          </div>
          <Settings
            className="size-4 text-xs cursor-pointer"
            onClick={showSettingsDialog}
          />
        </div>
        {formState.isSubmitted && collectionIdError && (
          <div className="text-sm font-medium text-state-error">
            {t('evaluation.selectCollection')}
          </div>
        )}
      </section>
      {showDialog && (
        <EvaluationSettingsDialog hideModal={hideSettingsDialog} />
      )}
      <ChatBasicSetting
        prefix={ChatPrefix}
        option={{ 'prompt_config.quote': { disabled: true } }}
        hideName
      ></ChatBasicSetting>
      <Separator />
      <ChatPromptEngine prefix={ChatPrefix}></ChatPromptEngine>
      <Separator />
      <ChatModelSettings
        prefix={ChatPrefix + 'llm_setting'}
        llmId={ChatPrefix + 'llm_id'}
      ></ChatModelSettings>
    </>
  );
}

export function EvaluationConfigPanel({
  type,
  visible,
  onClose,
  form,
}: EvaluationConfigPanelProps) {
  const { t } = useTranslation();

  const { handleSubmit, loading } = useSubmitSettings();

  useInitializeSettingsOnMount(form);

  if (!visible) return null;

  return (
    <section className="w-80 flex flex-col">
      <div className="flex justify-between items-center pb-5 font-semibold pr-5">
        <span>{t('evaluation.configuration')}</span>
        <PanelRightClose className="size-4 cursor-pointer" onClick={onClose} />
      </div>
      <Form {...form}>
        <form
          onSubmit={form.handleSubmit(handleSubmit, (errors) => {
            console.log('🚀 ~ EvaluationConfigPanel ~ errors:', errors);
          })}
          className="space-y-6 flex-1 min-h-0 flex flex-col"
        >
          <div className="space-y-6 overflow-auto flex-1 pr-5">
            {type === EvaluationType.Agent ? (
              <AgentEvaluationConfig />
            ) : (
              <ChatEvaluationConfig form={form} />
            )}
          </div>

          <div className="text-right pr-5">
            <ButtonLoading type="submit" loading={loading}>
              {t('common.save')}
            </ButtonLoading>
          </div>
        </form>
      </Form>
    </section>
  );
}
