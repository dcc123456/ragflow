import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
} from '@/components/ui/form';
import { Switch } from '@/components/ui/switch';
import { cn } from '@/lib/utils';
import { MouseEventHandler, ReactNode, useCallback } from 'react';
import { useFormContext } from 'react-hook-form';

interface SwitchFormItemProps {
  name: string;
  label: ReactNode;
  vertical?: boolean;
  tooltip?: ReactNode;
  shouldStopPropagation?: boolean;
  disabled?: boolean;
}

export function SwitchFormField({
  label,
  name,
  vertical = true,
  tooltip,
  shouldStopPropagation = false,
  disabled = false,
}: SwitchFormItemProps) {
  const form = useFormContext();

  const handleClick: MouseEventHandler = useCallback(
    (e) => {
      if (shouldStopPropagation) {
        e.stopPropagation();
      }
    },
    [shouldStopPropagation],
  );

  return (
    <FormField
      control={form.control}
      name={name}
      render={({ field }) => (
        <FormItem
          className={cn('flex', {
            'gap-2': vertical,
            'flex-col': vertical,
            'justify-between': !vertical,
          })}
          onClick={handleClick}
        >
          <FormLabel tooltip={tooltip}>{label}</FormLabel>
          <FormControl>
            <Switch
              checked={field.value}
              disabled={disabled}
              onCheckedChange={field.onChange}
              aria-readonly
              className="!m-0"
            />
          </FormControl>
        </FormItem>
      )}
    />
  );
}
