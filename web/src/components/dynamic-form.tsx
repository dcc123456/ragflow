import { zodResolver } from '@hookform/resolvers/zod';
import {
  createContext,
  forwardRef,
  useCallback,
  useContext,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
} from 'react';
import {
  ControllerRenderProps,
  DefaultValues,
  FieldValues,
  SubmitHandler,
  UseFormTrigger,
  useForm,
  useFormContext,
  useWatch,
} from 'react-hook-form';
import { ZodSchema, z } from 'zod';

import EditTag from '@/components/edit-tag';
import { SelectWithSearch } from '@/components/originui/select-with-search';
import { RAGFlowFormItem } from '@/components/ragflow-form';
import { Checkbox } from '@/components/ui/checkbox';
import { Form } from '@/components/ui/form';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { cn } from '@/lib/utils';
import { t } from 'i18next';
import { Loader } from 'lucide-react';
import { InputSelect, InputSelectOption } from './ui/input-select';
import { MultiSelect, MultiSelectOptionType } from './ui/multi-select';
import { Segmented } from './ui/segmented';
import { Switch } from './ui/switch';

const getNestedValue = (obj: any, path: string) => {
  return path.split('.').reduce((current, key) => {
    return current && current[key] !== undefined ? current[key] : undefined;
  }, obj);
};

const setNestedValue = (
  obj: Record<string, any>,
  path: string,
  value: any,
): void => {
  const keys = path.split('.');
  let current = obj;
  for (let i = 0; i < keys.length - 1; i++) {
    const key = keys[i];
    if (!current[key] || typeof current[key] !== 'object') {
      current[key] = {};
    }
    current = current[key];
  }
  current[keys[keys.length - 1]] = value;
};

/**
 * Properties of this field will be treated as static attributes and will be filtered out during form submission.
 */
export const FilterFormField = 'RAG_DY_STATIC';

// Field type enumeration
export enum FormFieldType {
  Text = 'text',
  Email = 'email',
  Password = 'password',
  Number = 'number',
  Textarea = 'textarea',
  Select = 'select',
  MultiSelect = 'multi-select',
  Checkbox = 'checkbox',
  Switch = 'switch',
  Tag = 'tag',
  Segmented = 'segmented',
  InputSelect = 'input-select',
  Custom = 'custom',
}

// Field configuration interface
export interface FormFieldConfig {
  name: string;
  label: string;
  hideLabel?: boolean;
  type: FormFieldType;
  hidden?: boolean;
  required?: boolean;
  placeholder?: string;
  options?: { label: string; value: string }[];
  allowCustomValue?: boolean;
  defaultValue?: any;
  validation?: {
    pattern?: RegExp;
    minLength?: number;
    maxLength?: number;
    min?: number;
    max?: number;
    message?: string;
  };
  render?: (fieldProps: ControllerRenderProps) => React.ReactNode;
  horizontal?: boolean;
  onChange?: (value: any) => void;
  tooltip?: React.ReactNode;
  customValidate?: (
    value: any,
    formValues: any,
  ) => string | boolean | Promise<string | boolean>;
  dependencies?: string[];
  schema?: ZodSchema;
  shouldRender?: (formValues: any) => boolean;
  labelClassName?: string;
  className?: string;
  disabled?: boolean;
}

// Component props interface
interface DynamicFormProps<T extends FieldValues> {
  fields: FormFieldConfig[];
  onSubmit: SubmitHandler<T>;
  className?: string;
  children?: React.ReactNode;
  defaultValues?: DefaultValues<T>;
  labelClassName?: string;
}

// Form ref interface
export interface DynamicFormRef {
  submit: () => void;
  isDirty: () => boolean;
  getValues: (name?: string) => any;
  reset: (values?: any) => void;
  trigger: UseFormTrigger<any>;
  watch: (field: string, callback: (value: any) => void) => () => void;
  watchDirty: (callback: (isDirty: boolean, values: any) => void) => () => void;
  updateFieldType: (fieldName: string, newType: FormFieldType) => void;
  onFieldUpdate: (
    fieldName: string,
    newFieldProperties: Partial<FormFieldConfig>,
  ) => void;
  filterActiveValues: (values: any) => any;
}

