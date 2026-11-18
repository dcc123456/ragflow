import type { ModelTreeSelectProps } from '@/components/model-tree-select';
import { Label } from '@/components/ui/label';
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group';
import { ModelTypeProviderMap } from '@/constants/llm';
import { useModelGroupTenantAccess } from '@/ee/hooks/use-model-group-tenant-access';
import { isModelGroupRef } from '@/ee/utils/model-group-util';
import {
  forwardRef,
  useCallback,
  useEffect,
  useId,
  useMemo,
  useState,
} from 'react';
import type { ForwardRefExoticComponent, RefAttributes } from 'react';
import { useTranslation } from 'react-i18next';
import { ModelGroupTreeSelect } from './model-group-tree-select';

type DirectModelSelectComponent = ForwardRefExoticComponent<
  ModelTreeSelectProps & RefAttributes<HTMLButtonElement>
>;

enum SelectionMode {
  Direct = 'direct',
  Group = 'group',
}

export interface ModelReferenceSelectProps extends ModelTreeSelectProps {
  directModelSelect: DirectModelSelectComponent;
  modelTypes: string[];
}

export const ModelReferenceSelect = forwardRef<
  HTMLButtonElement,
  ModelReferenceSelectProps
>(function ModelReferenceSelect(
  {
    directModelSelect: DirectModelSelect,
    modelTypes,
    providers,
    value,
    onChange,
    disabled,
    placeholder,
    showSearch,
    allowClear,
    className,
    renderSelected,
    testId,
    ownerTenantId,
    skipTenantInfo,
  },
  ref,
) {
  const { t } = useTranslation();
  const radioGroupId = useId();
  const [mode, setMode] = useState<SelectionMode>(() =>
    isModelGroupRef(value) ? SelectionMode.Group : SelectionMode.Direct,
  );
  const [open, setOpen] = useState(false);
  const { canUseGroups, isSettled: tenantAccessSettled } =
    useModelGroupTenantAccess({
      ownerTenantId,
      skipTenantInfo,
    });
  const directOnly = !canUseGroups;
  const groupValue = isModelGroupRef(value);
  const awaitingModeValueClear =
    !directOnly &&
    Boolean(value) &&
    (mode === SelectionMode.Group) !== groupValue;
  const awaitingTenantAccess = !tenantAccessSettled && groupValue;
  const selectorValue =
    awaitingModeValueClear || awaitingTenantAccess ? '' : value;
  const selectorDisabled =
    disabled || awaitingModeValueClear || awaitingTenantAccess;

  useEffect(() => {
    if (value)
      setMode(
        isModelGroupRef(value) ? SelectionMode.Group : SelectionMode.Direct,
      );
  }, [value]);

  const allowedProviders = useMemo(() => {
    if (providers) return providers;
    for (const type of modelTypes) {
      const declared = ModelTypeProviderMap[type];
      if (declared) return declared;
    }
    return undefined;
  }, [providers, modelTypes]);

  const handleModeChange = useCallback(
    (nextMode: string) => {
      if (
        (nextMode !== SelectionMode.Direct &&
          nextMode !== SelectionMode.Group) ||
        nextMode === mode
      ) {
        return;
      }
      setMode(nextMode);
      if (value) onChange?.('');
    },
    [mode, onChange, value],
  );

  const dropdownHeader = directOnly ? undefined : (
    <RadioGroup
      value={mode}
      onValueChange={handleModeChange}
      disabled={disabled}
      className="flex gap-4 border-b px-3 py-2"
    >
      <div className="flex items-center gap-2">
        <RadioGroupItem
          id={`${radioGroupId}-direct`}
          value={SelectionMode.Direct}
          disabled={disabled}
        />
        <Label htmlFor={`${radioGroupId}-direct`}>
          {t('setting.modelGroupSelector.direct')}
        </Label>
      </div>
      <div className="flex items-center gap-2">
        <RadioGroupItem
          id={`${radioGroupId}-group`}
          value={SelectionMode.Group}
          disabled={disabled}
        />
        <Label htmlFor={`${radioGroupId}-group`}>
          {t('setting.modelGroupSelector.group')}
        </Label>
      </div>
    </RadioGroup>
  );

  const directSelect = (
    <DirectModelSelect
      ref={ref}
      modelTypes={modelTypes}
      providers={allowedProviders}
      dropdownHeader={dropdownHeader}
      open={open}
      onOpenChange={setOpen}
      value={selectorValue}
      onChange={onChange}
      disabled={selectorDisabled}
      placeholder={placeholder}
      showSearch={showSearch}
      allowClear={allowClear}
      className={className}
      renderSelected={renderSelected}
      testId={testId}
      ownerTenantId={ownerTenantId}
      skipTenantInfo={skipTenantInfo}
    />
  );

  if (directOnly) return directSelect;

  return mode === SelectionMode.Group ? (
    <ModelGroupTreeSelect
      ref={ref}
      modelTypes={modelTypes}
      providers={allowedProviders}
      dropdownHeader={dropdownHeader}
      open={open}
      onOpenChange={setOpen}
      value={selectorValue}
      onChange={onChange}
      disabled={selectorDisabled}
      placeholder={placeholder}
      showSearch={showSearch}
      allowClear={allowClear}
      className={className}
      testId={testId}
    />
  ) : (
    directSelect
  );
});
