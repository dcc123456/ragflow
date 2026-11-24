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
  allowModelGroups?: boolean;
}

export const ModelReferenceSelect = forwardRef<
  HTMLButtonElement,
  ModelReferenceSelectProps
>(function ModelReferenceSelect(
  {
    directModelSelect: DirectModelSelect,
    modelTypes,
    allowModelGroups = false,
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
    accessUserId,
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
  const { canUseGroups: tenantCanUseGroups, isSettled: tenantAccessSettled } =
    useModelGroupTenantAccess({
      ownerTenantId,
      skipTenantInfo: skipTenantInfo || allowModelGroups,
    });
  const canUseGroups = allowModelGroups || tenantCanUseGroups;
  const groupAccessSettled = allowModelGroups || tenantAccessSettled;
  const directOnly = !canUseGroups;
  const groupValue = isModelGroupRef(value);
  // Switching the mode radio is a pure view change: the value is never cleared,
  // so a selection survives a round trip between the two modes. A group
  // reference stays hidden from the direct selector (it would render as a
  // missing model), but the group selector echoes a persisted direct model so
  // the current selection remains visible while group mode is active. Choosing
  // an option afterwards overwrites it.
  const hideGroupValueInDirectMode =
    !directOnly && groupValue && mode === SelectionMode.Direct;
  const awaitingTenantAccess = !groupAccessSettled && groupValue;
  const selectorValue =
    hideGroupValueInDirectMode || awaitingTenantAccess ? '' : value;
  const selectorDisabled = disabled || awaitingTenantAccess;

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
    },
    [mode],
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
      accessUserId={accessUserId}
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
      accessUserId={accessUserId}
    />
  ) : (
    directSelect
  );
});
