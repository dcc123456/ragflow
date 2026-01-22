import { Card, CardContent } from '@/components/ui/card';

import { cn } from '@/lib/utils';

type Props = React.HTMLAttributes<HTMLElement>;

export default function AuthCard(props: Props) {
  const { className, children, title, ...restProps } = props;

  return (
    <article className={cn('w-[540px]', className)} {...restProps}>
      <header className="mb-8 text-center">
        <h2 className="font-semibold text-text-primary">{title}</h2>
      </header>

      <Card className="bg-bg-component shadow-xl border-0.5 border-border-button">
        <CardContent className="p-10">{children}</CardContent>
      </Card>
    </article>
  );
}
