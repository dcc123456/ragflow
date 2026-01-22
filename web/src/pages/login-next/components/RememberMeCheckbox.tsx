import { forwardRef } from 'react';

import { Checkbox } from '@/components/ui/checkbox';
import { CheckboxProps } from '@radix-ui/react-checkbox';

import { useTranslate } from '@/hooks/common-hooks';
import { cn } from '@/lib/utils';

type Props = Omit<React.PropsWithChildren<CheckboxProps>, 'value'> & {
  value?: any;
};

const RememberMeCheckbox = forwardRef<React.ElementRef<typeof Checkbox>, Props>(
  function RememberMeCheckbox(props, ref) {
    const { children, className, ...restProps } = props;

    const { t } = useTranslate('login');

    return (
      <div className="inline-flex items-center gap-1.5">
        <Checkbox ref={ref} className={cn('peer', className)} {...restProps} />

        <span
          className={cn(
            'transition-colors text-text-disabled',
            'peer-hover:text-text-secondary peer-focus:text-text-secondary',
            'peer-checked:text-text-primary peer-aria-checked:text-text-primary peer-data-[state=checked]:text-text-primary',
          )}
        >
          {children || t('rememberMe')}
        </span>
      </div>
    );
  },
);

export default RememberMeCheckbox;
