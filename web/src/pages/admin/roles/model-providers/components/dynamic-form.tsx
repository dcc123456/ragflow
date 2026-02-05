import { zodResolver } from '@hookform/resolvers/zod';
import { isEmpty, mapValues, noop, pickBy } from 'lodash';
import { useEffect, useMemo, useState } from 'react';
import {
  type ControllerRenderProps,
  DeepPartial,
  type DefaultValues,
  FieldErrors,
  type FieldValues,
  type Path,
  useForm,
  useFormContext,
} from 'react-hook-form';
import z from 'zod';

import { Checkbox } from '@/components/ui/checkbox';
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form';
import { Input } from '@/components/ui/input';
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group';
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';

import type {
  FieldConfig,
  FieldConfigTypeDisplay,
  OptionDefs,
} from './dynamic-form/FieldConfig';

import PasswordInput from '@/components/originui/password-input';
import { SelectWithSearch } from '@/components/originui/select-with-search';
import { Segmented } from '@/components/ui/segmented';

export type * from './dynamic-form/FieldConfig';

export type DynamicFormRootProps<T extends FieldValues> = {
  id?: string;
  fields: FieldConfig[];
  onSubmit: (data: T) => void;
  defaultValues?: DefaultValues<T> | (() => DefaultValues<T>);
  labelClassName?: string;
};

const normalizeOptions = (
  options: OptionDefs,
): { value: string; label: string }[] => {
  return options.map((o) =>
    typeof o === 'string'
      ? {
          value: o,
          label: o,
        }
      : {
          value: o.value,
          label: o.label ?? o.value,
        },
  );
};