// Internal context used to expose filterActiveValues to SavingButton without
// relying on attaching ad-hoc properties to the react-hook-form instance.
const DynamicFormContext = createContext<{
  filterActiveValues?: (values: any) => any;
} | null>(null);

// Generate Zod validation schema based on field configurations
export const generateSchema = (fields: FormFieldConfig[]): ZodSchema<any> => {
  const schema: Record<string, ZodSchema> = {};
  const nestedSchemas: Record<
    string,
    Record<string, ZodSchema | Record<string, any>>
  > = {};

  const isZodSchema = (v: unknown): v is ZodSchema => v instanceof z.ZodType;

  fields.forEach((field) => {
    let fieldSchema: ZodSchema;

    if (field.schema) {
      fieldSchema = field.schema;
    } else {
      switch (field.type) {
        case FormFieldType.Email:
          fieldSchema = z.string().email('Please enter a valid email address');
          break;
        case FormFieldType.MultiSelect:
          fieldSchema = z.array(z.string()).optional();
          break;
        case FormFieldType.Segmented:
          fieldSchema = z.string();
          break;
        case FormFieldType.Number: {
          // Use preprocess to convert empty strings to undefined so that
          // z.coerce.number().optional() does not silently coerce "" -> 0.
          let numberSchema: z.ZodTypeAny = z.preprocess(
            (val) => {
              if (val === '' || val === null || val === undefined) {
                return undefined;
              }
              const num = Number(val);
              return Number.isNaN(num) ? val : num;
            },
            z
              .number({
                invalid_type_error:
                  field.validation?.message || 'Must be a number',
              })
              .optional(),
          );

          if (field.validation?.min !== undefined) {
            numberSchema = numberSchema.refine(
              (val) =>
                val === undefined ||
                (val as number) >= (field.validation!.min as number),
              {
                message:
                  field.validation.message ||
                  `Value cannot be less than ${field.validation.min}`,
              },
            );
          }
          if (field.validation?.max !== undefined) {
            numberSchema = numberSchema.refine(
              (val) =>
                val === undefined ||
                (val as number) <= (field.validation!.max as number),
              {
                message:
                  field.validation.message ||
                  `Value cannot be greater than ${field.validation.max}`,
              },
            );
          }

          fieldSchema = numberSchema;
          break;
        }
        case FormFieldType.Checkbox:
        case FormFieldType.Switch:
          fieldSchema = z.boolean();
          break;
        case FormFieldType.Tag:
          fieldSchema = z.array(z.string()).optional();
          break;
        default:
          fieldSchema = z.string();
          break;
      }
    }

    // Handle required fields
    if (field.required) {
      const requiredMessage =
        field.validation?.message || `${field.label} is required`;

      if (field.type === FormFieldType.Checkbox) {
        fieldSchema = (fieldSchema as z.ZodBoolean).refine(
          (val) => val === true,
          { message: requiredMessage },
        );
      } else if (
        field.type === FormFieldType.Tag ||
        field.type === FormFieldType.MultiSelect
      ) {
        fieldSchema = z.array(z.string()).min(1, { message: requiredMessage });
      } else if (field.type === FormFieldType.Number) {
        fieldSchema = fieldSchema.refine((val) => val !== undefined, {
          message: requiredMessage,
        });
      } else {
        fieldSchema = (fieldSchema as z.ZodString).min(1, {
          message: requiredMessage,
        });
      }
    }

    if (
      !field.required &&
      field.type !== FormFieldType.Number &&
      field.type !== FormFieldType.Tag &&
      field.type !== FormFieldType.MultiSelect
    ) {
      fieldSchema = fieldSchema.optional();
    }

    // Handle other validation rules for string-like fields only
    if (
      field.required &&
      field.type !== FormFieldType.Number &&
      field.type !== FormFieldType.Checkbox &&
      field.type !== FormFieldType.Switch &&
      field.type !== FormFieldType.Custom &&
      field.type !== FormFieldType.Tag &&
      field.type !== FormFieldType.MultiSelect
    ) {
      fieldSchema = fieldSchema as z.ZodString;

      if (field.validation?.minLength !== undefined) {
        fieldSchema = (fieldSchema as z.ZodString).min(
          field.validation.minLength,
          field.validation.message ||
            `Enter at least ${field.validation.minLength} characters`,
        );
      }

      if (field.validation?.maxLength !== undefined) {
        fieldSchema = (fieldSchema as z.ZodString).max(
          field.validation.maxLength,
          field.validation.message ||
            `Enter up to ${field.validation.maxLength} characters`,
        );
      }

      if (field.validation?.pattern) {
        fieldSchema = (fieldSchema as z.ZodString).regex(
          field.validation.pattern,
          field.validation.message || 'Invalid input format',
        );
      }
    }

    if (field.name.includes('.')) {
      const keys = field.name.split('.');
      const firstKey = keys[0];

      if (!nestedSchemas[firstKey]) {
        nestedSchemas[firstKey] = {} as Record<
          string,
          ZodSchema | Record<string, any>
        >;
      }

      let currentSchema: Record<string, ZodSchema | Record<string, any>> =
        nestedSchemas[firstKey];
      for (let i = 1; i < keys.length - 1; i++) {
        const key = keys[i];
        if (!currentSchema[key] || isZodSchema(currentSchema[key])) {
          currentSchema[key] = {} as Record<
            string,
            ZodSchema | Record<string, any>
          >;
        }
        currentSchema = currentSchema[key] as Record<
          string,
          ZodSchema | Record<string, any>
        >;
      }

      const lastKey = keys[keys.length - 1];
      currentSchema[lastKey] = fieldSchema;
    } else {
      schema[field.name] = fieldSchema;
    }
  });

  Object.keys(nestedSchemas).forEach((key) => {
    const buildNestedSchema = (
      obj: Record<string, ZodSchema | Record<string, any>>,
    ): ZodSchema => {
      const nestedSchema: Record<string, ZodSchema> = {};
      Object.keys(obj).forEach((subKey) => {
        const value = obj[subKey];
        if (!isZodSchema(value)) {
          nestedSchema[subKey] = buildNestedSchema(
            value as Record<string, ZodSchema | Record<string, any>>,
          );
        } else {
          nestedSchema[subKey] = value;
        }
      });
      return z.object(nestedSchema);
    };

    schema[key] = buildNestedSchema(nestedSchemas[key]);
  });
  return z.object(schema);
};

