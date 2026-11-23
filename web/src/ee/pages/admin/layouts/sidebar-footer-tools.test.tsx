import { ThemeEnum } from '@/constants/common';
import { fireEvent, render, screen } from '@testing-library/react';
import { AdminSidebarFooterTools } from './sidebar-footer-tools';

const mockSetTheme = jest.fn();
const mockChangeLanguage = jest.fn();
let mockTheme = ThemeEnum.Dark;

jest.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
    i18n: { language: 'en', resolvedLanguage: 'en' },
  }),
}));

jest.mock('@/components/theme-provider', () => ({
  useTheme: () => ({ theme: mockTheme, setTheme: mockSetTheme }),
}));

jest.mock('@/locales/config', () => ({
  changeLanguageAsync: (lng: string) => mockChangeLanguage(lng),
  supportedLanguages: [
    { code: 'en', displayName: 'English' },
    { code: 'zh-Hans', displayName: '简体中文' },
  ],
}));

describe('AdminSidebarFooterTools', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockTheme = ThemeEnum.Dark;
  });

  it('renders the system version', () => {
    render(<AdminSidebarFooterTools version="v0.22.1" />);

    expect(screen.getByText('v0.22.1')).toBeInTheDocument();
  });

  it('shows the language short code of the active language', () => {
    render(<AdminSidebarFooterTools />);

    expect(
      screen.getByRole('button', { name: 'setting.language' }),
    ).toHaveTextContent('EN');
  });

  it.each([
    ['sidebar.systemTheme', ThemeEnum.System],
    ['sidebar.lightTheme', ThemeEnum.Light],
    ['sidebar.darkTheme', ThemeEnum.Dark],
  ])('selects %s', (label, theme) => {
    render(<AdminSidebarFooterTools />);

    fireEvent.click(screen.getByRole('radio', { name: label }));

    expect(mockSetTheme).toHaveBeenCalledWith(theme);
  });

  it('marks the active theme as checked', () => {
    mockTheme = ThemeEnum.System;
    render(<AdminSidebarFooterTools />);

    expect(
      screen.getByRole('radio', { name: 'sidebar.systemTheme' }),
    ).toHaveAttribute('aria-checked', 'true');
    expect(
      screen.getByRole('radio', { name: 'sidebar.darkTheme' }),
    ).toHaveAttribute('aria-checked', 'false');
  });
});
