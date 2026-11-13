import { cn } from '@/lib/utils';
import { ReactNode, SVGProps } from 'react';

type EeIconDefinition = {
  content: ReactNode;
  viewBox?: string;
};

const DefaultViewBox = '0 0 1024 1024';

// Keeps the registry keys literal — `EeIconName` stays a union of the icon
// names — while every entry is typed as a full icon definition.
const defineIcon = (icon: EeIconDefinition): EeIconDefinition => icon;

/**
 * Icons that only exist in the enterprise edition.
 *
 * `public/iconfont.js` is kept byte-identical to open source, so enterprise
 * glyphs cannot be appended to the shared sprite. They are declared inline in
 * this registry instead, and every one of them renders through `EeIcon`.
 *
 * To add an enterprise icon: append an entry here and use
 * `<EeIcon name="..." />` at the call site. No per-icon component is needed.
 */
const eeIconRegistry = {
  // Customer-service glyph behind the enterprise tickets link.
  kefu: defineIcon({
    content: (
      <path d="M533.333333 170.666667a234.666667 234.666667 0 0 1 233.728 256h-42.922666a192 192 0 1 0-200.96 170.368l10.154666 0.298666c38.357333 0 74.069333-11.264 104.064-30.634666l34.005334 28.416c-8.832 6.4-18.133333 12.245333-27.818667 17.408a405.504 405.504 0 0 1 294.528 368.853333h-42.709333a362.666667 362.666667 0 0 0-351.146667-341.205333l-12.032-0.213334-9.813333 0.213334A362.666667 362.666667 0 0 0 171.306667 981.333333H128.554667c9.130667-176.213333 130.816-322.730667 294.570666-368.896A234.666667 234.666667 0 0 1 533.333333 170.666667z m0-85.333334a320 320 0 0 1 319.317334 341.333334H896v128h-42.666667v-59.306667a233.557333 233.557333 0 0 1-128 37.973333l-5.632-0.128a42.666667 42.666667 0 1 1-0.213333-42.752l2.901333 0.170667 2.944 0.042667a191.146667 191.146667 0 0 0 87.978667-21.333334H810.666667V405.333333a277.333333 277.333333 0 1 0-554.666667 0V469.333333l-42.666667-0.042666V554.666667H170.666667v-128h43.349333A320 320 0 0 1 533.333333 85.333333z" />
    ),
  }),
};

export type EeIconName = keyof typeof eeIconRegistry;

interface EeIconProps extends Omit<SVGProps<SVGSVGElement>, 'children'> {
  name: EeIconName;
}

export function EeIcon({
  name,
  className,
  viewBox,
  style,
  ...restProps
}: EeIconProps) {
  const icon = eeIconRegistry[name];

  return (
    <svg
      viewBox={viewBox ?? icon.viewBox ?? DefaultViewBox}
      className={cn('size-4', className)}
      // Matches IconFontFill: the glyph follows the surrounding text color.
      style={{ fill: 'currentColor', ...style }}
      {...restProps}
    >
      {icon.content}
    </svg>
  );
}