// Generate default values based on field configurations
const generateDefaultValues = <T extends FieldValues>(
  fields: FormFieldConfig[],
): DefaultValues<T> => {
  const defaultValues: Record<string, any> = {};

  fields.forEach((field) => {
    let value: any;
    if (field.defaultValue !== undefined) {
      value = field.defaultValue;
    } else if (
      field.type === FormFieldType.Checkbox ||
      field.type === FormFieldType.Switch
    ) {
      value = false;
    } else if (
      field.type === FormFieldType.Tag ||
      field.type === FormFieldType.MultiSelect
    ) {
      value = [];
    } else {
      value = '';
    }

    if (field.name.includes('.')) {
      setNestedValue(defaultValues, field.name, value);
    } else {
      defaultValues[field.name] = value;
    }
  });

  return defaultValues as DefaultValues<T>;
};

// Extract the raw "value" from a Controller field onChange event so that
// `field.onChange` always receives a normalized value (string/number/etc.)
// regardless of whether the underlying component fires a DOM event or a value.
const extractFieldValue = (eventOrValue: any): any => {
  if (eventOrValue === null || eventOrValue === undefined) return eventOrValue;
  if (
    typeof eventOrValue === 'object' &&
    eventOrValue.target &&
    'value' in eventOrValue.target
  ) {
    return eventOrValue.target.value;
  }
  return eventOrValue;
};

