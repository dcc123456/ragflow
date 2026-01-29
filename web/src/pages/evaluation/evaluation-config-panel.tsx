import { SelectWithSearch } from '@/components/originui/select-with-search';
import { RAGFlowFormItem } from '@/components/ragflow-form';
import { Button } from '@/components/ui/button';
import { Form } from '@/components/ui/form';
import { Separator } from '@/components/ui/separator';
import { useSetModalState } from '@/hooks/common-hooks';
import { useFetchVersionList } from '@/hooks/use-agent-request';
import {
  useFetchAllEvaluationCollection,
  useFetchEvaluationRun,
  useStartEvaluationRun,
} from '@/hooks/use-evaluation-request';
import { zodResolver } from '@hookform/resolvers/zod';
import { isEmpty } from 'lodash';
import { PanelRightClose, Settings } from 'lucide-react';
import { useCallback, useEffect, useMemo } from 'react';
import { useForm, useFormContext, useWatch } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import ChatBasicSetting from '../next-chats/chat/app-settings/chat-basic-settings';
import { ChatModelSettings } from '../next-chats/chat/app-settings/chat-model-settings';
import { ChatPromptEngine } from '../next-chats/chat/app-settings/chat-prompt-engine';
import { EvaluationType } from './constants';
import {
  EvaluationSettingsFormSchema,
  EvaluationSettingsFormType,
} from './evaluation-schemas';
import { EvaluationSettingsDialog } from './evaluation-settings-dialog';
import { EvaluationSettingsForm } from './evaluation-settings-form';

type EvaluationConfigPanelProps = {
  type: EvaluationType;
  visible: boolean;
  onClose: () => void;
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

function ChatEvaluationConfig({ collectionName }: { collectionName?: string }) {
  const { t } = useTranslation();
  const collectionId = useWatch({ name: 'collection_id' });

  const form = useFormContext();

  const {
    data: { config_snapshot },
  } = useFetchEvaluationRun();

  const {
    visible: showDialog,
    showModal: showSettingsDialog,
    hideModal: hideSettingsDialog,
  } = useSetModalState();

  useEffect(() => {
    if (!isEmpty(config_snapshot)) {
      form.reset(config_snapshot);
    }
  }, [config_snapshot, form]);

  return (
    <>
      <section className="bg-bg-input border-border-default border-0.5 flex items-center justify-between rounded-md p-2">
        <div className="space-x-2">
          <span>{t('evaluation.title')}</span>
          <span className="text-text-secondary">
            {collectionName || collectionId || t('evaluation.notConfigured')}
          </span>
        </div>
        <Settings
          className="size-4 text-xs cursor-pointer"
          onClick={showSettingsDialog}
        />
      </section>
      {showDialog && (
        <EvaluationSettingsDialog hideModal={hideSettingsDialog} />
      )}
      <ChatBasicSetting></ChatBasicSetting>
      <Separator />
      <ChatPromptEngine></ChatPromptEngine>
      <Separator />
      <ChatModelSettings></ChatModelSettings>
    </>
  );
}

export function EvaluationConfigPanel({
  type,
  visible,
  onClose,
}: EvaluationConfigPanelProps) {
  const { t } = useTranslation();

  const { startEvaluationRun } = useStartEvaluationRun();

  const {
    data: { collections },
  } = useFetchAllEvaluationCollection();

  const form = useForm<EvaluationSettingsFormType>({
    resolver: zodResolver(EvaluationSettingsFormSchema),
    defaultValues: {
      collection_id: '',
      relevancy: { enabled: true, model: undefined },
      factuality: { enabled: true, model: undefined },
      consistency: { enabled: true, model: undefined },
      prompt: '',
    },
  });

  const currentCollectionId = form.watch('collection_id');
  const currentCollectionName = useMemo(() => {
    if (!currentCollectionId) return undefined;
    return collections.find((c) => c.id === currentCollectionId)?.name;
  }, [currentCollectionId, collections]);

  const handleSubmit = useCallback(
    async (data: EvaluationSettingsFormType) => {
      startEvaluationRun({ collection_id: data.collection_id });
    },
    [startEvaluationRun],
  );

  if (!visible) return null;

  return (
    <section className="w-80 flex flex-col">
      <div className="flex justify-between items-center pb-5 font-semibold pr-5">
        <span>
          {type === 'agent' ? 'Agent Configuration' : 'Chat Configuration'}
        </span>
        <PanelRightClose className="size-4 cursor-pointer" onClick={onClose} />
      </div>
      <Form {...form}>
        <form
          onSubmit={form.handleSubmit(handleSubmit)}
          className="space-y-6 flex-1 min-h-0 flex flex-col"
        >
          <div className="space-y-6 overflow-auto flex-1 pr-5">
            {type === EvaluationType.Agent ? (
              <AgentEvaluationConfig />
            ) : (
              <ChatEvaluationConfig collectionName={currentCollectionName} />
            )}
          </div>

          <div className="text-right pr-5">
            <Button type="submit">{t('common.save')}</Button>
          </div>
        </form>
      </Form>
    </section>
  );
}
