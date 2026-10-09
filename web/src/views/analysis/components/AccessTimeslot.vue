<template>
  <a-spin :loading="loading" style="width:100%">
    <a-card class="general-card" title="后台操作时段（每两小时）"><Chart :options="chartOption" height="320px" /></a-card>
  </a-spin>
</template>
<script setup lang="ts">
import { ref,onMounted } from 'vue';
import useChartOption from '@/hooks/chart-option';
import { getAnalysisTimeslot,type DashboardChartCommonResp } from '../api';
const loading=ref(false),data=ref<DashboardChartCommonResp[]>([]);
const {chartOption}=useChartOption(isDark=>({
  grid:{left:45,right:25,top:25,bottom:35},
  xAxis:{type:'category',data:data.value.map(p=>p.name),axisLabel:{color:isDark?'#c9cdd4':'#4e5969'}},
  yAxis:{type:'value',minInterval:1,axisLabel:{color:isDark?'#c9cdd4':'#4e5969'}},
  tooltip:{trigger:'axis'},
  series:[{name:'后台操作',type:'line',smooth:true,data:data.value.map(p=>p.value),areaStyle:{opacity:0.15},itemStyle:{color:'#246eff'}}],
}));
onMounted(async()=>{loading.value=true;try{data.value=(await getAnalysisTimeslot()).data;}finally{loading.value=false;}});
</script>
