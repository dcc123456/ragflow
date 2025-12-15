import i18n from 'i18next';
import LanguageDetector from 'i18next-browser-languagedetector';
import { initReactI18next } from 'react-i18next';

import { LanguageAbbreviation } from '@/constants/common';
import translation_de from './de';
import translation_en from './en';
import translation_es from './es';
import translation_fr from './fr';
import translation_id from './id';
import translation_it from './it';
import translation_ja from './ja';
import translation_pt_br from './pt-br';
import translation_ru from './ru';
import { createTranslationTable, flattenObject } from './until';
import translation_vi from './vi';
import translation_zh from './zh';
import translation_zh_traditional from './zh-traditional';

// private
import merge from 'lodash/merge';
import translation_en_private from './en-private';
import translation_zh_private from './zh-private';
import translation_zh_traditional_private from './zh-traditional-private';

const resources = {
  [LanguageAbbreviation.En]: merge(translation_en, translation_en_private),
  [LanguageAbbreviation.Zh]: merge(translation_zh, translation_zh_private),
  [LanguageAbbreviation.ZhTraditional]: merge(
    translation_zh_traditional,
    translation_zh_traditional_private,
  ),
  [LanguageAbbreviation.Id]: translation_id,
  [LanguageAbbreviation.Ja]: translation_ja,
  [LanguageAbbreviation.Es]: translation_es,
  [LanguageAbbreviation.Vi]: translation_vi,
  [LanguageAbbreviation.Ru]: translation_ru,
  [LanguageAbbreviation.PtBr]: translation_pt_br,
  [LanguageAbbreviation.De]: translation_de,
  [LanguageAbbreviation.Fr]: translation_fr,
  [LanguageAbbreviation.It]: translation_it,
};
const enFlattened = flattenObject(translation_en);
const viFlattened = flattenObject(translation_vi);
const ruFlattened = flattenObject(translation_ru);
const esFlattened = flattenObject(translation_es);
const zhFlattened = flattenObject(translation_zh);
const jaFlattened = flattenObject(translation_ja);
const pt_brFlattened = flattenObject(translation_pt_br);
const zh_traditionalFlattened = flattenObject(translation_zh_traditional);
const deFlattened = flattenObject(translation_de);
const frFlattened = flattenObject(translation_fr);
const itFlattened = flattenObject(translation_it);
export const translationTable = createTranslationTable(
  [
    enFlattened,
    viFlattened,
    ruFlattened,
    esFlattened,
    zhFlattened,
    zh_traditionalFlattened,
    jaFlattened,
    pt_brFlattened,
    deFlattened,
    frFlattened,
    itFlattened,
  ],
  [
    'English',
    'Vietnamese',
    'ru',
    'Spanish',
    'zh',
    'zh-TRADITIONAL',
    'ja',
    'pt-BR',
    'Deutsch',
    'French',
    'Italian',
  ],
);
i18n
  .use(initReactI18next)
  .use(LanguageDetector)
  .init({
    detection: {
      lookupLocalStorage: 'lng',
    },
    supportedLngs: Object.values(LanguageAbbreviation),
    resources,
    fallbackLng: 'en',
    interpolation: {
      escapeValue: false,
    },
  });

export default i18n;
