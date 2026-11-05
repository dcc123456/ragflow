import type { LucideProps } from 'lucide-react';
import { forwardRef } from 'react';

const LucideCloudKey = forwardRef(function LucideCloudKey(
  props: LucideProps,
  ref: React.Ref<SVGSVGElement>,
) {
  return (
    <svg
      ref={ref}
      xmlns="http://www.w3.org/2000/svg"
      width="24"
      height="24"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      className="lucide lucide-cloud-upload size-[1em]"
      aria-hidden="true"
      {...props}
    >
      <path d="M4 14.899A7 7 0 1 1 15.71 8h1.79a4.5 4.5 0 0 1 2.5 8.242"></path>
      <circle cx="10" cy="19" r="2" />
      <path d="m16 13-4.5 4.5" />
      <path d="m15 14 1 1" />
    </svg>
  );
});

export { LucideCloudKey as CloudKey, LucideCloudKey };
