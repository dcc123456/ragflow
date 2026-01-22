import { Card, CardContent } from '@/components/ui/card';
import { Spin } from '@/components/ui/spin';

import { cn } from '@/lib/utils';

type Props = React.HTMLAttributes<HTMLElement> & {
  spinning?: boolean;
};

export default function AuthCard(props: Props) {
  const { className, children, title, spinning = false, ...restProps } = props;

  return (
    <article className={cn('w-[540px]', className)} {...restProps}>
      <header className="mb-8 text-center">
        <h2 className="font-semibold text-text-primary">{title}</h2>
      </header>

      <Card className="bg-bg-component shadow-xl border-0.5 border-border-button overflow-hidden">
        <Spin spinning={spinning} size="large" className="after:bg-transparent">
          <CardContent className="p-10">{children}</CardContent>
        </Spin>
      </Card>
    </article>
  );
}
