import { useState } from 'react';

import {
  LucideChevronsDown,
  LucideSettings,
  LucideTrash2,
  LucideX,
} from 'lucide-react';

import * as AccordionPrimitive from '@radix-ui/react-accordion';

import { EmptyType } from '@/components/empty/constant';
import Empty from '@/components/empty/empty';

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog';

import { LlmIcon } from '@/components/svg-icon';
import { Button } from '@/components/ui/button';

import { useSetModalState, useTranslate } from '@/hooks/common-hooks';
import { LlmItem } from '@/hooks/use-llm-request';

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';

import { cn } from '@/lib/utils';

import {
  LLM_MODEL_TAG_MAP,
  useAddFactory,
  useDeleteMyFactory,
  useMyLlmList,
} from '@/pages/admin/hooks/useLlm';
import { LocalLlmFactories } from '@/pages/user-setting/constants';

import message from '@/components/ui/message';
import { getRealModelName } from '@/utils/llm-util';
import { useTranslation } from 'react-i18next';
import ProviderModal from '../model-config/ProviderModal';

type TagType =
  | 'LLM'
  | 'TEXT EMBEDDING'
  | 'TEXT RE-RANK'
  | 'TTS'
  | 'SPEECH2TEXT'
  | 'IMAGE2TEXT'
  | 'MODERATION';

const sortTags = (tags: string) => {
  const orderMap: Record<TagType, number> = {
    LLM: 1,
    'TEXT EMBEDDING': 2,
    'TEXT RE-RANK': 3,
    TTS: 4,
    SPEECH2TEXT: 5,
    IMAGE2TEXT: 6,
    MODERATION: 7,
  };

  return tags
    .split(',')
    .map((tag) => tag.trim())
    .sort(
      (a, b) =>
        (orderMap[a as TagType] || Infinity) -
        (orderMap[b as TagType] || Infinity),
    );
};

