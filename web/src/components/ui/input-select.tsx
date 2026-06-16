import { Input } from '@/components/ui/input';
import { cn } from '@/lib/utils';
import { isEmpty } from 'lodash';
import { ChevronDown, X } from 'lucide-react';
import * as React from 'react';
import { useTranslation } from 'react-i18next';
import { Popover, PopoverContent, PopoverTrigger } from './popover';

/**
 * Extracts text content from a ReactNode for filtering purposes.
 * Handles strings, numbers, JSX elements with nested text, and arrays.
 */
const getNodeText = (node: React.ReactNode): string => {
  if (typeof node === 'string' || typeof node === 'number') {
    return String(node);
  }
  if (React.isValidElement(node)) {
    const children = (node.props as { children?: React.ReactNode }).children;
    if (children) {
      return getNodeText(children);
    }
    return '';
  }
  if (Array.isArray(node)) {
    return node.map(getNodeText).join('');
  }
  return '';
};

/**
 * Recursively look up an option by its value, descending into grouped options.
 * Used for label lookup so grouped structures resolve correctly.
 */
const findOptionByValue = (
  opts: InputSelectOption[],
  val: string | number | Date,
  t: 'text' | 'number' | 'date' | 'datetime',
): InputSelectOption | undefined => {
  for (const o of opts) {
    const matches =
      t === 'number'
        ? Number(o.value) === Number(val)
        : t === 'date' || t === 'datetime'
          ? new Date(o.value).getTime() === new Date(val as any).getTime()
          : String(o.value) === String(val);
    if (matches) return o;
    if (Array.isArray(o.options)) {
      const inner = findOptionByValue(o.options, val, t);
      if (inner) return inner;
    }
  }
  return undefined;
};

/** Interface for tag select options */
export interface InputSelectOption {
  /** Value of the option */
  value: string;
  /** Display label of the option */
  label: string | React.ReactNode;
  /** Optional keywords matched case-insensitively when filtering. */
  keywords?: string[];
  /** If present, this option is a group header; nested options render under it. */
  options?: InputSelectOption[];
  /** If true, the option renders but is not selectable. */
  disabled?: boolean;
}

/** Properties for the InputSelect component */
export interface InputSelectProps {
  /** Options for the select component */
  options?: InputSelectOption[];
  /** Selected values - type depends on the input type */
  value?: string | string[] | number | number[] | Date | Date[];
  /** Callback when value changes */
  onChange?: (
    value: string | string[] | number | number[] | Date | Date[],
  ) => void;
  /** Placeholder text */
  placeholder?: string;
  /** Additional class names (applied to the trigger). */
  className?: string;
  /** Alias of className — used when className is not provided. */
  triggerClassName?: string;
  /** Style object */
  style?: React.CSSProperties;
  /** Whether to allow multiple selections */
  multi?: boolean;
  /** Type of input: text, number, date, or datetime */
  type?: 'text' | 'number' | 'date' | 'datetime';
  /** Disable the trigger entirely. */
  disabled?: boolean;
  /** Show clear (X) button in single-select mode. Default true (preserves current behavior). */
  allowClear?: boolean;
  /** Custom empty-state node. Default t('common.noResults'). */
  emptyData?: React.ReactNode;
  /** When true, allow committing input as a custom value. Default true (preserves current behavior). */
  allowCustomValue?: boolean;
}

/** Internal display for single-select selected value. Click label to re-edit (string labels only). */
const SingleSelectDisplay: React.FC<{
  value: string | number | Date;
  options: InputSelectOption[];
  type: 'text' | 'number' | 'date' | 'datetime';
  disabled?: boolean;
  allowClear?: boolean;
  onEdit: (editText: string) => void;
  onRemove: () => void;
}> = ({ value, options, type, disabled, allowClear, onEdit, onRemove }) => {
  const selectedOption = findOptionByValue(options, value, type);

  const label =
    selectedOption?.label ??
    (type === 'number'
      ? String(value)
      : type === 'date' || type === 'datetime'
        ? new Date(value as any).toLocaleString()
        : String(value));

  const canEdit = typeof label === 'string' && !disabled;

  return (
    <div className={cn('flex items-center max-w-full')}>
      <div
        className={cn(
          'flex-1 truncate',
          canEdit ? 'cursor-text' : 'cursor-default',
        )}
        onClick={(e) => {
          if (!canEdit) return;
          e.stopPropagation();
          onEdit(getNodeText(label));
        }}
      >
        {label}
      </div>
      {allowClear && (
        <button
          type="button"
          className="ml-2 flex-[0_0_24px] text-text-secondary hover:text-text-primary focus:outline-none"
          onClick={(e) => {
            e.stopPropagation();
            onRemove();
          }}
        >
          <X className="h-3 w-3" />
        </button>
      )}
    </div>
  );
};

