import { TreeSelect, type TreeSelectNode } from '@/components/tree-select';
import { Button } from '@/components/ui/button';
import { useModelGroupOptions } from '@/ee/hooks/use-model-group-options';
import {
  buildModelGroupRef,
  isModelGroupRef,
} from '@/ee/utils/model-group-util';
import { useFetchAllAddedModels } from '@/hooks/use-llm-request';
import { forwardRef, useCallback, useMemo } from 'react';
import { useTranslation } from 'react-i18next';

export interface ModelGroupTreeSelectProps {
  modelTypes: string[];
  providers?: string[];
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
  } = useFetchAllAddedModels();
  const {
    groups,
    compatibleGroupIds,
    isFetched: groupsFetched,
    isError: groupsError,
    refetch: refetchGroups,
  } = useModelGroupOptions(modelTypes, allModels, true, providers);

  const settled =
    modelsFetched && !modelsError && groupsFetched && !groupsError;
  const fatalError = modelsError || groupsError;

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
      !isModelGroupRef(value) ||
      compatibleNodes.some((node) => node.id === value)
    ) {
      return compatibleNodes;
    }

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
  }, [compatibleGroupIds, groups, settled, t, value]);

  const renderSelected = useCallback(
    (node: TreeSelectNode | undefined) => (
      <span className="truncate">
        {node?.title || value || placeholder || t('common.pleaseSelect')}
      </span>
    ),
    [placeholder, t, value],
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
