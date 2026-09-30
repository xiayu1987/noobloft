<!--
Copyright (c) 2026 xiayu
Contact: 126240622+xiayu1987@users.noreply.github.com
SPDX-License-Identifier: MIT
-->

<script setup lang="ts">
import { t, locale } from '../../i18n/index'

import { computed, ref } from 'vue'
const props = defineProps<{ config: any }>()
const emit = defineEmits<{ save: [value: any] }>()
const c = ref(JSON.parse(JSON.stringify(props.config)))
const section = ref('provider')
const lists: Record<string, string[]> = { network: ['listenAddrs','staticPeers','announceAddrs'], relay: ['staticRelays','allowedPeers'], provider: ['advertiseModels'] }
const labels = computed<Record<string,string>>(() => ({ enabled:t('common.enable'), maxConcurrent:t('settings.maxConcurrent'), advertiseModels:t('settings.advertiseModels'), listenAddrs:t('settings.listenAddrs'), staticPeers:t('settings.staticPeers'), announceAddrs:t('settings.announceAddrs'), enableMdns:t('settings.enableMdns'), enableDht:t('settings.enableDht'), enableHolePunch:t('settings.enableHolePunch'), dhtMode:t('settings.dhtMode'), lowWater:t('settings.lowWater'), highWater:t('settings.highWater'), useRelays:t('settings.useRelays'), staticRelays:t('settings.staticRelays'), allowedPeers:t('settings.allowedPeers'), maxCircuits:t('settings.maxCircuits'), probeEnabled:t('settings.probeEnabled'), probeIntervalSec:t('settings.probeIntervalSec'), probeCooldownSec:t('settings.probeCooldownSec'), bindAddr:t('settings.bindAddr'), requestTimeoutSec:t('settings.requestTimeoutSec') }))
const fields: Record<string,string[]> = {provider:['enabled','maxConcurrent','advertiseModels'],network:['listenAddrs','staticPeers','announceAddrs','enableMdns','enableDht','dhtMode','enableHolePunch','lowWater','highWater'],relay:['enabled','useRelays','staticRelays','allowedPeers','maxCircuits'],reputation:['enabled','probeEnabled','probeIntervalSec','probeCooldownSec'],gateway:['bindAddr','requestTimeoutSec']}
function save(){const {network,relay,reputation,backends,provider,gateway}=c.value;emit('save',{network,relay,reputation,backends,provider:{enabled:provider.enabled,maxConcurrent:provider.maxConcurrent,advertiseModels:provider.advertiseModels},gateway:{bindAddr:gateway.bindAddr,requestTimeoutSec:gateway.requestTimeoutSec}})}
</script>
<template>
  <el-alert :title="t('settings.restartHint')" type="info" :closable="false" />
  <el-tabs v-model="section">
    <el-tab-pane v-for="(label,key) in {provider:t('config.modelService'),network:t('settings.network'),relay:t('settings.relay'),backends:t('settings.backends'),reputation:t('settings.reputation'),gateway:t('settings.gateway')}" :key="key" :label="label" :name="key" />
  </el-tabs>
  <el-form label-position="top" @submit.prevent="save">
    <template v-if="section !== 'backends'">
      <el-form-item v-for="key in fields[section]" :key="section+key" :label="labels[key]">
        <el-input v-if="lists[section]?.includes(key)" :model-value="(c[section][key] || []).join('\n')" type="textarea" :rows="3" :placeholder="t('settings.onePerLine')" @update:model-value="c[section][key] = $event.split('\n').map((v:string)=>v.trim()).filter(Boolean)" />
        <el-switch v-else-if="typeof c[section][key] === 'boolean'" v-model="c[section][key]" />
        <el-input-number v-else-if="typeof c[section][key] === 'number'" v-model="c[section][key]" :min="1" :max="1000000" />
        <el-select v-else-if="key === 'dhtMode'" v-model="c[section][key]"><el-option v-for="mode in ['auto','client','server']" :key="mode" :value="mode" /></el-select>
        <el-input v-else v-model="c[section][key]" />
      </el-form-item>
    </template>
    <template v-else>
      <el-card v-for="(b,i) in c.backends" :key="i" class="backend-card" shadow="never">
        <el-form-item :label="t('common.name')"><el-input v-model="b.name" /></el-form-item>
        <el-form-item :label="t('common.type')"><el-select v-model="b.kind"><el-option value="ollama" /><el-option value="openai-compatible" /></el-select></el-form-item>
        <el-form-item :label="t('backend.serviceAddress')"><el-input v-model="b.baseUrl" /></el-form-item>
        <el-form-item :label="t('backend.apiKey')"><el-input v-model="b.apiKey" type="password" show-password autocomplete="off" /></el-form-item>
        <el-switch v-model="b.enabled" :active-text="t('common.enable')" />
        <el-button type="danger" text @click="c.backends.splice(i,1)">{{ t('backend.remove') }}</el-button>
      </el-card>
      <el-button @click="c.backends.push({name:'',kind:'ollama',baseUrl:'http://127.0.0.1:11434',enabled:true,apiKey:''})">{{ t('backend.add') }}</el-button>
    </template>
    <div class="form-footer"><el-button type="primary" native-type="submit">{{ t('settings.save') }}</el-button></div>
  </el-form>
</template>