// Wraps the fieldProps so that `field.onChange` (if provided) is invoked
// with the normalized value in addition to the standard react-hook-form
// update. Returns the props unchanged when no custom onChange is supplied.
const wrapOnChange = (
  field: FormFieldConfig,
  fieldProps: ControllerRenderProps,
): ControllerRenderProps => {
  if (!field.onChange) return fieldProps;
  return {
    ...fieldProps,
    onChange: (eventOrValue: any) => {
      fieldProps.onChange?.(eventOrValue);
      field.onChange?.(extractFieldValue(eventOrValue));
    },
  };
};

// MultiSelect uses onValueChange instead of onChange. The wrapper is always
// applied (regardless of whether field.onChange is supplied) because the
// underlying component requires onValueChange unconditionally.
const wrapOnValueChange = (
  field: FormFieldConfig,
  fieldProps: ControllerRenderProps,
): ControllerRenderProps & { onValueChange: (value: string[]) => void } => ({
  ...fieldProps,
  onValueChange: (value: string[]) => {
    fieldProps.onChange?.(value);
    field.onChange?.(value);
  },
});

// Registry of field-type -> inner control renderer. Each entry is responsible
// for rendering the inner control only; the surrounding <RAGFlowFormItem>
// wrapper is added uniformly by RenderField. To add a new field type, add a
// single entry to this map — no switch to keep in sync.
type FieldControlRenderer = (
  field: FormFieldConfig,
  fieldProps: ControllerRenderProps,
) => React.ReactNode;

const renderInputField: FieldControlRenderer = (field, fieldProps) => (
  <div className="w-full">
    <Input
      {...wrapOnChange(field, fieldProps)}
      type={field.type}
      placeholder={field.placeholder}
      disabled={field.disabled}
    />
  </div>
);

const fieldControlRenderers: Record<FormFieldType, FieldControlRenderer> = {
  [FormFieldType.Text]: renderInputField,
  [FormFieldType.Email]: renderInputField,
  [FormFieldType.Password]: renderInputField,
  [FormFieldType.Number]: renderInputField,
  [FormFieldType.Textarea]: (field, fieldProps) => (
    <Textarea
      {...wrapOnChange(field, fieldProps)}
      placeholder={field.placeholder}
      disabled={field.disabled}
    />
  ),
  [FormFieldType.Select]: (field, fieldProps) => (
    <SelectWithSearch
      triggerClassName="!shrink"
      {...wrapOnChange(field, fieldProps)}
      options={field.options}
      allowCustomValue={field.allowCustomValue}
      disabled={field.disabled}
    />
  ),
  [FormFieldType.InputSelect]: (field, fieldProps) => (
    <InputSelect
      triggerClassName="!shrink"
      {...wrapOnChange(field, fieldProps)}
      options={field.options as InputSelectOption[] | undefined}
      allowCustomValue={field.allowCustomValue || false}
      allowClear={false}
      disabled={field.disabled}
    />
  ),
  [FormFieldType.MultiSelect]: (field, fieldProps) => {
    const wrapped = wrapOnValueChange(field, fieldProps);
    return (
      <MultiSelect
        variant="inverted"
        maxCount={100}
        options={field.options as MultiSelectOptionType[]}
        disabled={field.disabled}
        value={(fieldProps.value as string[] | undefined) ?? []}
        onValueChange={wrapped.onValueChange}
      />
    );
  },
  [FormFieldType.Checkbox]: (field, fieldProps) => (
    <div
      className={cn('flex items-center', {
        'h-8': !field.horizontal,
        'w-full': field.horizontal,
      })}
    >
      <Checkbox
        checked={Boolean(fieldProps.value)}
        onCheckedChange={(checked) => fieldProps.onChange?.(Boolean(checked))}
        disabled={field.disabled}
      />
    </div>
  ),
  [FormFieldType.Switch]: (field, fieldProps) => (
    <Switch
      checked={fieldProps.value as boolean}
      onCheckedChange={(checked) => fieldProps.onChange?.(checked)}
      disabled={field.disabled}
    />
  ),
  [FormFieldType.Tag]: (field, fieldProps) => (
    <div className="w-full">
      <EditTag {...fieldProps} disabled={field.disabled} />
    </div>
  ),
  [FormFieldType.Segmented]: (field, fieldProps) => (
    <Segmented
      {...wrapOnChange(field, fieldProps)}
      options={field.options || []}
      className="w-full"
      itemClassName="flex-1 justify-center"
      disabled={field.disabled}
    />
  ),
  // Custom without an explicit `field.render` falls back to a plain input,
  // matching the original switch's default-branch behavior.
  [FormFieldType.Custom]: renderInputField,
};