function FormFieldItemRenderer<T extends FieldValues>({
  field,
  fieldConfig,
  labelClassName,
}: Pick<DynamicFormRootProps<T>, 'labelClassName'> & {
  field: ControllerRenderProps<T>;
  fieldConfig: Exclude<FieldConfig, FieldConfigTypeDisplay>;
}) {
  const element = (() => {
    switch (fieldConfig.type) {
      case 'text':
      case 'email':
      case 'number':
        return (
          <Input
            {...field}
            value={field.value ?? ''}
            className="w-full h-10"
            type={fieldConfig.type}
            onChange={(e) => {
              field.onChange?.(e.target.value);
            }}
            placeholder={fieldConfig.placeholder}
          />
        );
      case 'password':
        return (
          <PasswordInput
            {...field}
            value={field.value ?? ''}
            className="w-full h-10"
            type={fieldConfig.type}
          />
        );
      case 'checkbox':
        return (
          <Checkbox
            ref={field.ref}
            name={field.name}
            checked={field.value ?? false}
            onCheckedChange={field.onChange}
            onBlur={field.onBlur}
            disabled={field.disabled}
          />
        );
      case 'switch':
        return (
          <Switch
            ref={field.ref}
            name={field.name}
            checked={field.value ?? false}
            onCheckedChange={field.onChange}
            onBlur={field.onBlur}
            disabled={field.disabled}
          />
        );
      case 'radio-group':
        return (
          <RadioGroup
            ref={field.ref}
            name={field.name}
            value={field.value ?? ''}
            onValueChange={field.onChange}
            onBlur={field.onBlur}
            disabled={field.disabled}
          >
            {normalizeOptions(fieldConfig.options).map((option) => (
              <RadioGroupItem key={option.value} value={option.value}>
                {option.label}
              </RadioGroupItem>
            ))}
          </RadioGroup>
        );
      case 'select':
        return fieldConfig.searchable ? (
          <SelectWithSearch
            {...field}
            triggerClassName="w-full h-10"
            options={normalizeOptions(fieldConfig.options)}
            placeholder={fieldConfig.placeholder}
          />
        ) : (
          <Select
            name={field.name}
            value={field.value ?? ''}
            onValueChange={field.onChange}
            disabled={field.disabled}
          >
            <SelectTrigger
              className="w-full h-10"
              ref={field.ref}
              onBlur={field.onBlur}
              disabled={field.disabled}
            >
              <SelectValue placeholder={fieldConfig.placeholder} />
            </SelectTrigger>

            <SelectContent>
              <SelectGroup>
                {normalizeOptions(fieldConfig.options).map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        );
      case 'segmented':
        return (
          <Segmented
            className="h-10"
            options={normalizeOptions(fieldConfig.options)}
            value={field.value ?? ''}
            onChange={field.onChange}
            disabled={field.disabled}
          />
        );
      default:
        return null;
    }
  })();

  return (
    <FormItem className="relative">
      {fieldConfig.label && (
        <FormLabel
          className={labelClassName}
          required={fieldConfig.required}
          tooltip={fieldConfig.labelTooltip}
        >
          {fieldConfig.label}
        </FormLabel>
      )}

      <FormControl>{element}</FormControl>

      <FormMessage className="absolute !mt-1" />
    </FormItem>
  );
}

function DynamicFormField<T extends FieldValues>({
  fieldConfig,
  labelClassName,
}: {
  fieldConfig: FieldConfig;
  labelClassName?: string;
}) {
  const { shouldRender } = fieldConfig;

  const [render, setRender] = useState(true);
  const form = useFormContext<T>();
  const { watch } = form;

  useEffect(() => {
    if (shouldRender != null) {
      const watchCallback = (values: DeepPartial<T>) => {
        setRender(typeof shouldRender !== 'function' || shouldRender(values));
      };

      const { unsubscribe } = watch(watchCallback);
      watchCallback(form.getValues() as DeepPartial<T>);

      return unsubscribe;
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [shouldRender, watch]);

  if (!render) {
    return null;
  }

  if (fieldConfig.type === 'display') {
    return fieldConfig.Component ? (
      <fieldConfig.Component />
    ) : (
      <>{fieldConfig.element}</>
    );
  }

  return (
    <FormField
      control={form.control}
      name={fieldConfig.name as Path<T>}
      render={({ field }) => (
        <FormFieldItemRenderer
          field={field}
          fieldConfig={fieldConfig}
          labelClassName={labelClassName}
        />
      )}
    />
  );
}

function DynamicFormRoot<T extends FieldValues>({
  id,
  fields,
  defaultValues,
  labelClassName,
  onSubmit = noop,
}: DynamicFormRootProps<T>) {
  const schema = useMemo(() => {
    const fieldNameMap = Object.fromEntries(
      fields
        .filter((field) => field.type !== 'display')
        .map((field) => [field.name, field]),
    );
    const schema = mapValues(fieldNameMap, (field) => field.rules);

    return z.object(schema);
  }, [fields]);

  const form = useForm<T>({
    resolver: async (data, context, options) => {
      const result = await zodResolver(schema)(data, context, options);

      if (isEmpty(result.errors)) {
        return result;
      }

      const filteredErrors = pickBy(
        result.errors as FieldErrors,
        (value, key) => {
          // @ts-ignore
          const fieldConfig = fields.find((field) => field.name === key)!;
          return (
            typeof fieldConfig.shouldRender !== 'function' ||
            fieldConfig.shouldRender(data)
          );
        },
      );

      if (isEmpty(filteredErrors)) {
        return {
          errors: {},
          values: data,
        };
      }

      return {
        errors: filteredErrors,
        values: {},
      } as typeof result;
    },
    defaultValues: defaultValues
      ? typeof defaultValues === 'function'
        ? defaultValues()
        : defaultValues
      : {},
  });

  return (
    <Form {...form}>
      <form
        id={id}
        className="my-4 space-y-8"
        onSubmit={form.handleSubmit(onSubmit)}
      >
        {fields.map((config, index) => (
          <DynamicFormField
            key={config.type === 'display' ? index : config.name}
            fieldConfig={config}
            labelClassName={labelClassName}
          />
        ))}
      </form>
    </Form>
  );
}

const DynamicForm = {
  Root: DynamicFormRoot,
};

export default DynamicForm;
