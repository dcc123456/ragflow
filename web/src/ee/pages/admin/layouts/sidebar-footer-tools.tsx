import { useTheme } from '@/components/theme-provider';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { ThemeEnum } from '@/constants/common';
import { changeLanguageAsync, supportedLanguages } from '@/locales/config';
import { cn } from '@/lib/utils';
import {
  LucideChevronDown,
  LucideMonitor,
  LucideMoon,
  LucideSun,
} from 'lucide-react';
import { useCallback, useMemo } from 'react';
import { useTranslation } from 'react-i18next';

const themeOptions = [
  {
    value: ThemeEnum.System,
    icon: LucideMonitor,
    labelKey: 'sidebar.systemTheme',
  },
  { value: ThemeEnum.Light, icon: LucideSun, labelKey: 'sidebar.lightTheme' },
  { value: ThemeEnum.Dark, icon: LucideMoon, labelKey: 'sidebar.darkTheme' },
] as const;

type AdminSidebarFooterToolsProps = {
  version?: string;
};

export function AdminSidebarFooterTools({
  version,
}: AdminSidebarFooterToolsProps) {
  const { t, i18n } = useTranslation();
  const { theme, setTheme } = useTheme();

  const currentLanguage = useMemo(() => {
    const active = i18n.resolvedLanguage || i18n.language;

    return (
      supportedLanguages.find(({ code }) => code === active) ??
      supportedLanguages.find(({ code }) =>
        active?.startsWith(code.split('-')[0]),
      ) ??
      supportedLanguages[0]
    );
  }, [i18n.language, i18n.resolvedLanguage]);

  const handleLanguageChange = useCallback((code: string) => {
    changeLanguageAsync(code);
  }, []);

  return (
    <div className="flex items-center justify-between gap-2">
      <span className="min-w-0 truncate text-xs leading-none text-accent-primary">
        {version}
      </span>

      <div className="flex shrink-0 items-center gap-2">
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button
              type="button"
              aria-label={t('setting.language')}
              className={cn(
                'flex items-center gap-1 rounded-sm px-1 py-0.5',
                'text-xs font-medium text-text-secondary transition-colors',
                'hover:text-text-primary data-[state=open]:text-text-primary',
              )}
            >
              {currentLanguage.code.split('-')[0].toUpperCase()}
              <LucideChevronDown className="size-3" />
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent
            side="top"
            align="start"
            className="max-h-72 w-44 overflow-y-auto"
          >
            <DropdownMenuRadioGroup
              value={currentLanguage.code}
              onValueChange={handleLanguageChange}
            >
              {supportedLanguages.map(({ code, displayName }) => (
                <DropdownMenuRadioItem
                  key={code}
                  value={code}
                  className="cursor-pointer"
                >
                  {displayName}
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuContent>
        </DropdownMenu>

        <div
          role="radiogroup"
          aria-label={t('sidebar.theme')}
          className="flex items-center gap-0.5 rounded-full bg-bg-card p-0.5"
        >
          {themeOptions.map(({ value, icon: Icon, labelKey }) => {
            const label = t(labelKey);
            const checked = theme === value;

            return (
              <button
                key={value}
                type="button"
                role="radio"
                aria-checked={checked}
                aria-label={label}
                title={label}
                onClick={() => setTheme(value)}
                className={cn(
                  'flex size-6 items-center justify-center rounded-full transition-colors',
                  checked
                    ? 'border border-border-button bg-bg-base text-text-primary shadow-sm'
                    : 'text-text-secondary hover:text-text-primary',
                )}
              >
                <Icon className="size-3.5" />
              </button>
            );
          })}
        </div>
      </div>
    </div>
  );
}
