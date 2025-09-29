import { Button } from '@/components/ui/button';
import { X } from 'lucide-react';
import { useState } from 'react';

export function TopMessage() {
  const [shown, setShown] = useState(true);

  const hide = () => {
    setShown(false);
  };

  return (
    shown && (
      <section className="fixed flex top-0  w-full bg-orange-200 justify-center items-center   p-1 rounded-md shadow-sm shadow-orange-100">
        <div className="w-fit">
          Please enter a valid email address. We will use the verification code
          to log in in the future. Email addresses that do not receive the
          verification code will not be able to log in.
        </div>
        <Button type={'button'} variant={'ghost'} onClick={hide}>
          <X />
        </Button>
      </section>
    )
  );
}