// Render form fields
export const RenderField = ({
  field,
  labelClassName,
}: {
  field: FormFieldConfig;
  labelClassName?: string;
}) => {
  const itemLabelClassName = labelClassName || field.labelClassName;

  // Custom render path: bypasses the registry entirely.
  if (field.render) {
    if (field.type === FormFieldType.Custom && field.hideLabel) {
      return (
        <div className="w-full">
          {field.render({} as ControllerRenderProps)}
        </div>
      );
    }
    return (
      <RAGFlowFormItem {...field} labelClassName={itemLabelClassName}>
        {(fieldProps) => (
          <div className="w-full">
            {field.render?.(wrapOnChange(field, fieldProps))}
          </div>
        )}
      </RAGFlowFormItem>
    );
  }

  const renderControl = fieldControlRenderers[field.type];

  return (
    <RAGFlowFormItem {...field} labelClassName={itemLabelClassName}>
      {(fieldProps) => renderControl(field, fieldProps)}
    </RAGFlowFormItem>
  );
};

// Dynamic form component
// eslint-disable-next-line react/display-name
const DynamicFormRoot = forwardRef(
  <T extends FieldValues>(
    {
      fields: originFields,
      onSubmit,
      className = '',
      children,
      defaultValues: formDefaultValues = {} as DefaultValues<T>,
      labelClassName,
    }: DynamicFormProps<T>,
    ref: React.Ref<any>,
  ) => {
    // Local state is required so that onFieldType / onFieldUpdate (ref API)
    // can mutate fields at runtime. We sync with originFields via useEffect.
    const [fields, setFields] = useState<FormFieldConfig[]>(originFields);

    useEffect(() => {
      setFields(originFields);
    }, [originFields]);

    const fieldDefaults = useMemo(
      () => generateDefaultValues(fields),
      [fields],
    );

    const mergedDefaults = useMemo<DefaultValues<T>>(
      () => ({ ...fieldDefaults, ...formDefaultValues }),
      [fieldDefaults, formDefaultValues],
    );

    const filterActiveValues = useCallback(
      (allValues: any) => {
        const filteredValues: any = {};

        fields.forEach((field) => {
          if (
            !field.shouldRender ||
            (field.shouldRender(allValues) &&
              field.name?.indexOf(FilterFormField) < 0)
          ) {
            const value = getNestedValue(allValues, field.name);
            if (value !== undefined) {
              setNestedValue(filteredValues, field.name, value);
            }
          }
        });

        return filteredValues;
      },
      [fields],
    );

    // Initialize form. The resolver closes over `fields` via the ref to the
    // latest value; react-hook-form stores resolvers in a ref and uses the
    // most recent one on each validation pass.
    const fieldsRef = useRef(fields);
    fieldsRef.current = fields;

    const form = useForm<T>({
      resolver: async (data, context, options) => {
        const currentFields = fieldsRef.current;
        const activeFields = currentFields.filter(
          (field) => !field.shouldRender || field.shouldRender(data),
        );
        const activeSchema = generateSchema(activeFields);
        const zodResult = await zodResolver(activeSchema)(
          data,
          context,
          options,
        );

        const combinedErrors: Record<string, any> = { ...zodResult.errors };

        for (const field of currentFields) {
          if (
            field.customValidate &&
            (!field.shouldRender || field.shouldRender(data))
          ) {
            const value = getNestedValue(data, field.name);
            const isEmpty =
              value === undefined ||
              value === '' ||
              (Array.isArray(value) && value.length === 0);
            // Skip custom validation on empty optional fields to avoid
            // running async checks against uninitialized values.
            if (isEmpty && !field.required) continue;

            try {
              const result = await field.customValidate(value, data);
              if (typeof result === 'string') {
                combinedErrors[field.name] = {
                  type: 'custom',
                  message: result,
                };
              } else if (result === false) {
                combinedErrors[field.name] = {
                  type: 'custom',
                  message:
                    field.validation?.message || `${field.label} is invalid`,
                };
              }
            } catch (error) {
              combinedErrors[field.name] = {
                type: 'custom',
                message:
                  error instanceof Error ? error.message : 'Validation failed',
              };
            }
          }
        }

        for (const key in combinedErrors) {
          if (Array.isArray(combinedErrors[key])) {
            combinedErrors[key] = combinedErrors[key][0];
          }
        }

        return {
          values: Object.keys(combinedErrors).length ? {} : data,
          errors: combinedErrors,
        } as any;
      },
      defaultValues: mergedDefaults,
    });

    // Watch field dependencies to re-validate downstream fields
    useEffect(() => {
      const dependencyMap: Record<string, string[]> = {};
      fields.forEach((field) => {
        if (field.dependencies && field.dependencies.length > 0) {
          field.dependencies.forEach((dep) => {
            if (!dependencyMap[dep]) {
              dependencyMap[dep] = [];
            }
            dependencyMap[dep].push(field.name);
          });
        }
      });

      const subscriptions = Object.keys(dependencyMap).map((depField) =>
        form.watch((_values: any, { name }) => {
          if (name === depField && dependencyMap[depField]) {
            dependencyMap[depField].forEach((dependentField) => {
              form.trigger(dependentField as any);
            });
          }
        }),
      );

      return () => {
        subscriptions.forEach((sub) => sub.unsubscribe?.());
      };
    }, [fields, form]);

    // Reset the form when formDefaultValues reference changes.
    // Using a ref to track the previous reference prevents re-running the
    // reset when `fields` toggles reference (which happens whenever the
    // parent re-renders without memoizing the array).
    const prevFormDefaultValuesRef = useRef<DefaultValues<T> | null>(null);
    useEffect(() => {
      if (
        formDefaultValues &&
        Object.keys(formDefaultValues).length > 0 &&
        prevFormDefaultValuesRef.current !== formDefaultValues
      ) {
        form.reset({ ...fieldDefaults, ...formDefaultValues });
        prevFormDefaultValuesRef.current = formDefaultValues;
      }
    }, [form, formDefaultValues, fieldDefaults]);

    useImperativeHandle(
      ref,
      () => ({
        form,
        submit: () => {
          form.handleSubmit((values) => {
            onSubmit(filterActiveValues(values) as T);
          })();
        },
        isDirty: () => form.formState.isDirty,
        getValues: form.getValues,
        reset: (values?: T) => {
          if (values) {
            form.reset(values);
          } else {
            form.reset();
          }
        },
        setError: form.setError,
        clearErrors: form.clearErrors,
        trigger: form.trigger,
        filterActiveValues,
        watch: (field: string, callback: (value: any) => void) => {
          const { unsubscribe } = form.watch((values: any) => {
            if (values && values[field] !== undefined) {
              callback(values[field]);
            }
          });
          return unsubscribe;
        },
        watchDirty: (callback: (isDirty: boolean, values: any) => void) => {
          const { unsubscribe } = form.watch((values: any) => {
            callback(form.formState.isDirty, values);
          });
          return unsubscribe;
        },
        onFieldUpdate: (
          fieldName: string,
          updatedField: Partial<FormFieldConfig>,
        ) => {
          setFields((prev) =>
            prev.map((field) =>
              field.name === fieldName ? { ...field, ...updatedField } : field,
            ),
          );
        },
        updateFieldType: (fieldName: string, newType: FormFieldType) => {
          setFields((prev) =>
            prev.map((field) =>
              field.name === fieldName ? { ...field, type: newType } : field,
            ),
          );
        },
      }),
      [form, onSubmit, filterActiveValues],
    );

    // Subscribe to form values via useWatch so that shouldRender checks
    // re-evaluate only when the watched values actually change.
    const formValues = useWatch({ control: form.control }) as any;

    const ctxValue = useMemo(
      () => ({ filterActiveValues }),
      [filterActiveValues],
    );

    return (
      <Form {...form}>
        <DynamicFormContext.Provider value={ctxValue}>
          <form
            className={`space-y-6 ${className}`}
            onSubmit={(e) => {
              e.preventDefault();
              form.handleSubmit((values) => {
                onSubmit(filterActiveValues(values) as T);
              })(e);
            }}
          >
            {fields.map((field) => {
              const shouldShow = field.shouldRender
                ? field.shouldRender(formValues)
                : true;
              return (
                <div
                  key={field.name}
                  className={cn({ hidden: field.hidden || !shouldShow })}
                >
                  <RenderField field={field} labelClassName={labelClassName} />
                </div>
              );
            })}
            {children}
          </form>
        </DynamicFormContext.Provider>
      </Form>
    );
  },
) as <T extends FieldValues>(
  props: DynamicFormProps<T> & { ref?: React.Ref<DynamicFormRef> },
) => React.ReactElement;

