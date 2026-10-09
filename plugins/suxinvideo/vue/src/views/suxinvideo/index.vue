<template>
  <div class="sx-admin">
    <div class="layout">
      <aside class="side" :class="{ open: menuOpen }">
        <div class="brand"><img class="brand-logo" :src="'/suxinvideo/brand?file=app-icon.png'" alt="" />速信影视CMS</div>
        <template v-for="group in visibleGroups" :key="group.name">
          <div v-if="group.name" class="grp">{{ group.name }}</div>
          <a v-for="item in group.items" :key="item.view" class="mi" :class="{ on: view === item.view }" href="#" @click.prevent="navigate(item.view)">{{ item.view === 'main/dashboard' ? '◈' : '▪' }} {{ item.title }}</a>
        </template>
      </aside>
      <div class="overlay" :class="{ show: menuOpen }" @click="menuOpen = false" />
      <div class="main">
        <div class="topbar">
          <div class="top-title"><button class="btn plain sm menu-toggle" @click="menuOpen = true">☰</button><h1>管理后台</h1></div>
          <div class="ops"><a class="op-btn" href="/suxinvideo/" target="_blank">访问站点</a><button v-if="capabilities?.actions.clearCache" class="op-btn" @click="clearCache">清理缓存</button><button class="op-btn exit" @click="logout">退出</button></div>
        </div>
        <div v-if="!capabilities" class="card">正在加载管理权限…</div>
        <Dashboard v-else-if="view === 'main/dashboard'" />
        <Collect v-else-if="view === 'content/collect'" :permissions="capabilities.resources.collect_api || {}" :actions="capabilities.actions" />
        <Live v-else-if="view === 'content/live'" :actions="capabilities.actions" />
        <Templates v-else-if="view === 'system/market'" :permissions="capabilities.resources.plugin || {}" :actions="capabilities.actions" />
        <Settings v-else-if="view === 'system/setting'" :actions="capabilities.actions" />
        <Images v-else-if="view === 'system/images'" :actions="capabilities.actions" />
        <Logs v-else-if="view === 'system/log'" :actions="capabilities.actions" />
        <Clients v-else-if="view === 'system/clients'" :actions="capabilities.actions" />
        <Resource v-else :kind="resourceKind" :permissions="capabilities.resources[resourceKind] || {}" :actions="capabilities.actions" :key="view" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useUserStore } from '@/store';
import { defHttp } from '@/utils/http';
import { Message } from '@arco-design/web-vue';
import Dashboard from './pages/dashboard.vue';
import Resource from './pages/resource.vue';
import Collect from './pages/collect.vue';
import Templates from './pages/templates.vue';
import Settings from './pages/settings.vue';
import Images from './pages/images.vue';
import Logs from './pages/logs.vue';
import Clients from './pages/clients.vue';
import Live from './pages/live.vue';
import './admin.css';

const route = useRoute();
const router = useRouter();
const userStore = useUserStore();
const menuOpen = ref(false);
const capabilities = ref<{ resources: Record<string, Record<string, boolean>>; pages: Record<string, boolean>; actions: Record<string, boolean> } | null>(null);
const groups = [
  { name: '', items: [{ view: 'main/dashboard', title: '仪表盘' }] },
  { name: '内容管理', items: [
    { view: 'content/vod', title: '视频管理' }, { view: 'content/type', title: '分类管理' },
    { view: 'content/slide', title: '幻灯管理' }, { view: 'content/article', title: '文章公告' },
    { view: 'content/link', title: '友情链接' },
    { view: 'content/live', title: '直播管理' },
  ] },
  { name: '采集中心', items: [
    { view: 'content/collect', title: '采集管理' }, { view: 'content/filmreqs', title: '求片管理' },
    { view: 'content/player', title: '播放器管理' },
  ] },
  { name: '用户中心', items: [
    { view: 'user/user', title: '会员列表' }, { view: 'user/goods', title: '充值套餐' },
    { view: 'user/order', title: '订单管理' }, { view: 'user/comment', title: '评论管理' },
  ] },
  { name: '应用商店', items: [{ view: 'system/market', title: '模板管理' }] },
  { name: '系统', items: [
    { view: 'system/setting', title: '系统设置' }, { view: 'system/images', title: '图片管理' },
    { view: 'system/clients', title: '客户端发布' },
    { view: 'system/log', title: '安全日志' },
  ] },
];
const visibleGroups = computed(() => capabilities.value ? groups.map(group => ({ ...group, items: group.items.filter(item => capabilities.value?.pages[item.view]) })).filter(group => group.items.length) : []);
const view = computed(() => {
  const selected = String(route.query.view || 'main/dashboard');
  if (!capabilities.value) return selected;
  return capabilities.value.pages[selected] ? selected : (visibleGroups.value[0]?.items[0]?.view || 'main/dashboard');
});
const resourceKind = computed(() => ({
  'content/vod': 'vod', 'content/type': 'type', 'content/slide': 'slide',
  'content/article': 'article', 'content/link': 'link', 'content/filmreqs': 'film_request',
  'content/player': 'player', 'user/user': 'user', 'user/goods': 'goods',
  'user/order': 'order', 'user/comment': 'comment',
} as Record<string, string>)[view.value] || 'vod');
function navigate(next: string) { menuOpen.value = false; router.push({ path: '/suxinvideo/admin', query: { view: next } }); }
async function clearCache() { await defHttp.post({ url: '/suxinvideo/clearCache', params: {} }); Message.success('缓存已清理'); }
async function logout() { await userStore.logout(true); }
onMounted(async () => { capabilities.value = await defHttp.get({ url: '/suxinvideo/capabilities' }); });
</script>
