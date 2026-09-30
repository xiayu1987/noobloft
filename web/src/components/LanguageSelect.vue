<!--
Copyright (c) 2026 xiayu
Contact: 126240622+xiayu1987@users.noreply.github.com
SPDX-License-Identifier: MIT
-->

<script setup lang="ts">
import { locale, setLanguage, type Language } from '../i18n/index'
function change(event: Event) { setLanguage((event.target as HTMLSelectElement).value as Language) }
</script>
<template>
  <label class="language-selector">
    <span>语言 / Language</span>
    <select aria-label="语言 / Language" :value="locale" @change="change">
      <option value="zh-CN">简体中文</option>
      <option value="en">English</option>
    </select>
  </label>
</template>
<style scoped>
.language-selector { position: fixed; top: 14px; right: 18px; z-index: 1000; display: flex; align-items: center; gap: 8px; padding: 6px 9px; border: 1px solid #d7e1ee; border-radius: 7px; background: #ffffffeb; color: #526177; font-size: 11px; box-shadow: 0 4px 14px #1a294014; backdrop-filter: blur(10px); }
select { font: inherit; padding: 3px 5px; border: 1px solid #cbd7e5; border-radius: 5px; color: #26364d; background: #fff; }
@media (max-width: 600px) { .language-selector { top: 76px; right: 12px; } }
</style>
