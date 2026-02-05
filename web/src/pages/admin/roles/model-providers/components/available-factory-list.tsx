import { forwardRef, useMemo, useState } from 'react';

import { Link } from 'react-router';

import { LucideArrowUpRight, LucidePlus } from 'lucide-react';

import * as ToggleGroupPrimitive from '@radix-ui/react-toggle-group';

import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';

import { LlmIcon } from '@/components/svg-icon';
import { SearchInput } from '@/components/ui/input';
import { ScrollArea } from '@/components/ui/scroll-area';

import { APIMapUrl } from '@/constants/llm';
import { useSetModalState, useTranslate } from '@/hooks/common-hooks';

import { cn } from '@/lib/utils';

import message from '@/components/ui/message';
import {
  LlmFactory,
  useAddFactory,
  useLlmFactoryList,
} from '@/pages/admin/hooks/useLlm';
import ProviderModal from '../model-config/ProviderModal';

const TAG_FILTER_ALL = '<all>';

const TagButton = forwardRef<
  HTMLButtonElement,
  React.ButtonHTMLAttributes<HTMLButtonElement>
>(function TagButton(props, ref) {
  return (
    <button
      ref={ref}
      {...props}
      className={cn(
        'px-1 py-0 text-xs rounded-sm transition-colors',
        'bg-bg-card text-text-secondary',
        'hover:bg-border-button focus:bg-border-button',
        'data-[state=on]:bg-text-primary data-[state=on]:text-text-primary-inverse',
        'data-[state=on]:hover:bg-text-primary/90 data-[state=on]:focus:bg-white/90',
        props.className,
      )}
    >
      {props.children}
    </button>
  );
});

function ModelFactoryCard({ factory }: { factory: LlmFactory }) {
  const { t } = useTranslate('setting');
  const { t: tMsg } = useTranslate('message');
  const apiUrl = APIMapUrl[factory.name as keyof typeof APIMapUrl];

  const {
    visible,
    showModal: showAddModelModal,
    hideModal: hideAddModelModal,
  } = useSetModalState();

  const { addFactory, isPending } = useAddFactory(factory.name);

  return (
    <>
      <Card className="group !shadow-none border-0.5 bg-transparent transition-colors hover:bg-bg-card focus-within:bg-bg-card">
        <CardHeader className="p-4">
          <CardTitle className="flex items-center gap-2">
            <LlmIcon name={factory.name} imgClass="size-8" />

            <span className="w-0 flex-1 text-base font-normal inline-flex items-center gap-2">
              <span className="truncate">{factory.name}</span>

              {apiUrl && (
                <Link
                  to={apiUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="block flex-none"
                >
                  <Button
                    variant="ghost"
                    size="icon"
                    className="p-0 size-5 flex bg-transparent"
                  >
                    <LucideArrowUpRight className="size-[1em]" />
                  </Button>
                </Link>
              )}
            </span>

            <Button
              size="sm"
              className="
                flex-none ml-auto px-2 items-center gap-1 text-xs h-6 rounded-md transition-all opacity-0
                group-hover:opacity-100 group-focus-within:opacity-100
              "
              onClick={() => showAddModelModal()}
              loading={isPending}
            >
              <LucidePlus className="size-[1em]" />
              <span>{t('addTheModel')}</span>
            </Button>
          </CardTitle>
        </CardHeader>

        <CardContent className="px-4 pb-4">
          <ul className="list-none m-0 flex flex-wrap gap-1">
            {factory.sortedTags.map((tag, index) => (
              <span
                key={`$${index}.${tag}`}
                className="px-1 flex items-center h-5 text-xs bg-bg-card text-text-secondary rounded-md"
              >
                {tag}
              </span>
            ))}
          </ul>
        </CardContent>
      </Card>

      <ProviderModal
        llmFactory={factory.name}
        open={visible}
        onClose={hideAddModelModal}
        onSubmit={async (data) => {
          await addFactory(data);
          hideAddModelModal();
          message.success(tMsg('modified'));
        }}
      />
    </>
  );
}

export default function AvailableFactoryList() {
  const { t } = useTranslate('setting');
  const { t: tCommon } = useTranslate('common');

  const { data: factoryList, sortedAllTags } = useLlmFactoryList();

  const [searchTerm, setSearchTerm] = useState('');
  const [selectedTag, setSelectedTag] = useState<string>(TAG_FILTER_ALL);

  const filteredModels = useMemo(() => {
    const models = factoryList.filter((model) => {
      const matchesSearch = model.name
        .toLowerCase()
        .includes(searchTerm.toLowerCase());
      const matchesTag =
        selectedTag === TAG_FILTER_ALL ||
        model.sortedTags.some((tag) => tag === selectedTag);
      return matchesSearch && matchesTag;
    });
    return models;
  }, [factoryList, searchTerm, selectedTag]);

  return (
    <Card className="!shadow-none relative h-full border-0 bg-transparent rounded-xl flex flex-col">
      <CardHeader className="flex-none flex flex-col gap-3 space-y-0">
        <CardTitle className="text-base leading-none">
          {t('availableModels')}
        </CardTitle>

        <SearchInput
          className="
            w-full px-4 py-2 pl-10 bg-bg-input
            border-0.5 border-border-default rounded-lg
            focus:outline-none focus:ring-0.5 focus:ring-border-button transition-colors
          "
          type="text"
          placeholder={t('search')}
          value={searchTerm}
          onChange={(e) => setSearchTerm(e.target.value)}
        />

        {/* Tags Filter */}
        <ToggleGroupPrimitive.Root
          className="flex flex-wrap gap-2"
          type="single"
          value={selectedTag}
          onValueChange={(value) => {
            if (value) {
              setSelectedTag(value);
            }
          }}
        >
          <ToggleGroupPrimitive.Item value={TAG_FILTER_ALL} asChild>
            <TagButton>{tCommon('all')}</TagButton>
          </ToggleGroupPrimitive.Item>

          {sortedAllTags.map((tag) => (
            <ToggleGroupPrimitive.Item key={tag} value={tag} asChild>
              <TagButton>{tag}</TagButton>
            </ToggleGroupPrimitive.Item>
          ))}
        </ToggleGroupPrimitive.Root>
      </CardHeader>

      <ScrollArea className="flex-1">
        <CardContent className="px-6 pb-6">
          <ul className="list-none space-y-6">
            {filteredModels.map((model) => (
              <li key={model.name}>
                <ModelFactoryCard factory={model} />
              </li>
            ))}
          </ul>
        </CardContent>
      </ScrollArea>
    </Card>
  );
}
