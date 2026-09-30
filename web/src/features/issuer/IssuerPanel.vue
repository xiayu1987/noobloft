<!--
Copyright (c) 2026 xiayu
Contact: 126240622+xiayu1987@users.noreply.github.com
SPDX-License-Identifier: MIT
-->

<script setup lang="ts">
import { t, locale } from '../../i18n/index'

import { ref, reactive, onMounted, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '../../api/client'
const props=defineProps<{state:any}>()
const emit=defineEmits(['changed'])
const info=ref<any>(null), busy=ref(false), drawer=ref(''), target=ref('')
const form=reactive({kind:'access',subject:'',models:'',peers:'',addresses:'',relays:'',ttlHours:720,maxTokens:1024,maxConcurrent:1,requestsPerMinute:30})
const list=(s:string)=>[...new Set(s.split(/[\n,，]/).map(v=>v.trim()).filter(Boolean))]
const date=(n:number)=>new Date(n*1000).toLocaleString(locale.value)
async function refresh(){try{info.value=await api('issuer')}catch(e:any){ElMessage.error(e.message)}}
onMounted(refresh)
watch(locale, refresh)
async function submit(body:any){busy.value=true;try{info.value=await api('issuer','POST',body,props.state.revision);emit('changed');if(info.value.syncError)ElMessage.warning(t('issuer.syncFailed')+info.value.syncError);else ElMessage.success(t('common.completed'));drawer.value='';return true}catch(e:any){ElMessage.error(e.message);return false}finally{busy.value=false}}
async function initialize(){try{await ElMessageBox.confirm(t('issuer.init.confirm'),t('issuer.init.title'),{type:'warning',confirmButtonText:t('common.confirm'),cancelButtonText:t('common.cancel')});await submit({operation:'init'})}catch{}}
function open(kind:string){form.kind=kind;form.models=(props.state.saved.provider.service?.models||props.state.saved.provider.advertiseModels||[]).join('\n');form.peers=props.state.peerId;form.addresses=(props.state.running.network.announceAddrs?.length?props.state.running.network.announceAddrs:props.state.saved.network.announceAddrs||[]).map((a:string)=>a.includes('/p2p/')?a:a+'/p2p/'+props.state.peerId).join('\n');drawer.value='issue'}
async function issue(){await submit({operation:'issue',ttlHours:form.ttlHours,document:{kind:form.kind,subject:form.subject.trim(),models:list(form.models),peers:list(form.peers),addresses:list(form.addresses),relays:list(form.relays),maxTokens:form.maxTokens,maxConcurrent:form.maxConcurrent,requestsPerMinute:form.requestsPerMinute}})}
async function revoke(id:string){try{await ElMessageBox.confirm(t('issuer.revoke.confirm'),t('issuer.revoke.title'),{type:'warning',confirmButtonText:t('common.confirm'),cancelButtonText:t('common.cancel')});await submit({operation:'revoke',target:id})}catch{}}
function download(d:any){const url=URL.createObjectURL(new Blob([JSON.stringify(d,null,2)],{type:'application/json'}));const a=document.createElement('a');a.href=url;a.download=d.kind+'-'+d.id+'.json';a.click();URL.revokeObjectURL(url)}
async function install(d:any){busy.value=true;try{if(props.state.saved.provider.service)await ElMessageBox.confirm(t('issuer.install.confirm'),t('publisher.install'),{type:'warning',confirmButtonText:t('common.confirm'),cancelButtonText:t('common.cancel')});await api('publisher','POST',{operation:'install',trust:info.value.publisher,document:d,revocations:info.value.revocations},props.state.revision);emit('changed');ElMessage.success(t('issuer.install.saved'));await refresh()}catch(e:any){if(e?.message)ElMessage.error(e.message)}finally{busy.value=false}}
function revoked(d:any){return info.value.revocations?.revoked?.includes(d.id)|| (d.subject&&info.value.revocations?.revoked?.includes(d.subject))}
function installation(d:any){if(d.kind!=='service')return t('issuer.state.remoteUnconfirmed');const s=info.value.recordStates?.[d.id];if(!s)return t('issuer.state.unconfirmed');return (s.installed?t('issuer.state.installed'):t('issuer.state.notInstalled'))+' · '+(s.running?(s.providerEnabled?t('issuer.state.running'):t('issuer.state.disabled')):t('issuer.state.notRunning'))+(s.revoked?(s.localRevocationApplied?t('issuer.state.revocationApplied'):t('issuer.state.revocationUnconfirmed')):'')}
</script>
<template>
<el-card class="spaced" v-loading="busy">
 <div class="toolbar"><h3>{{ t('issuer.title') }}</h3><el-button @click="refresh">{{ t('issuer.refresh') }}</el-button></div>
 <el-alert :title="t('issuer.revocationsExplanation')" type="info" :closable="false" />
 <template v-if="info&&!info.enabled"><p>{{ t('issuer.intro') }}</p><el-button type="primary" @click="initialize">{{ t('issuer.init.enable') }}</el-button></template>
 <template v-if="info?.enabled">
  <p>{{ t('issuer.publisherLabel') }}<code>{{info.publisher}}</code></p>
  <el-alert v-if="info.syncError||info.renewalError" :title="info.syncError||info.renewalError" type="error" :closable="false"/>
  <p>{{ t('issuer.listVersion') }} {{info.revocations?.sequence}} {{ t('revocations.validUntil') }} {{date(info.revocations?.expiresAt)}} {{ t('issuer.autoRenew') }}</p>
  <p><el-tag :type="info.applied?'success':'warning'">{{info.applied?t('issuer.listApplied'):t('issuer.listUnconfirmed')}}</el-tag></p>
  <p class="muted">{{ t('issuer.remoteHint') }}</p>
  <div class="actions spaced"><el-button type="primary" @click="open('service')">{{ t('issuer.issueService') }}</el-button><el-button type="primary" @click="open('access')">{{ t('issuer.issueAccess') }}</el-button><el-button @click="target='';drawer='revoke'">{{ t('issuer.revokeConsumer') }}</el-button><el-button @click="submit({operation:'renew'})">{{ t('issuer.sync') }}</el-button><el-button :disabled="!info.revocations" @click="download(info.revocations)">{{ t('issuer.exportRevocations') }}</el-button></div>
  <p class="muted">{{ t('issuer.sourcesHint') }}</p>
  <el-table :data="info.records"><el-table-column prop="id" :label="t('issuer.credentialId')"/><el-table-column :label="t('issuer.installationStatus')"><template #default="{row}">{{installation(row)}}</template></el-table-column></el-table>
  <el-table :data="info.records"><el-table-column :label="t('common.type')" width="110"><template #default="{row}">{{row.kind==='service'?t('document.delegation'):t('document.access')}}</template></el-table-column><el-table-column prop="id" :label="t('issuer.credentialId')"/><el-table-column prop="subject" :label="t('signing.subject')"/><el-table-column :label="t('common.models')"><template #default="{row}">{{row.models?.join(t('common.listSeparator'))}}</template></el-table-column><el-table-column :label="t('common.status')"><template #default="{row}">{{revoked(row)?t('credential.revoked'):row.expiresAt*1000<=Date.now()?t('credential.expired'):t('credential.valid')}}<br/>{{date(row.expiresAt)}}</template></el-table-column><el-table-column :label="t('common.actions')" width="220"><template #default="{row}"><el-button size="small" @click="download(row)">{{ t('common.download') }}</el-button><el-button v-if="row.kind==='service'&&row.peers?.includes(state.peerId)" size="small" :disabled="!!revoked(row)||row.expiresAt*1000<=Date.now()" @click="install(row)">{{ t('issuer.installLocal') }}</el-button><el-button size="small" type="danger" :disabled="!!revoked(row)" @click="revoke(row.id)">{{ t('issuer.revoke') }}</el-button></template></el-table-column></el-table>
 </template>
</el-card>
<el-drawer :model-value="drawer==='issue'" :title="form.kind==='service'?t('issuer.issueService'):t('issuer.issueAccess')" direction="rtl" size="var(--console-drawer-width)" @close="drawer=''">
 <el-form label-position="top" @submit.prevent="issue">
  <el-form-item v-if="form.kind==='access'" :label="t('issuer.form.subject')" required><el-input v-model="form.subject"/><p class="field-hint">{{ t('issuer.form.subjectHint') }}</p></el-form-item>
  <el-form-item v-else :label="t('issuer.form.peers')" required><el-input v-model="form.peers" type="textarea"/></el-form-item>
  <el-form-item :label="t('issuer.form.models')" required><el-input v-model="form.models" type="textarea"/></el-form-item>
  <el-form-item :label="t('issuer.form.ttl')"><el-input-number v-model="form.ttlHours" :min="1" :max="8760"/></el-form-item>
  <template v-if="form.kind==='access'"><el-form-item :label="t('issuer.form.tokens')"><el-input-number v-model="form.maxTokens" :min="1"/></el-form-item><el-form-item :label="t('issuer.form.concurrency')"><el-input-number v-model="form.maxConcurrent" :min="1"/></el-form-item><el-form-item :label="t('issuer.form.rpm')"><el-input-number v-model="form.requestsPerMinute" :min="1"/></el-form-item></template>
  <template v-else><el-form-item :label="t('issuer.form.addresses')"><el-input v-model="form.addresses" type="textarea" :placeholder="t('issuer.form.addressPlaceholder')"/><p class="field-hint">{{ t('issuer.form.addressHint') }}</p></el-form-item><el-form-item :label="t('issuer.form.relays')"><el-input v-model="form.relays" type="textarea" :placeholder="t('issuer.form.relayPlaceholder')"/><p class="field-hint">{{ t('issuer.form.relayHint') }}</p></el-form-item></template>
  <el-button native-type="submit" type="primary" :loading="busy" :disabled="!form.models.trim()||(form.kind==='access'?!form.subject.trim():!form.peers.trim())">{{ t('issuer.issueSave') }}</el-button>
 </el-form>
</el-drawer>
<el-drawer :model-value="drawer==='revoke'" :title="t('issuer.revokeConsumerTitle')" direction="rtl" size="var(--console-drawer-width)" @close="drawer=''"><el-form label-position="top" @submit.prevent="revoke(target.trim())"><el-form-item :label="t('issuer.form.subject')"><el-input v-model="target"/><p class="field-hint">{{ t('issuer.revokeConsumerHint') }}</p></el-form-item><el-button native-type="submit" type="danger" :disabled="!target.trim()" :loading="busy">{{ t('issuer.revokeConsumer') }}</el-button></el-form></el-drawer>
</template>
