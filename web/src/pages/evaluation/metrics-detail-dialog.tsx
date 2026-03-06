import MessageItem from '@/components/next-message-item';
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { FormTooltip } from '@/components/ui/tooltip';
import { MessageType } from '@/constants/chat';
import { IModalProps } from '@/interfaces/common';
import { IEvaluationRunResult } from '@/interfaces/database/evaluation';
import { camelCase, get } from 'lodash';
import { ChevronsDown, ChevronsUp } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { RunType } from './constants';

interface MetricsDetailDialogProps extends IModalProps<any> {
  resultData?: IEvaluationRunResult;
}

const QuestionKeys = ['question', 'reference_answer'];

function CollapsibleMetric({
  field,
  resultData,
}: {
  field: string;
  resultData: IEvaluationRunResult;
}) {
  const { t } = useTranslation();
  const [isOpen, setIsOpen] = useState(false);

  return (
    <Collapsible key={field} open={isOpen} onOpenChange={setIsOpen}>
      <section>
        <CollapsibleTrigger asChild>
          <div className="flex justify-between items-center">
            <div>
              <span className="text-text-secondary">
                {t(`evaluation.${camelCase(field)}`)}
              </span>
              <FormTooltip
                tooltip={t(`evaluation.${camelCase(field)}Tip`)}
              ></FormTooltip>
            </div>
            <div className="flex items-end gap-2">
              <span className="text-accent-primary text-xs">
                {get(resultData, ['metrics', field])}
              </span>
              {isOpen ? (
                <ChevronsUp className="size-4 cursor-pointer" />
              ) : (
                <ChevronsDown className="size-4 cursor-pointer" />
              )}
            </div>
          </div>
        </CollapsibleTrigger>
        <CollapsibleContent className="text-xs pb-1">
          {get(resultData, ['metrics', `${field}_reason`])}
        </CollapsibleContent>
      </section>
    </Collapsible>
  );
}

export function MetricsDetailDialog({
  hideModal,
  resultData,
}: MetricsDetailDialogProps) {
  const { t } = useTranslation();

  if (!resultData) return null;

  return (
    <Dialog open onOpenChange={hideModal}>
      <DialogContent className="max-w-5xl ">
        <DialogHeader>
          <DialogTitle>{t('evaluation.metricsDetail')}</DialogTitle>
        </DialogHeader>

        <div className="flex max-h-[70vh] overflow-y-auto">
          <section className="bg-bg-card flex-1 min-w-0 p-5 space-y-4 rounded-lg">
            {QuestionKeys.map((x) => (
              <div key={x}>
                <div className="font-medium pb-2 text-text-secondary">
                  {t(`evaluation.${camelCase(x)}`)}
                </div>
                <p className="text-sm text-text-primary">
                  {get(resultData, ['variable', x])}
                </p>
              </div>
            ))}

            {/* Model Answer */}
            <div>
              <div className="font-medium pb-2 text-text-secondary">
                {t('evaluation.modelAnswer')}
              </div>
              <MessageItem
                item={{
                  content: resultData.generated_answer,
                  role: MessageType.Assistant,
                  id: 'xxx',
                }}
                reference={resultData.retrieved_chunks}
                hideTitle
                visibleAvatar={false}
                className="!text-sm !py-0 !pl-8"
              ></MessageItem>
            </div>
          </section>

          <div className="ml-5 w-[140px]">
            <h3 className="font-medium mb-4">{t('evaluation.metrics')}</h3>
            <div className="space-y-5">
              {[RunType.Relevancy, RunType.Factuality, RunType.Consistency].map(
                (x) => (
                  <CollapsibleMetric
                    key={x}
                    field={x}
                    resultData={resultData}
                  ></CollapsibleMetric>
                ),
              )}
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
