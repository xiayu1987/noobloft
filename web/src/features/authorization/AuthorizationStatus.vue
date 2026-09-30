<!--
Copyright (c) 2026 xiayu
Contact: 126240622+xiayu1987@users.noreply.github.com
SPDX-License-Identifier: MIT
-->

<script setup lang="ts">
import { t, locale } from '../../i18n/index'

import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { api } from '../../api/client'
const props = defineProps<{ state: any }>()
const emit = defineEmits<{ navigate: [key: string] }>()
const data = ref<any>(null), loading = ref(false), error = ref(''), assistant = ref(false)
const root = ref(''), file = ref('service.noobloft'), fileTouched = ref(false)
const kind = ref('service'), models = ref(''), peers = ref('')
const addresses = ref(''), addressesTouched = ref(false), relays = ref(''), subject = ref(''), revoked = ref('')
const previous = ref(''), ttl = ref('24h'), maxTokens = ref(1024), concurrency = ref(1), rpm = ref(30)
const FIELD = computed(() => ({
  root: { required: true, hint: t('signing.rootHint') },
  file: { required: true, def: t('signing.fileDefault'), hint: t('signing.fileHint') },
  ttl: { required: true, def: '24h', hint: t('signing.ttlHint') },
  peers: { required: true, def: t('node.selfPeerId'), hint: t('signing.peersHint') },
  models: { required: true, hint: t('signing.modelsHint') },
  addresses: { required: false, def: t('signing.addressDefault'), hint: t('signing.addressHint') },
  relays: { required: false, hint: t('signing.relayHint') },
  subject: { required: true, hint: t('signing.subjectHint') },
  revoked: { required: false, def: t('common.empty'), hint: t('signing.revokedHint') },
  previous: { required: false, hint: t('signing.previousHint') },
  'max-tokens': { required: true, def: '1024', hint: t('signing.tokensHint') },
  concurrency: { required: true, def: '1', hint: t('signing.concurrencyHint') },
  rpm: { required: true, def: '30', hint: t('signing.rpmHint') },
}))
const FILE_DEFAULT: Record<string, string> = { service: 'service.noobloft', grant: 'consumer-access.json', revocations: 'revocations-1.json' }
function joinPath(dir: string, name: string) {
  const base = dir.trim()
  if (!base) return name
  const sep = base.includes('\\') ? '\\' : '/'
  const trimmed = base.replace(/[\\/]+$/, '')
  return (trimmed || '') + sep + name
}
const defaultFile = computed(() => joinPath(root.value, FILE_DEFAULT[kind.value] || 'service.noobloft'))
watch(defaultFile, (next) => { if (!fileTouched.value) file.value = next }, { immediate: true })
const severityType: Record<string, string> = { blocked: 'danger', warning: 'warning', info: 'info' }
const statusMap = computed<Record<string, { label: string; type: string; hint: string }>>(() => ({
  active: { label: t('authorization.active'), type: 'success', hint: t('authorization.activeHint') },
  warning: { label: t('authorization.warning'), type: 'warning', hint: t('authorization.warningHint') },
  blocked: { label: t('authorization.blocked'), type: 'danger', hint: t('authorization.blockedHint') },
}))
const status = computed(() => statusMap.value[data.value?.status] || { label: t('common.unknown'), type: 'info', hint: '' })
const findings = computed(() => data.value?.findings || [])
const modelPool = computed(() => (data.value?.service?.models || []).filter((name: string) => name !== '*' && !name.includes('::')))
const canSubmit = computed(() => {
  if (!root.value.trim() || !file.value.trim() || !ttl.value.trim()) return false
  if (kind.value === 'grant') return subject.value.trim() !== '' && models.value.trim() !== ''
  if (kind.value === 'revocations') return true
  return models.value.trim() !== '' && peerList.value.length > 0
})
const peerList = computed(() => (peers.value.trim() || props.state?.peerId || '').split(/[\s,]+/).filter(Boolean))
const addressList = computed(() => addresses.value.split(/[\s,]+/).map((value) => value.trim()).filter(Boolean))
const relayList = computed(() => relays.value.split(/[\s,]+/).map((value) => value.trim()).filter(Boolean))
const selfId = computed(() => props.state?.peerId || '')
const runningAddrs = computed<string[]>(() => props.state?.running?.network?.announceAddrs || [])
const savedAddrs = computed<string[]>(() => props.state?.saved?.network?.announceAddrs || [])
const announceAddrs = computed<string[]>(() => (runningAddrs.value.length ? runningAddrs.value : savedAddrs.value))
const announceSource = computed(() => (runningAddrs.value.length ? t('config.running') : savedAddrs.value.length ? t('config.saved') : ''))
const prefillAddresses = computed(() => announceAddrs.value.map((addr) => {
  if (/\/p2p\//.test(addr)) return addr
  return selfId.value ? addr + '/p2p/' + selfId.value : addr
}).join('\n'))
function quote(value: string) {
  return /[\s"&|<>^]/.test(value) ? '"' + value + '"' : value
}
function flags(pairs: [string, string | number][]) {
  const kept = pairs.filter(([, value]) => String(value).trim() !== '')
  return kept.length ? ' ' + kept.map(([flag, value]) => `-${flag} ${quote(String(value))}`).join(' ') : ''
}
const command = computed(() => {
  if (kind.value === 'grant') {
    const modelList = models.value.trim() || modelPool.value.join(',')
    return 'noobloftd publisher grant' + flags([['root', root.value], ['file', file.value], ['subject', subject.value], ['models', modelList], ['ttl', ttl.value], ['max-tokens', maxTokens.value], ['concurrency', concurrency.value], ['rpm', rpm.value]])
  }
  if (kind.value === 'revocations') {
    return 'noobloftd publisher revocations' + flags([['root', root.value], ['file', file.value], ['revoked', revoked.value], ['previous', previous.value], ['ttl', ttl.value]])
  }
  return 'noobloftd publisher service' + flags([['root', root.value], ['file', file.value], ['peers', peerList.value.join(',')], ['models', models.value], ['addresses', addressList.value.join(',')], ['relays', relayList.value.join(',')], ['ttl', ttl.value]])
})
async function load() {
  loading.value = true; error.value = ''
  try { data.value = await api('authorization') }
  catch (e: any) { error.value = e.message }
  finally { loading.value = false }
}
function openAssistant(next: string) {
  kind.value = next
  if (!fileTouched.value) file.value = defaultFile.value
  if (next === 'service' && !peers.value) peers.value = selfId.value
  if (next === 'service' && !addressesTouched.value) addresses.value = prefillAddresses.value
  if (next === 'grant' && !models.value) models.value = modelPool.value.join(',')
  assistant.value = true
}
defineExpose({ openAssistant })
function resetAddresses() {
  addresses.value = prefillAddresses.value
  addressesTouched.value = false
}
async function act(finding: any) {
  if (finding.code === 'pending_restart' || finding.code === 'provider_disabled' || finding.code === 'advertised_not_delegated' || finding.code === 'no_served_models' || finding.code === 'no_local_models') {
    emit('navigate', 'settings')
    return
  }
  if (finding.code === 'service_missing' || finding.code === 'service_expiring' || finding.code === 'service_invalid' || finding.code === 'revoked_service' || finding.code === 'node_not_delegated') {
    openAssistant('service')
    return
  }
  if (finding.code === 'revocations_expiring' || finding.code === 'revocations_unavailable' || finding.code === 'revocations_invalid' || finding.code === 'revoked_node' || finding.code === 'revocations_missing') {
    openAssistant('revocations')
    return
  }
  await load()
}
function fallbackCopy(value: string) {
  const field = document.createElement('textarea')
  field.value = value
  field.setAttribute('readonly', 'readonly')
  field.style.position = 'fixed'
  field.style.opacity = '0'
  document.body.appendChild(field)
  field.select()
  const copied = document.execCommand('copy')
  document.body.removeChild(field)
  return copied
}
async function copy() {
  try {
    try { await navigator.clipboard.writeText(command.value) }
    catch { if (!fallbackCopy(command.value)) throw new Error(t('clipboard.unavailable')) }
    ElMessage.success(t('clipboard.copied'))
  } catch (e: any) {
    ElMessage.error(t('clipboard.failed') + e.message)
  }
}
onMounted(load)
watch(locale, load)
function label(key: string, name: string) {
  const meta = (FIELD.value as any)[key]
  const mark = meta.required ? t('common.required') : t('common.optional')
  const def = meta.def ? t('common.defaultPrefix') + meta.def : ''
  return name + t('common.openParen') + '-' + key + t('common.fieldSeparator') + mark + def + t('common.closeParen')
}
function hint(key: string) {
  return (FIELD.value as any)[key].hint
}
</script>
<template>
  <el-card v-loading="loading" class="spaced">
    <div class="toolbar">
      <div>
        <el-tag :type="(status.type as any)" size="large" effect="dark" data-test="authorization-status">{{ status.label }}</el-tag>
        <p class="muted">{{ status.hint }}</p>
      </div>
      <div class="actions">
        <el-button @click="openAssistant('service')">{{ t('signing.title') }}</el-button>
        <el-button text @click="load">{{ t('authorization.recheck') }}</el-button>
      </div>
    </div>
    <el-alert v-if="error" :title="error" type="error" :closable="false" class="spaced" />
    <template v-if="data">
      <el-descriptions :column="3" border class="spaced">
        <el-descriptions-item :label="t('authorization.node')">{{ data.service.delegates ? t('authorization.authorized') : t('authorization.unauthorized') }}</el-descriptions-item>
        <el-descriptions-item :label="t('authorization.serviceExpires')">{{ data.service.expiresAt ? new Date(data.service.expiresAt * 1000).toLocaleString(locale) : t('common.notInstalled') }}</el-descriptions-item>
        <el-descriptions-item :label="t('revocations.sequence')">{{ data.revocations.sequence || '—' }}</el-descriptions-item>
        <el-descriptions-item :label="t('revocations.expires')">{{ data.revocations.expiresAt ? new Date(data.revocations.expiresAt * 1000).toLocaleString(locale) : t('common.unavailable') }}</el-descriptions-item>
        <el-descriptions-item :label="t('config.modelService')">{{ data.provider.enabled ? t('common.enabled') : t('common.disabled') }}</el-descriptions-item>
        <el-descriptions-item :label="t('authorization.servedModels')">{{ data.servedModels.join(t('common.listSeparator')) || t('common.none') }}</el-descriptions-item>
      </el-descriptions>
      <p class="muted">{{ t('authorization.admissionHint') }}</p>
      <el-empty v-if="!findings.length" :description="t('authorization.noIssues')" />
      <div v-else>
        <el-card v-for="finding in findings" :key="finding.code + finding.detail" shadow="never" class="finding">
          <div class="finding-head">
            <el-tag :type="(severityType[finding.severity] as any)" effect="dark" size="small">{{ finding.severity }}</el-tag>
            <el-tag type="info" size="small">{{ finding.code }}</el-tag>
          </div>
          <p>{{ finding.detail }}</p>
          <p class="muted">{{ finding.action }}</p>
          <el-button size="small" :aria-label="t('authorization.actionLabel') + finding.code" @click="act(finding)">{{ t('authorization.followAction') }}</el-button>
        </el-card>
      </div>
    </template>
  </el-card>
  <el-drawer v-model="assistant" :title="t('signing.title')" direction="rtl" size="var(--console-drawer-width)" destroy-on-close>
    <el-alert :title="t('signing.offlineNotice')" type="info" :closable="false" />
    <el-form label-position="top" class="spaced">
      <el-form-item :label="t('signing.kind')">
        <el-radio-group v-model="kind">
          <el-radio-button value="service">{{ t('document.service') }}</el-radio-button>
          <el-radio-button value="grant">{{ t('document.access') }}</el-radio-button>
          <el-radio-button value="revocations">{{ t('document.revocations') }}</el-radio-button>
        </el-radio-group>
      </el-form-item>
      <el-form-item :label="label('root', t('signing.root'))">
        <el-input v-model="root" :placeholder="t('signing.rootPlaceholder')" />
        <p class="field-hint">{{ hint('root') }}</p>
      </el-form-item>
      <el-form-item :label="label('file', t('signing.file'))">
        <el-input v-model="file" @input="fileTouched = true" />
        <p class="field-hint">{{ hint('file') }}</p>
      </el-form-item>
      <template v-if="kind === 'service'">
        <el-form-item :label="label('peers', t('signing.peers'))">
          <el-input v-model="peers" :placeholder="t('signing.peersPlaceholder')" />
          <p class="field-hint">{{ hint('peers') }}</p>
        </el-form-item>
        <el-form-item :label="label('models', t('signing.models'))">
          <el-input v-model="models" :placeholder="t('signing.modelsPlaceholder')" />
          <p class="field-hint">{{ hint('models') }}</p>
        </el-form-item>
        <el-form-item :label="label('addresses', t('signing.addresses'))">
          <el-input v-model="addresses" type="textarea" :rows="3" placeholder="/ip4/192.0.2.10/tcp/4001/p2p/12D3Koo..." @input="addressesTouched = true" />
          <p class="field-hint">{{ hint('addresses') }}</p>
          <p v-if="announceAddrs.length" class="field-hint">
            {{ t('signing.prefilledPrefix') }}<strong>{{ announceSource }}</strong>{{ t('signing.prefilledSuffix') }}{{ selfId }}{{ t('common.closeParen') }}{{ t('common.period') }}
            <el-button link type="primary" @click="resetAddresses">{{ t('signing.refill') }}</el-button>
          </p>
          <p v-else class="field-hint">{{ t('signing.noAnnounceHint') }}</p>
        </el-form-item>
        <el-form-item :label="label('relays', t('signing.relays'))">
          <el-input v-model="relays" type="textarea" :rows="2" placeholder="/ip4/203.0.113.7/tcp/4001/p2p/12D3Koo..." />
          <p class="field-hint">{{ hint('relays') }}</p>
        </el-form-item>
      </template>
      <template v-else-if="kind === 'grant'">
        <el-form-item :label="label('subject', t('signing.subject'))">
          <el-input v-model="subject" />
          <p class="field-hint">{{ hint('subject') }}</p>
        </el-form-item>
        <el-form-item :label="label('models', t('signing.models'))">
          <el-input v-model="models" :placeholder="t('signing.modelsPlaceholder')" />
          <p class="field-hint">{{ hint('models') }}</p>
        </el-form-item>
        <el-form-item :label="label('max-tokens', t('signing.maxTokens'))">
          <el-input-number v-model="maxTokens" :min="1" />
          <p class="field-hint">{{ hint('max-tokens') }}</p>
        </el-form-item>
        <el-form-item :label="label('concurrency', t('signing.concurrency'))">
          <el-input-number v-model="concurrency" :min="1" />
          <p class="field-hint">{{ hint('concurrency') }}</p>
        </el-form-item>
        <el-form-item :label="label('rpm', t('signing.rpm'))">
          <el-input-number v-model="rpm" :min="1" />
          <p class="field-hint">{{ hint('rpm') }}</p>
        </el-form-item>
      </template>
      <template v-else>
        <el-form-item :label="label('revoked', t('signing.revoked'))">
          <el-input v-model="revoked" :placeholder="t('signing.revokedPlaceholder')" />
          <p class="field-hint">{{ hint('revoked') }}</p>
        </el-form-item>
        <el-form-item :label="label('previous', t('signing.previous'))">
          <el-input v-model="previous" :placeholder="t('signing.previousPlaceholder')" />
          <p class="field-hint">{{ hint('previous') }}</p>
        </el-form-item>
      </template>
      <el-form-item :label="label('ttl', t('signing.ttl'))">
        <el-input v-model="ttl" />
        <p class="field-hint">{{ hint('ttl') }}</p>
      </el-form-item>
    </el-form>
    <el-alert v-if="kind === 'revocations'" :title="t('signing.rollbackHint')" type="warning" :closable="false" class="notice" />
    <el-alert v-if="kind === 'service'" :title="t('signing.delegatedModelsHint')" type="warning" :closable="false" class="notice" />
    <pre data-test="assistant-command">{{ command }}</pre>
    <p class="muted">{{ t('signing.installHint') }}</p>
    <template #footer>
      <el-button @click="assistant = false">{{ t('common.close') }}</el-button>
      <el-button type="primary" :disabled="!canSubmit" @click="copy">{{ t('signing.copy') }}</el-button>
    </template>
  </el-drawer>
</template>