const InputSelect = React.forwardRef<HTMLInputElement, InputSelectProps>(
  ({
    options = [],
    value = [],
    onChange,
    placeholder = 'Select tags...',
    className,
    triggerClassName,
    style,
    multi = false,
    type = 'text',
    disabled = false,
    allowClear = true,
    emptyData,
    allowCustomValue = true,
  }) => {
    const resolvedClassName = className ?? triggerClassName;
    const [inputValue, setInputValue] = React.useState('');
    const [open, setOpen] = React.useState(false);
    const [isFocused, setIsFocused] = React.useState(false);
    const inputRef = React.useRef<HTMLInputElement>(null);
    const { t } = useTranslation();

    // Normalize value to array for consistent handling based on type
    const normalizedValue = React.useMemo(() => {
      if (Array.isArray(value)) {
        return value;
      } else if (value !== undefined && value !== null) {
        if (type === 'number') {
          return typeof value === 'number' ? [value] : [Number(value)];
        } else if (type === 'date' || type === 'datetime') {
          return value instanceof Date ? [value] : [new Date(value as any)];
        } else {
          return typeof value === 'string' ? [value] : [String(value)];
        }
      } else {
        return [];
      }
    }, [value, type]);

    /**
     * Removes a tag from the selected values
     * @param tagValue - The value of the tag to remove
     */
    const handleRemoveTag = (tagValue: any) => {
      let newValue: any[];

      if (type === 'number') {
        newValue = (normalizedValue as number[]).filter((v) => v !== tagValue);
      } else if (type === 'date' || type === 'datetime') {
        newValue = (normalizedValue as Date[]).filter(
          (v) => v.getTime() !== tagValue.getTime(),
        );
      } else {
        newValue = (normalizedValue as string[]).filter((v) => v !== tagValue);
      }

      // Return single value if not multi-select, otherwise return array
      let result: string | number | Date | string[] | number[] | Date[];
      if (multi) {
        result = newValue;
      } else {
        if (type === 'number') {
          result = newValue[0] || 0;
        } else if (type === 'date' || type === 'datetime') {
          result = newValue[0] || new Date();
        } else {
          result = newValue[0] || '';
        }
      }

      onChange?.(result);
    };

    /**
     * Adds a tag to the selected values
     * @param optionValue - The value of the tag to add
     */
    const handleAddTag = (optionValue: any) => {
      let newValue: any[];

      if (multi) {
        // For multi-select, add to array if not already included
        if (type === 'number') {
          const numValue =
            typeof optionValue === 'number' ? optionValue : Number(optionValue);
          if (
            !(normalizedValue as number[]).includes(numValue) &&
            !isNaN(numValue)
          ) {
            newValue = [...(normalizedValue as number[]), numValue];
            onChange?.(newValue as number[]);
          }
        } else if (type === 'date' || type === 'datetime') {
          const dateValue =
            optionValue instanceof Date ? optionValue : new Date(optionValue);
          if (
            !(normalizedValue as Date[]).some(
              (d) => d.getTime() === dateValue.getTime(),
            )
          ) {
            newValue = [...(normalizedValue as Date[]), dateValue];
            onChange?.(newValue as Date[]);
          }
        } else {
          if (!(normalizedValue as string[]).includes(optionValue)) {
            newValue = [...(normalizedValue as string[]), optionValue];
            onChange?.(newValue as string[]);
          }
        }
      } else {
        // For single-select, replace the value
        if (type === 'number') {
          const numValue =
            typeof optionValue === 'number' ? optionValue : Number(optionValue);
          if (!isNaN(numValue)) {
            onChange?.(numValue);
          }
        } else if (type === 'date' || type === 'datetime') {
          const dateValue =
            optionValue instanceof Date ? optionValue : new Date(optionValue);
          onChange?.(dateValue);
        } else {
          onChange?.(optionValue);
        }
      }

      setInputValue('');
      setOpen(false); // Close the popover after adding a tag
    };

    const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
      const newValue = e.target.value;
      setInputValue(newValue);
      setOpen(!!newValue); // Open popover when there's input
    };

    /**
     * Commits the current inputValue to the selected values, matching by label first,
     * then falling back to the typed value. No-op when inputValue is empty/whitespace,
     * or when custom values are disabled.
     * Used by Enter key handler and blur handler.
     */
    const commitInputValue = () => {
      if (inputValue.trim() === '') return;

      // Match by label text first (searches grouped and flat options)
      const flatForLabelMatch: InputSelectOption[] = [];
      const collect = (opts: InputSelectOption[]) => {
        for (const o of opts) {
          if (Array.isArray(o.options)) collect(o.options);
          else flatForLabelMatch.push(o);
        }
      };
      collect(options);
      const matchedOption = flatForLabelMatch.find(
        (opt) =>
          getNodeText(opt.label).toLowerCase() === inputValue.toLowerCase(),
      );
      if (matchedOption) {
        handleAddTag(matchedOption.value);
        return;
      }

      if (!allowCustomValue) return;

      // Otherwise, validate by type and add as a new value
      let valueToAdd: any;
      if (type === 'number') {
        const numValue = Number(inputValue);
        if (isNaN(numValue)) return;
        valueToAdd = numValue;
      } else if (type === 'date' || type === 'datetime') {
        const dateValue = new Date(inputValue);
        if (isNaN(dateValue.getTime())) return;
        valueToAdd = dateValue;
      } else {
        valueToAdd = inputValue;
      }

      // Skip if value is already selected
      const isAlreadySelected = normalizedValue.some((v) =>
        type === 'number'
          ? Number(v) === Number(valueToAdd)
          : type === 'date' || type === 'datetime'
            ? new Date(v as any).getTime() === valueToAdd.getTime()
            : String(v) === valueToAdd,
      );
      if (!isAlreadySelected) {
        handleAddTag(valueToAdd);
      }
    };
    const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
      if (disabled) return;
      if (
        e.key === 'Backspace' &&
        inputValue === '' &&
        normalizedValue.length > 0
      ) {
        // Remove last tag when pressing backspace on empty input
        const newValue = [...normalizedValue];
        newValue.pop();
        // Return single value if not multi-select, otherwise return array
        let result: string | number | Date | string[] | number[] | Date[];
        if (multi) {
          result = newValue;
        } else {
          if (type === 'number') {
            result = newValue[0] || 0;
          } else if (type === 'date' || type === 'datetime') {
            result = newValue[0] || new Date();
          } else {
            result = newValue[0] || '';
          }
        }

        onChange?.(result);
      } else if (e.key === 'Enter' && inputValue.trim() !== '') {
        e.preventDefault();
        commitInputValue();
      } else if (e.key === 'Escape') {
        inputRef.current?.blur();
        setOpen(false);
      } else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        // Allow navigation in the dropdown
        return;
      }
    };

    const handleContainerClick = () => {
      if (disabled) return;
      inputRef.current?.focus();
      setOpen(true);
      setIsFocused(true);
    };

    const handleInputFocus = () => {
      if (disabled) return;
      setOpen(true);
      setIsFocused(true);
    };

    const handleInputBlur = () => {
      // Delay closing to allow click on options to register
      setTimeout(() => {
        if (disabled) return;
        commitInputValue();
        setOpen(false);
        setIsFocused(false);
      }, 150);
    };

    // Filter options to exclude already selected ones (only for multi-select)
    const availableOptions = multi
      ? options.filter(
          (option) =>
            !normalizedValue.some((v) =>
              type === 'number'
                ? Number(v) === Number(option.value)
                : type === 'date' || type === 'datetime'
                  ? new Date(v as any).getTime() ===
                    new Date(option.value).getTime()
                  : String(v) === option.value,
            ),
        )
      : options;

    const filteredOptions = availableOptions.filter((option) => {
      if (!inputValue) return true;
      const needle = inputValue.toString().toLowerCase();
      if (getNodeText(option.label).toLowerCase().includes(needle)) return true;
      if (option.keywords?.some((k) => k.toLowerCase().includes(needle)))
        return true;
      return false;
    });

    // If there are no matching options but there is an input value, create a new option with the input value
    const showInputAsOption = React.useMemo(() => {
      if (!inputValue) return false;
      if (!allowCustomValue) return false;

      const hasLabelMatch = options.some(
        (option) =>
          getNodeText(option.label).toLowerCase() ===
          inputValue.toString().toLowerCase(),
      );

      let isAlreadySelected = false;
      if (type === 'number') {
        const numValue = Number(inputValue);
        isAlreadySelected =
          !isNaN(numValue) && (normalizedValue as number[]).includes(numValue);
      } else if (type === 'date' || type === 'datetime') {
        const dateValue = new Date(inputValue);
        isAlreadySelected =
          !isNaN(dateValue.getTime()) &&
          (normalizedValue as Date[]).some(
            (d) => d.getTime() === dateValue.getTime(),
          );
      } else {
        isAlreadySelected = (normalizedValue as string[]).includes(inputValue);
      }
      return (
        !hasLabelMatch &&
        !isAlreadySelected &&
        inputValue.toString().trim() !== ''
      );
    }, [inputValue, options, normalizedValue, type, allowCustomValue]);

    const triggerElement = (
      <div
        className={cn(
          'flex items-center gap-1 w-full rounded-md border-0.5 border-border-button bg-bg-input px-3 py-1 min-h-8',
          disabled ? 'cursor-not-allowed opacity-60' : 'cursor-text',
          'outline-none transition-colors',
          'focus-within:outline-none focus-within:ring-1 focus-within:ring-accent-primary',
          resolvedClassName,
        )}
        style={style}
        onClick={handleContainerClick}
        aria-disabled={disabled || undefined}
      >
        {/* Wrapper for tags and input - this part wraps */}
        <div className="flex flex-wrap items-center gap-1 flex-1 min-w-0">
          {/* Render selected tags - only show tags if multi is true or if single select has a value */}
          {multi &&
            normalizedValue.map((tagValue, index) => {
              const option = findOptionByValue(options, tagValue, type) ?? {
                value: String(tagValue),
                label: String(tagValue),
              };

              return (
                <div
                  key={`${tagValue}-${index}`}
                  className="flex items-center bg-bg-card text-text-primary rounded px-2 py-1 text-xs mr-1 mb-1 border border-border-card truncate"
                >
                  <div className="flex-1  truncate">{option.label}</div>
                  <button
                    type="button"
                    className="ml-1 text-text-secondary hover:text-text-primary focus:outline-none disabled:cursor-not-allowed"
                    disabled={disabled}
                    onClick={(e) => {
                      e.stopPropagation();
                      handleRemoveTag(tagValue);
                    }}
                  >
                    <X className="h-3 w-3" />
                  </button>
                </div>
              );
            })}

          {/* For single select, show the selected value as text instead of a tag */}
          {!multi && !isEmpty(normalizedValue[0]) && (
            <SingleSelectDisplay
              value={normalizedValue[0]}
              options={options}
              type={type}
              disabled={disabled}
              allowClear={allowClear}
              onEdit={(editText) => {
                handleRemoveTag(normalizedValue[0]);
                setInputValue(editText);
                setIsFocused(true);
                setOpen(true);
                requestAnimationFrame(() => {
                  const input = inputRef.current;
                  if (input) {
                    input.focus();
                    input.setSelectionRange(editText.length, editText.length);
                  }
                });
              }}
              onRemove={() => handleRemoveTag(normalizedValue[0])}
            />
          )}

          {/* Input field for adding new tags - hide if single select and value is already selected, or in multi select when not focused */}
          {(multi ? isFocused : multi || isEmpty(normalizedValue[0])) && (
            <Input
              ref={inputRef}
              type={
                type === 'date'
                  ? 'date'
                  : type === 'datetime'
                    ? 'datetime-local'
                    : type === 'number'
                      ? 'number'
                      : 'text'
              }
              value={
                type === 'number' && inputValue
                  ? String(inputValue)
                  : type === 'date' || type === 'datetime'
                    ? inputValue
                    : inputValue
              }
              onChange={handleInputChange}
              onKeyDown={handleKeyDown}
              placeholder={
                (
                  multi
                    ? normalizedValue.length === 0
                    : isEmpty(normalizedValue[0])
                )
                  ? placeholder
                  : ''
              }
              className="flex-grow min-w-[50px] border-none px-1 py-0 bg-transparent focus-visible:ring-0 focus-visible:ring-offset-0 h-auto disabled:cursor-not-allowed"
              onClick={(e) => e.stopPropagation()}
              onFocus={handleInputFocus}
              onBlur={handleInputBlur}
              disabled={disabled}
            />
          )}
        </div>
        <ChevronDown
          className={cn(
            'h-4 w-4 text-text-secondary shrink-0 transition-transform',
            open && 'rotate-180',
          )}
        />
      </div>
    );

    return (
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>{triggerElement}</PopoverTrigger>
        <PopoverContent
          className="p-0 min-w-[var(--radix-popover-trigger-width)] max-w-[var(--radix-popover-trigger-width)] data-[state=open]:data-[side=top]:animate-slideDownAndFade data-[state=open]:data-[side=right]:animate-slideLeftAndFade data-[state=open]:data-[side=bottom]:animate-slideUpAndFade data-[state=open]:data-[side=left]:animate-slideRightAndFade"
          align="start"
          sideOffset={4}
          collisionPadding={4}
          onOpenAutoFocus={(e) => e.preventDefault()} // Prevent auto focus on content
        >
          <div className="max-h-60 overflow-auto">
            {filteredOptions.length > 0 &&
              filteredOptions.map((option, idx) => {
                // Group header: render nested items under it.
                if (
                  Array.isArray(option.options) &&
                  option.options.length > 0
                ) {
                  const needle = inputValue.toString().toLowerCase();
                  const nestedFiltered = option.options.filter((nested) => {
                    if (!inputValue) return true;
                    if (
                      getNodeText(nested.label).toLowerCase().includes(needle)
                    )
                      return true;
                    if (
                      nested.keywords?.some((k) =>
                        k.toLowerCase().includes(needle),
                      )
                    )
                      return true;
                    return false;
                  });
                  if (nestedFiltered.length === 0) return null;
                  return (
                    <div key={option.value || `group-${idx}`} className="py-1">
                      <div className="px-4 py-1 text-xs font-medium text-text-disabled uppercase tracking-wide">
                        {option.label}
                      </div>
                      {nestedFiltered.map((nested, nIdx) => (
                        <div
                          key={nested.value || `group-${idx}-item-${nIdx}`}
                          className={cn(
                            'px-4 py-2 hover:bg-border-button cursor-pointer text-text-secondary w-full truncate',
                            nested.disabled && 'pointer-events-none opacity-50',
                          )}
                          onClick={() => {
                            if (nested.disabled) return;
                            let optionValue: any;
                            if (type === 'number') {
                              optionValue = Number(nested.value);
                              if (isNaN(optionValue)) return;
                            } else if (type === 'date' || type === 'datetime') {
                              optionValue = new Date(nested.value);
                              if (isNaN(optionValue.getTime())) return;
                            } else {
                              optionValue = nested.value;
                            }
                            handleAddTag(optionValue);
                          }}
                        >
                          {nested.label}
                        </div>
                      ))}
                    </div>
                  );
                }
                // Flat option.
                return (
                  <div
                    key={option.value || `option-${idx}`}
                    className={cn(
                      'px-4 py-2 hover:bg-border-button cursor-pointer text-text-secondary w-full truncate',
                      option.disabled && 'pointer-events-none opacity-50',
                    )}
                    onClick={() => {
                      if (option.disabled) return;
                      let optionValue: any;
                      if (type === 'number') {
                        optionValue = Number(option.value);
                        if (isNaN(optionValue)) return; // Skip invalid numbers
                      } else if (type === 'date' || type === 'datetime') {
                        optionValue = new Date(option.value);
                        if (isNaN(optionValue.getTime())) return; // Skip invalid dates
                      } else {
                        optionValue = option.value;
                      }
                      handleAddTag(optionValue);
                    }}
                  >
                    {option.label}
                  </div>
                );
              })}
            {showInputAsOption && (
              <div
                key={inputValue}
                className="px-4 py-2 hover:bg-border-button cursor-pointer text-text-secondary w-full truncate"
                onClick={() =>
                  handleAddTag(
                    type === 'number'
                      ? Number(inputValue)
                      : type === 'date' || type === 'datetime'
                        ? new Date(inputValue)
                        : inputValue,
                  )
                }
              >
                {t('common.add')} &quot;{inputValue}&quot;
              </div>
            )}
            {filteredOptions.length === 0 && !showInputAsOption && (
              <div className="px-4 py-2 text-text-secondary w-full truncate">
                {emptyData ?? t('common.noResults')}
              </div>
            )}
          </div>
        </PopoverContent>
      </Popover>
    );
  },
);

InputSelect.displayName = 'InputSelect';

export { InputSelect };
