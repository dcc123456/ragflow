import { LlmIcon } from '@/components/svg-icon';
import { TreeSelect, type TreeSelectNode } from '@/components/tree-select';
import { Button } from '@/components/ui/button';
import { useModelGroupOptions } from '@/ee/hooks/use-model-group-options';
import {
  buildModelGroupRef,
  isModelGroupRef,
} from '@/ee/utils/model-group-util';
import { useFetchAllAddedModels } from '@/hooks/use-llm-request';
import { parseModelValue } from '@/utils/llm-util';
import { forwardRef, useCallback, useMemo } from 'react';
import { useTranslation } from 'react-i18next';

export interface ModelGroupTreeSelectProps {
  modelTypes: string[];
  providers?: string[];
  accessUserId?: string | null;
  dropdownHeader?: React.ReactNode;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  value?: string;
  onChange?: (value: string) => void;
  disabled?: boolean;
  placeholder?: string;
  showSearch?: boolean;
  allowClear?: boolean;
  className?: string;
  testId?: string;
}

export const ModelGroupTreeSelect = forwardRef<
  HTMLButtonElement,
  ModelGroupTreeSelectProps
>(function ModelGroupTreeSelect(
  {
    modelTypes,
    providers,
    accessUserId,
    dropdownHeader,
    open,
    onOpenChange,
    value,
    onChange,
    disabled,
    placeholder,
    showSearch = true,
    allowClear = false,
    className,
    testId,
  },
  ref,
) {
  const { t } = useTranslation();
  const {
    data: allModels,
    isFetched: modelsFetched,
    isError: modelsError,
    refetch: refetchModels,
  } = useFetchAllAddedModels(undefined, undefined, accessUserId);
  const {
    groups,
    compatibleGroupIds,
    isFetched: groupsFetched,
    isError: groupsError,
    refetch: refetchGroups,
  } = useModelGroupOptions(
    modelTypes,
    allModels,
    true,
    providers,
    accessUserId,
  );

  const settled =
    modelsFetched && !modelsError && groupsFetched && !groupsError;
  const fatalError = modelsError || groupsError;

  const resolveModelDisplay = useCallback(
    (modelValue: string) => {
      const parsed = parseModelValue(modelValue);
      const model = parsed?.model_name
        ? undefined
        : allModels.find((item) => item.model_id === modelValue);
      return {
        name: parsed?.model_name ?? model?.name ?? modelValue,
        instance: parsed?.model_instance ?? model?.instance_name,
        provider: parsed?.model_provider ?? model?.provider_name ?? '',
      };
    },
    [allModels],
  );

  // Icon + name (+ instance for the trigger), matching how the direct selector
  // renders a selected model.
  const renderModelLabel = useCallback(
    (modelValue: string, withInstance: boolean) => {
      const { name, instance, provider } = resolveModelDisplay(modelValue);
      return (
        <span className="flex min-w-0 items-center gap-1.5">
          <LlmIcon
            name={provider}
            width={22}
            height={22}
            imgClass="size-[22px] flex-shrink-0"
          />
          <span className="truncate">{name}</span>
          {withInstance && instance && (
            <span className="flex-shrink-0 truncate text-text-secondary">
              {instance}
            </span>
          )}
        </span>
      );
    },
    [resolveModelDisplay],
  );

  const nodes = useMemo(() => {
    const compatibleNodes: TreeSelectNode[] = groups
      .filter((group) => compatibleGroupIds.has(group.id))
      .map((group) => ({
        id: buildModelGroupRef(group.id),
        title: group.group_name,
        label: <span className="truncate">{group.group_name}</span>,
      }));

    if (
      !settled ||
      !value ||
      compatibleNodes.some((node) => node.id === value)
    ) {
      return compatibleNodes;
    }

    if (isModelGroupRef(value)) {
      const currentName =
        groups.find((group) => buildModelGroupRef(group.id) === value)
          ?.group_name || value;
      const title = `${currentName} (${t('setting.modelGroupSelector.unavailable')})`;
      return [
        {
          id: value,
          title,
          disabled: true,
          label: <span className="truncate">{title}</span>,
        },
        ...compatibleNodes,
      ];
    }

    // A persisted direct-model value: echo it so switching to group mode keeps
    // the current selection visible until a group is picked.
    return [
      {
        id: value,
        title: resolveModelDisplay(value).name,
        disabled: true,
        label: renderModelLabel(value, false),
      },
      ...compatibleNodes,
    ];
  }, [
    compatibleGroupIds,
    groups,
    renderModelLabel,
    resolveModelDisplay,
    settled,
    t,
    value,
  ]);

  const renderSelected = useCallback(
    (node: TreeSelectNode | undefined) => {
      if (node && !isModelGroupRef(node.id)) {
        return renderModelLabel(node.id, true);
      }
      return (
        <span className="truncate">
          {node?.title || value || placeholder || t('common.pleaseSelect')}
        </span>
      );
    },
    [placeholder, renderModelLabel, t, value],
  );

  const handleRetry = useCallback(() => {
    void Promise.all([refetchModels(), refetchGroups()]);
  }, [refetchGroups, refetchModels]);

  return (
    <>
      <TreeSelect
        ref={ref}
        data={nodes}
        dropdownHeader={dropdownHeader}
        open={open}
        onOpenChange={onOpenChange}
        value={value}
        onChange={onChange}
        placeholder={placeholder}
        disabled={disabled}
        showSearch={showSearch}
        allowClear={allowClear}
        defaultExpandAll={false}
        className={className}
        renderSelected={renderSelected}
        loading={!settled}
        testId={testId}
      />
      {fatalError && (
        <div role="alert" className="flex items-center gap-2">
          <span className="min-w-0 flex-1 break-words text-sm text-state-error">
            {t('setting.modelGroupPage.loadFailed')}
          </span>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="shrink-0"
            onClick={handleRetry}
          >
            {t('setting.modelGroupPage.retry')}
          </Button>
        </div>
      )}
    </>
  );
});
