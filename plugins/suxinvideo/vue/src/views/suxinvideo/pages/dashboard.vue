<template>
  <div>
    <div class="stat-grid">
      <div class="stat red"><div class="t">影片总数</div><div class="n">{{ data.vod || 0 }}</div><div class="t">今日新增 {{ data.today_vod || 0 }}</div></div>
      <div class="stat"><div class="t">会员总数</div><div class="n">{{ data.user || 0 }}</div><div class="t">今日注册 {{ data.today_user || 0 }}</div></div>
      <div class="stat"><div class="t">成交订单</div><div class="n">{{ data.order_paid || 0 }}</div><div class="t">累计 ¥{{ money(data.order_money) }}</div></div>
      <div class="stat red"><div class="t">今日收入</div><div class="n">¥{{ money(data.today_money) }}</div><div class="t">评论 {{ data.comment || 0 }} 条</div></div>
    </div>
    <div class="card" style="margin-top:16px"><b>近7日数据</b><div class="tb-wrap"><table class="tb"><thead><tr><th>日期</th><th v-for="item in week" :key="item.date">{{ item.date }}</th></tr></thead><tbody><tr><th>新增影片</th><td v-for="item in week" :key="item.date">{{ item.vod }}</td></tr><tr><th>当日收入</th><td v-for="item in week" :key="item.date">¥{{ money(item.money) }}</td></tr></tbody></table></div></div>
    <div class="card"><b>最新订单</b><div class="tb-wrap"><table class="tb"><thead><tr><th>订单号</th><th>会员</th><th>套餐</th><th>金额</th><th>方式</th><th>状态</th><th>时间</th></tr></thead><tbody><tr v-for="item in orders" :key="item.id"><td>{{ item.order_no }}</td><td>{{ item.name || item.email }}</td><td>{{ item.title }}</td><td>¥{{ money(item.amount) }}</td><td>{{ item.pay_type }}</td><td><span class="tag" :class="item.status ? 'g' : 'gr'">{{ item.status ? '已支付' : '待支付' }}</span></td><td>{{ date(item.paid_time || item.created) }}</td></tr><tr v-if="!orders.length"><td colspan="7" class="hint">暂无订单</td></tr></tbody></table></div></div>
  </div>
</template>
<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue';
import { defHttp } from '@/utils/http';
const data = reactive<Record<string, any>>({});
const week = ref<Record<string, any>[]>([]);
const orders = ref<Record<string, any>[]>([]);
const money = (value: any) => Number(value || 0).toFixed(2);
const date = (value: any) => value ? new Date(Number(value) * 1000).toLocaleString('zh-CN') : '-';
onMounted(async () => { const result = await defHttp.get({ url: '/suxinvideo/dashboard' }); Object.assign(data, result.stats || {}); week.value = result.week || []; orders.value = result.orders || []; });
</script>
