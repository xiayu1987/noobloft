// Copyright (c) 2026 xiayu
// Contact: 126240622+xiayu1987@users.noreply.github.com
// SPDX-License-Identifier: MIT

import { createApp, defineComponent, h } from 'vue'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import App from './App.vue'
import './styles/main.css'
import { i18n, elementLocale } from './i18n/index'
import { ElConfigProvider } from 'element-plus'
const Root = defineComponent(() => () => h(ElConfigProvider, { locale: elementLocale.value }, () => h(App)))
createApp(Root).use(i18n).use(ElementPlus).mount('#app')
