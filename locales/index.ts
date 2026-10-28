import en from './en.json';
import zh from './zh-CN.json';
export type Locale = 'en' | 'zh-CN';
export type MessageKey = keyof typeof en;
export const normalizeLocale = (tag: string): Locale => /^zh(?:$|-cn$|-hans)/i.test(tag.replaceAll('_', '-')) ? 'zh-CN' : 'en';
export function translate(locale: string, key: MessageKey): string {
  return (normalizeLocale(locale) === 'zh-CN' ? zh : en)[key] ?? en[key];
}
