<template>
  <div class="container analysis-page">
    <page-card breadcrumb scrollPage>
      <div class="analysis-header">
        <div><h2>统计分析</h2><p v-if="snapshot">{{snapshot.scope}} · 更新于 {{snapshot.updatedAt}}</p></div>
        <a-space><a-radio-group v-model="days" type="button" @change="load"><a-radio :value="7">近 7 天</a-radio><a-radio :value="30">近 30 天</a-radio><a-radio :value="90">近 90 天</a-radio></a-radio-group><a-button :loading="loading" @click="load">刷新</a-button></a-space>
      </div>
      <a-alert v-if="loadError" type="error">统计读取失败，请点击刷新重试。</a-alert>
      <a-spin :loading="loading" style="width:100%">
        <template v-if="snapshot">
          <div class="stats-grid">
            <a-card v-for="card in visibleCards" :key="card.key" class="stat-card"><div class="muted">{{card.title}}</div><strong :data-stat="card.key">{{Number(snapshot.summary[card.key]||0).toLocaleString()}}</strong><div class="muted">{{card.period?'所选时段':'当前总量'}}</div></a-card>
          </div>
          <a-card title="每日趋势" class="section"><Chart :options="trendOption" height="300px" /></a-card>
          <div :key="generation" class="charts-grid section">
            <div class="trend-col"><AccessTimeslot /><Module class="section" /></div>
            <div><Os /><Browser class="section" />
              <a-card title="登录地域（前 10）" class="section"><a-table :data="snapshot.geo" :pagination="false" :columns="[{title:'地域',dataIndex:'name'},{title:'成功登录',dataIndex:'value'}]" size="small" /></a-card>
            </div>
          </div>
          <a-alert class="section">操作次数与访问 IP 按后台操作记录统计；浏览器、终端及地域按成功登录记录统计。画册访问按阅读记录统计，内容数量为当前总量。没有记录的项目显示为 0。</a-alert>
        </template>
      </a-spin>
    </page-card>
  </div>
</template>

<script setup lang="ts">
import { ref,computed,onMounted } from 'vue';
import { use } from 'echarts/core';
import { LineChart } from 'echarts/charts';
import useChartOption from '@/hooks/chart-option';
import { getOverview,resetAnalysis,type AnalysisSnapshot } from './api';
import AccessTimeslot from './components/AccessTimeslot.vue';
import Module from './components/Module.vue';
import Os from './components/Os.vue';
import Browser from './components/Browser.vue';
use([LineChart]);
const days=ref(30),loading=ref(false),loadError=ref(false),generation=ref(0),snapshot=ref<AnalysisSnapshot>();
const cards=[{key:'operations',title:'后台操作',period:true},{key:'uniqueIPs',title:'后台访问 IP',period:true},{key:'logins',title:'成功登录',period:true},{key:'albumVisits',title:'画册阅读',period:true},{key:'films',title:'影视内容',period:false},{key:'albums',title:'电子画册',period:false},{key:'privatePackages',title:'私有代码',period:false},{key:'members',title:'影视会员',period:false}];
const visibleCards=computed(()=>cards.filter(card=>snapshot.value?.canManageAll || !['films','members'].includes(card.key)));
const {chartOption:trendOption}=useChartOption(isDark=>({
  tooltip:{trigger:'axis'},legend:{data:['后台操作','成功登录','画册阅读'],textStyle:{color:isDark?'#c9cdd4':'#4e5969'}},
  grid:{left:45,right:25,top:45,bottom:35},
  xAxis:{type:'category',data:snapshot.value?.trend.map(d=>d.name.slice(5))||[],axisLabel:{color:isDark?'#c9cdd4':'#4e5969'}},
  yAxis:{type:'value',minInterval:1,axisLabel:{color:isDark?'#c9cdd4':'#4e5969'}},
  series:([{name:'后台操作',field:'operations'},{name:'成功登录',field:'logins'},{name:'画册阅读',field:'albumVisits'}] as const).map(s=>({name:s.name,type:'line' as const,smooth:true,data:snapshot.value?.trend.map(d=>d[s.field])||[]})),
}));
async function load(){if(loading.value)return;loading.value=true;loadError.value=false;try{resetAnalysis(days.value);snapshot.value=await getOverview();generation.value++;}catch{loadError.value=true;}finally{loading.value=false;}}
onMounted(load);
</script>

<style scoped>
.analysis-header{display:flex;justify-content:space-between;align-items:center;flex-wrap:wrap;gap:16px;margin-bottom:24px}.analysis-header h2{margin:0;color:var(--color-text-1)}.analysis-header p,.muted{color:var(--color-text-3)}.stats-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:16px}.stat-card strong{display:block;font-size:32px;margin:10px 0;color:var(--color-text-1)}.section{margin-top:18px}.charts-grid{display:grid;grid-template-columns:minmax(0,2fr) minmax(0,1fr);gap:18px}@media(max-width:1000px){.stats-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.charts-grid{grid-template-columns:1fr}}@media(max-width:550px){.stats-grid{grid-template-columns:1fr}}
</style>