(DynamicFormRoot as any).displayName = 'DynamicFormRoot';

const DynamicForm = {
  Root: DynamicFormRoot,
  SavingButton: ({
    submitLoading,
    buttonText,
    submitFunc,
  }: {
    submitLoading?: boolean;
    buttonText?: string;
    submitFunc?: (values: FieldValues) => void;
  }) => {
    const form = useFormContext();
    const ctx = useContext(DynamicFormContext);

    const handleClick = async () => {
      try {
        const beValid = await form.trigger();
        if (beValid && submitFunc) {
          form.handleSubmit(async (values) => {
            const filtered = ctx?.filterActiveValues
              ? ctx.filterActiveValues(values)
              : values;
            submitFunc(filtered);
          })();
        }
      } catch (e) {
        // Surface to the console without leaking through a stray log in
        // production paths; callers handle submit errors via submitFunc.
        // eslint-disable-next-line no-console
        console.error(e);
      }
    };

    return (
      <button
        type="button"
        disabled={submitLoading}
        onClick={handleClick}
        className={cn(
          'px-2 py-1 bg-primary text-primary-foreground rounded-md hover:bg-primary/90',
        )}
      >
        {submitLoading && (
          <Loader className="inline-block mr-2 h-4 w-4 animate-spin" />
        )}
        {buttonText ?? t('modal.okText')}
      </button>
    );
  },

  CancelButton: ({
    handleCancel,
    cancelText,
  }: {
    handleCancel: () => void;
    cancelText?: string;
  }) => {
    return (
      <button
        type="button"
        onClick={handleCancel}
        className="px-2 py-1 border border-border-button rounded-md text-text-secondary hover:bg-bg-card hover:text-primary"
      >
        {cancelText ?? t('modal.cancelText')}
      </button>
    );
  },
};

(DynamicForm.Root as unknown as { displayName?: string }).displayName =
  'DynamicFormRoot';

export { DynamicForm };