export const ModelProviderCard = ({ item }: { item: LlmItem }) => {
  const { t } = useTranslation();

  const apiKeyModal = useSetModalState();
  const [expanded, setExpanded] = useState(false);

  const { delete: deleteFactory } = useDeleteMyFactory(item.name);
  const { addFactory, isPending } = useAddFactory(item.name);

  return (
    <AccordionPrimitive.Root
      type="single"
      collapsible
      value={expanded ? item.name : ''}
      onValueChange={(value) => setExpanded(value === item.name)}
    >
      <AccordionPrimitive.Item value={item.name} asChild>
        <Card className="!shadow-none relative h-full border-0.5 border-border-button bg-transparent rounded-lg flex flex-col">
          <AccordionPrimitive.Header>
            <CardHeader className="space-y-0 p-4 flex flex-row items-center gap-3">
              <CardTitle className="flex items-center gap-2 text-lg">
                <LlmIcon name={item.name} imgClass="size-[1.35em]" />
                <h2 className="font-normal text-[1em] text-text-primary">
                  {item.name}
                </h2>
              </CardTitle>

              <div className="ml-auto flex flex-row items-center gap-2">
                <Button
                  className="h-8"
                  variant="ghost"
                  onClick={apiKeyModal.showModal}
                >
                  <LucideSettings />

                  <span className="max-xl:sr-only">
                    {LocalLlmFactories.some((x) => x === item.name)
                      ? t('setting.addTheModel')
                      : t('setting.apiKey')}
                  </span>
                </Button>

                <AccordionPrimitive.Trigger asChild>
                  <Button className="h-8" variant="ghost">
                    <span className="max-xl:sr-only">
                      {expanded
                        ? t('setting.hideModels')
                        : t('setting.showMoreModels')}
                    </span>
                    <LucideChevronsDown
                      className={cn(
                        'size-[1em] transition-transform',
                        expanded ? 'rotate-180' : '',
                      )}
                    />
                  </Button>
                </AccordionPrimitive.Trigger>

                <AlertDialog>
                  <AlertDialogTrigger asChild>
                    <Button
                      className="border-0 size-8"
                      variant="danger"
                      size="icon"
                    >
                      <LucideTrash2 />
                    </Button>
                  </AlertDialogTrigger>

                  <AlertDialogContent
                    aria-describedby={undefined}
                    onSelect={(e) => e.preventDefault()}
                    onClick={(e) => e.stopPropagation()}
                    className="bg-bg-base gap-8"
                  >
                    <AlertDialogHeader>
                      <AlertDialogTitle>
                        {t('common.deleteModalTitle')}
                        <AlertDialogCancel className="border-none bg-transparent hover:border-none hover:bg-transparent absolute right-3 top-3 hover:text-text-primary">
                          <LucideX size={16} />
                        </AlertDialogCancel>
                      </AlertDialogTitle>
                    </AlertDialogHeader>

                    <div className="p-4 flex items-center gap-2 border-0.5 border-border-button rounded-lg">
                      <LlmIcon name={item.name} imgClass="size-[1.5em]" />
                      {item.name}
                    </div>

                    <AlertDialogFooter className="!space-x-4">
                      <AlertDialogCancel className="h-8 px-3">
                        {t('common.cancel')}
                      </AlertDialogCancel>

                      <AlertDialogAction
                        onClick={async () => {
                          await deleteFactory();
                          message.success(t('message.deleted'));
                        }}
                        className="h-8 px-3 bg-state-error text-text-primary hover:text-text-primary hover:bg-state-error"
                      >
                        {t('common.delete')}
                      </AlertDialogAction>
                    </AlertDialogFooter>
                  </AlertDialogContent>
                </AlertDialog>
              </div>
            </CardHeader>
          </AccordionPrimitive.Header>

          <AccordionPrimitive.Content className="data-[state=closed]:animate-accordion-up data-[state=open]:animate-accordion-down overflow-hidden text-sm">
            <CardContent className="px-4 pb-4">
              <div className="flex flex-wrap gap-1">
                {sortTags(item.tags).map((tag, index) => (
                  <span
                    key={index}
                    className="px-2 py-1 text-xs bg-bg-card text-text-secondary rounded-md"
                  >
                    {LLM_MODEL_TAG_MAP[
                      tag.trim() as keyof typeof LLM_MODEL_TAG_MAP
                    ] || tag.trim()}
                  </span>
                ))}
              </div>

              <div className="mt-4 bg-bg-card rounded-lg max-h-96 overflow-x-hidden overflow-y-auto scrollbar-auto">
                {item.llm.map((model) => (
                  <div
                    key={model.name}
                    className={cn(
                      'px-4 py-2 flex items-center justify-between',
                      'hover:bg-bg-card transition-colors',
                      'border-b-0.5 last:border-0 border-border-button',
                    )}
                  >
                    <div className="flex items-center gap-2">
                      <span>{getRealModelName(model.name)}</span>
                      <span className="px-2 py-1 text-xs bg-bg-card text-text-secondary rounded-md">
                        {model.type}
                      </span>
                    </div>
                  </div>
                ))}
              </div>
            </CardContent>
          </AccordionPrimitive.Content>
        </Card>
      </AccordionPrimitive.Item>

      <ProviderModal
        llmFactory={item.name}
        loading={isPending}
        open={apiKeyModal.visible}
        onClose={apiKeyModal.hideModal}
        onSubmit={async (data) => {
          await addFactory(data);
          apiKeyModal.hideModal();
          message.success(t('message.updated'));
        }}
      />
    </AccordionPrimitive.Root>
  );
};

export default function AddedModels() {
  const { t } = useTranslate('admin.roleModelProviders');
  const { data: myLlmList } = useMyLlmList();

  return (
    <div>
      <div className="space-y-4">
        {myLlmList.length ? (
          myLlmList.map((llm) => (
            <ModelProviderCard key={llm.name} item={llm} />
          ))
        ) : (
          <Empty
            className="py-16"
            type={EmptyType.Data}
            text={t('noModelsAdded')}
          />
        )}
      </div>
    </div>
  );
}
