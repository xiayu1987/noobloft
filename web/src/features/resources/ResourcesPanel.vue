<!--
Copyright (c) 2026 xiayu
Contact: 126240622+xiayu1987@users.noreply.github.com
SPDX-License-Identifier: MIT
-->

<script setup lang="ts">
import { t, locale } from '../../i18n/index'

import {ref,onMounted,watch} from 'vue'
import {ElMessage,ElMessageBox} from 'element-plus'
import {api} from '../../api/client'
const props=defineProps<{state:any}>(),emit=defineEmits(['changed'])
const info=ref<any>(null),busy=ref(false),reset=ref(false),confirmation=ref(''),backup=ref('')
async function refresh(){try{info.value=await api('resources')}catch(e:any){ElMessage.error(e.message)}}
onMounted(refresh)
watch(locale, refresh)
async function restart(){try{await ElMessageBox.confirm(t('lifecycle.restart.confirm'),t('lifecycle.restart.title'),{confirmButtonText:t('lifecycle.restart.action'),cancelButtonText:t('common.cancel'),type:'warning'});busy.value=true;await api('lifecycle','POST',{operation:'restart'},props.state.revision);let ok=false;const before=props.state.startedAt;for(let i=0;i<30;i++){await new Promise(r=>setTimeout(r,1000));try{const s=await api('state');if(s.startedAt!==before){ok=true;break}}catch{}}if(ok){emit('changed');ElMessage.success(t('lifecycle.restart.completed'))}else ElMessage.warning(t('lifecycle.restart.unconfirmed'))}catch(e:any){if(e?.message)ElMessage.error(e.message)}finally{busy.value=false}}
async function preview(){try{info.value=await api('resources');confirmation.value='';backup.value='';reset.value=true}catch(e:any){ElMessage.error(e.message)}}
const statusText=(s:string)=>({pending:t('recovery.status.pending'),completed:t('recovery.status.completed'),rolled_back:t('recovery.status.rolledBack'),failed:t('recovery.status.failed')}[s]||s)
async function restore(){busy.value=true;try{const op=await api('lifecycle','POST',{operation:'reset',confirm:confirmation.value,backup:backup.value},info.value.revision);reset.value=false;for(let i=0;i<45;i++){await new Promise(r=>setTimeout(r,1000));try{info.value=await api('resources');const s=info.value.recovery;if(s?.id===op.id&&s.status!=='pending'){emit('changed');if(s.status==='completed')ElMessage.success(statusText(s.status));else ElMessage.warning(statusText(s.status));return}}catch{}}ElMessage.warning(t('recovery.unconfirmed'))}catch(e:any){ElMessage.error(e.message)}finally{busy.value=false}}
</script>
<template>
<section v-loading="busy">
<el-alert :title="t('resources.sourcesHint')" :closable="false"/>
<el-card class="spaced"><h3>{{ t('recovery.title') }}</h3><el-button :disabled="!info?.restartSupported" @click="restart">{{ t('lifecycle.restart.title') }}</el-button><el-button type="danger" plain :disabled="!info?.restartSupported" @click="preview">{{ t('recovery.reset') }}</el-button><p>{{info?.resetScope}}</p><p v-if="info?.recovery">{{ t('recovery.latestOperation') }} {{info.recovery.id}}{{ t('common.colon') }}{{statusText(info.recovery.status)}}{{ t('recovery.backupLabel') }}{{info.recovery.backup}} {{info.recovery.error}}</p><h4>{{ t('recovery.newNode') }}</h4><p>{{ t('recovery.newNodeHint') }}</p><pre>Windows{{ t('common.colon') }}.\scripts\start.ps1 --mode shared --dir "D:\Swarm Nodes\new-node"
Linux{{ t('common.colon') }}sh scripts/start.sh --mode shared --dir "/srv/swarm/new-node"</pre></el-card>
<el-card class="spaced"><h3>{{ t('resources.title') }}</h3><p>{{ t('resources.directoryLabel') }}<code>{{info?.directory}}</code></p><el-table :data="info?.resources"><el-table-column prop="name" :label="t('resources.resource')" width="150"/><el-table-column prop="path" :label="t('resources.path')"/><el-table-column prop="purpose" :label="t('resources.purpose')"/><el-table-column prop="source" :label="t('resources.source')"/><el-table-column :label="t('resources.exists')"><template #default="{row}">{{row.exists?t('resources.exists'):t('resources.notCreated')}}</template></el-table-column></el-table><p>{{ t('resources.preservationHint') }}</p></el-card>
<el-card class="spaced"><h3>{{ t('help.title') }}</h3>
<h4>{{ t('help.firstStart') }}</h4>
<pre>Windows PowerShell{{ t('common.colon') }}
.\scripts\start.ps1 --mode shared
.\scripts\start.ps1 --help

Linux{{ t('common.colon') }}
sh scripts/start.sh --mode shared
sh scripts/start.sh --help</pre>
<p>{{ t('help.startExplanation') }}</p>
<p>{{ t('help.signInSteps') }}</p>
<ol><li>{{ t('help.overview') }}</li><li>{{ t('help.settings') }}</li><li>{{ t('help.publisher') }}</li><li>{{ t('help.consumer') }}</li><li>{{ t('help.revocation') }}</li><li>{{ t('help.peers') }}</li><li>{{ t('help.resources') }}</li></ol>
<p>{{ t('help.cliIntro') }}</p>
<pre>{{ t('help.cliCommands') }}</pre>
<p>{{ t('help.cliHint') }}</p>
</el-card>
</section>
<el-drawer v-model="reset" :title="t('recovery.drawerTitle')" direction="rtl"><p>{{ t('recovery.directoryLabel') }}{{info?.directory}}{{ t('common.semicolon') }}PeerID{{ t('common.colon') }}{{props.state?.peerId}}</p><p>{{info?.resetScope}}</p><p>{{ t('recovery.backupHint') }}</p><el-form @submit.prevent="restore"><el-form-item :label="t('recovery.selectBackup')"><el-select v-model="backup" clearable :placeholder="t('recovery.reset')"><el-option v-for="r in info?.resources?.filter((r:any)=>r.kind==='config_backup')" :key="r.path" :value="r.path.split(/[\\/]/).pop()" :label="r.path"/></el-select></el-form-item><p v-if="backup">{{ t('recovery.restoreBackupHint') }}</p><el-form-item :label="t('recovery.confirmLabel')"><el-input v-model="confirmation"/></el-form-item><el-button native-type="submit" type="danger" :disabled="confirmation!=='RESET CONFIG'" :loading="busy">{{ t('recovery.submit') }}</el-button></el-form></el-drawer>
</template>
