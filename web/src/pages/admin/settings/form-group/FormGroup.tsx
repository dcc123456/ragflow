import { useId } from 'react';
import { useFormContext } from 'react-hook-form';
import z from 'zod';

import { cn } from '@/lib/utils';

export type FormGroupBase<
  S extends z.ZodObject<z.ZodRawShape> = z.ZodObject<z.ZodRawShape>,
> = React.FC<{ name?: string }> & {
  schema: S;
  defaultValues?: z.infer<S>;
  mapValuesFromData: (data: Record<string, any>) => Record<string, any>;
};

export type FormGroupProps<T extends z.ZodRawShape = z.ZodRawShape> = {
  title?: React.ReactNode;
  description?: React.ReactNode;
  name?: string;
  fieldsSchema?: z.ZodObject<T>;
  fields?: string[];
};

function FormGroup({
  children,
  title,
  description,

  name,

  fields,
  fieldsSchema,
}: React.PropsWithChildren<FormGroupProps>) {
  const form = useFormContext();
  const descriptionId = useId();

  return (
    <fieldset>
      {title && (
        <legend
          className="text-base font-bold"
          {...(description ? { 'aria-describedby': descriptionId } : {})}
        >
          {title}
        </legend>
      )}

      {description && (
        <p id={descriptionId} className="text-sm text-text-secondary">
          {description}
        </p>
      )}

      <div
        className={cn(
          'grid grid-cols-[10rem_auto] gap-x-4 gap-y-8',
          (title || description) && 'mt-8',
        )}
      >
        {children}

        {/* Maybe will finish someday... */}
        {/* {children || (fieldsSchema &&
          (Array.isArray(fields) ? fields : Object.keys(fieldsSchema.shape)).map((fieldName) => {
            const zodField = fieldsSchema.shape[fieldName];

            if (!zodField) return null;

            const formFieldName = name ? `${name}.${fieldName}` : fieldName;

            return (
              <FormField
                key={formFieldName}
                control={form.control}
                name={formFieldName}
                render={({ field }) => (
                  <FormItem className="space-y-0 contents">
                    <FormLabel
                      required={!zodField.isOptional()}
                      className="h-10 flex items-center"
                    >
                      {fieldName}
                    </FormLabel>

                    <FormControl>

                    </FormControl>
                  </FormItem>
                )}
              />
            );
        }))} */}
      </div>
    </fieldset>
  );
}

export default FormGroup;
