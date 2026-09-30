<!--
Copyright (c) 2026 xiayu
Contact: 126240622+xiayu1987@users.noreply.github.com
SPDX-License-Identifier: MIT
-->

<script setup lang="ts">
import { t, locale } from '../../i18n/index'

import { computed, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api } from '../../api/client'
const props = defineProps<{ state: any }>()
const open = ref(false), busy = ref(false), source = ref('running')
const service = computed(() => props.state[source.value]?.provider.service)
function show() {
  source.value = props.state.running.provider.service ? 'running' : 'saved'
  open.value = true
}
async function download() {
  busy.value = true
  try {
    const document = await api(`publisher/export?source=${source.value}`)
    const url = URL.createObjectURL(new Blob([JSON.stringify(document, null, 2) + '\n'], { type: 'application/json' }))
    const link = documentCreate(url)
    link.click()
    link.remove()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
    ElMessage.success(t('export.completed'))
  } catch (e: any) { ElMessage.error(e.message) }
  finally { busy.value = false }
}
function documentCreate(url: string) {
  const link = document.createElement('a')
  link.href = url
  link.download = 'service.noobloft'
  document.body.appendChild(link)
  return link
}
</script>
<template>
  <div class="publish-banner">
    <div><div class="eyebrow">PUBLISH & CONNECT</div><h3>{{ t('export.heading') }}</h3><p>{{ t('export.intro') }}</p></div>
    <el-button type="primary" size="large" @click="show">{{ t('export.action') }}</el-button>
  </div>
  <el-drawer v-model="open" :title="t('export.title')" direction="rtl" size="var(--console-drawer-width)" destroy-on-close>
    <el-alert :title="t('export.accessHint')" type="info" :closable="false" />
    <el-form label-position="top" class="spaced">
      <el-form-item :label="t('export.version')"><el-radio-group v-model="source"><el-radio-button value="running">{{ t('config.running') }}</el-radio-button><el-radio-button value="saved">{{ t('config.saved') }}</el-radio-button></el-radio-group></el-form-item>
    </el-form>
    <el-alert v-if="source==='saved' && state.pendingRestart" :title="t('export.savedWarning')" type="warning" :closable="false" class="notice" />
    <template v-if="service">
      <el-descriptions :column="1" border>
        <el-descriptions-item :label="t('common.publisherId')"><code>{{ service.publisher }}</code></el-descriptions-item>
        <el-descriptions-item :label="t('common.serviceId')"><code>{{ service.id }}</code></el-descriptions-item>
        <el-descriptions-item :label="t('common.validUntil')">{{ new Date(service.expiresAt * 1000).toLocaleString(locale) }}</el-descriptions-item>
        <el-descriptions-item :label="t('signing.models')"><el-tag v-for="name in service.models" :key="name" class="model-tag">{{ name }}</el-tag></el-descriptions-item>
        <el-descriptions-item :label="t('export.peers')"><p v-for="id in service.peers" :key="id" class="mono">{{ id }}</p></el-descriptions-item>
        <el-descriptions-item :label="t('signing.addresses')"><p v-for="address in service.addresses" :key="address" class="mono">{{ address }}</p><span v-if="!service.addresses?.length">{{ t('export.noAddresses') }}</span></el-descriptions-item>
        <el-descriptions-item :label="t('signing.relays')"><p v-for="relay in service.relays" :key="relay" class="mono">{{ relay }}</p><span v-if="!service.relays?.length">{{ t('common.notDeclared') }}</span></el-descriptions-item>
      </el-descriptions>
      <el-alert v-if="service.expiresAt * 1000 <= Date.now()" :title="t('export.expired')" type="error" class="notice" :closable="false" />
      <p class="muted">{{ t('export.shareHint') }}</p>
    </template>
    <el-empty v-else :description="t('export.notInstalled')">
      <p class="muted">{{ t('export.installHint') }}</p>
    </el-empty>
    <template #footer><el-button @click="open=false">{{ t('common.close') }}</el-button><el-button type="primary" :loading="busy" :disabled="!service || service.expiresAt * 1000 <= Date.now()" @click="download">{{ t('export.download') }}</el-button></template>
  </el-drawer>
</template>
