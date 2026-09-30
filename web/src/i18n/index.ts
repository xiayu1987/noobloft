// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

import { computed, watch } from 'vue'
import { createI18n } from 'vue-i18n'
import zhCN from 'element-plus/es/locale/lang/zh-cn'
import enUS from 'element-plus/es/locale/lang/en'
import catalogue from '../../../locales/en.json'
import chinese from '../../../locales/zh-CN.json'

export type Language = 'zh-CN' | 'en'
export const languageStorageKey = 'noobloft.language'
export function normalizeLanguage(value: string | null | undefined): Language {
  return /^en(?:[-_]|$)/i.test(value || '') ? 'en' : 'zh-CN'
}
function initialLanguage(): Language {
  try {
    const saved = localStorage.getItem(languageStorageKey)
    if (saved === 'zh-CN' || saved === 'en') return saved
  } catch {}
  return normalizeLanguage(navigator.language)
}
const literalMessages = (messages: Record<string, string>) => Object.fromEntries(
  Object.entries(messages).map(([key, text]) => [key, () => text]),
)
export const i18n = createI18n({
  legacy: false,
  locale: initialLanguage(),
  fallbackLocale: 'zh-CN',
  missingWarn: false,
  fallbackWarn: false,
  messageResolver: (messages, key) => (messages as Record<string, any>)[key] ?? null,
  messages: { 'zh-CN': literalMessages(chinese), en: literalMessages(catalogue) },
})
export const locale = i18n.global.locale
export const elementLocale = computed(() => locale.value === 'en' ? enUS : zhCN)
export type MessageKey = keyof typeof chinese
export function t(key: MessageKey): string {
  return i18n.global.t(key)
}
export function setLanguage(value: Language) {
  locale.value = value
}
watch(locale, (value) => {
  document.documentElement.lang = value
  document.title = i18n.global.t('app.documentTitle')
  try { localStorage.setItem(languageStorageKey, value) } catch {}
}, { immediate: true })
