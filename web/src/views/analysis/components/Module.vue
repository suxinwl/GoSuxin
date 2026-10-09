<template>
  <a-spin :loading="loading" style="width: 100%">
    <a-card class="general-card" title="后台操作模块（前 10）">
      <div class="chart">
        <Chart v-if="!loading && chartData.length" :options="chartOption" height="355px" />
        <a-empty v-else-if="!loading" description="所选时段没有后台操作记录" />
      </div>
    </a-card>
  </a-spin>
</template>

<script setup lang="ts">
import { ref ,onMounted} from 'vue'
import useChartOption from '@/hooks/chart-option';
import { type DashboardChartCommonResp, getAnalysisModule as getData } from '../api'
import { use } from 'echarts/core'
import { BarChart  } from 'echarts/charts'
use([ BarChart ])
const yAxis = ref<string[]>([])
const chartData = ref<any>([])
const { chartOption } = useChartOption((isDark) => {
  return {
    grid: {
      left: 100,
      right: 20,
      top: 0,
      bottom: 20,
    },
    xAxis: {
      type: 'value',
      axisLabel: {
        show: true,
        formatter(value: number, idx: number) {
          if (idx === 0) return String(value)
          return value >= 1000 ? `${value / 1000}k` : String(value)
        },
      },
      splitLine: {
        lineStyle: {
          color: isDark ? '#484849' : '#E5E8EF',
        },
      },
    },
    yAxis: {
      type: 'category',
      data: yAxis.value,
      axisLabel: {
        show: true,
        color: '#4E5969',
      },
      axisTick: {
        show: true,
        length: 2,
        lineStyle: {
          color: '#A9AEB8',
        },
        alignWithLabel: true,
      },
      axisLine: {
        lineStyle: {
          color: isDark ? '#484849' : '#A9AEB8',
        },
      },
    },
    tooltip: {
      show: true,
      trigger: 'axis',
    },
    series: [
      {
        data: chartData.value,
        type: 'bar',
        barWidth: 7,
        itemStyle: {
          color: '#4086FF',
          borderRadius: 4,
        },
      },
    ],
  }
})

const loading = ref(false)
// 查询图表数据
const getChartData = async () => {
  try {
    loading.value = true
    const { data } = await getData()
    data.forEach((item: DashboardChartCommonResp) => {
      yAxis.value.unshift(item.name)
      chartData.value.unshift(item.value)
    })
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  getChartData()
})
</script>

<style scoped lang="less">
</style>
