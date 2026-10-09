import {
  createApp,
  computed,
  nextTick,
  onMounted,
  onUnmounted,
  ref,
} from 'vue';
import { PageFlip } from 'page-flip';
import './style.css';
import { copy, isEnglish, publicError } from './locale.js';
const branding = window.SuxinAlbumSite.branding;
const otherSite = window.SuxinAlbumSite.sites[isEnglish ? 'zh' : 'en'];

const api = async (url, options = {}) => {
  const response = await fetch(url, {
    headers: { 'Content-Type': 'application/json', 'X-Album-Site': location.pathname, ...(options.headers || {}) },
    ...options,
  });
  const body = await response.json();
  if (!body || body.code !== 0) {
    const error = new Error(body?.message || copy.requestError);
    error.code = body?.code;
    throw error;
  }
  return body.data;
};
const catalogueCache = new Map();
const coverPreview = (address) => {
  if (!address) return '';
  const u = new URL(address, location.origin);
  if (u.origin === location.origin && ['/common/album/file','/common/album/category-cover'].includes(u.pathname)) u.searchParams.set('preview','cover');
  return u.href;
};
const fetchAlbums = async (categoryId = '', onBatch = () => {}, signal) => {
  const items = [];
  for (let page = 1; ; page += 1) {
    const data = await api(
      `/common/album/list?page=${page}&pageSize=50&categoryId=${encodeURIComponent(
        categoryId
      )}`, { signal }
    );
    const batch = data.items || [];
    items.push(...batch);
    onBatch([...items]);
    if (!batch.length || items.length >= data.total) {
      catalogueCache.delete(String(categoryId));
      catalogueCache.set(String(categoryId), {items:[...items], at:Date.now()});
      if (catalogueCache.size > 24) catalogueCache.delete(catalogueCache.keys().next().value);
      return items;
    }
  }
};
export default {
  setup() {
    const shelfDefs = ref([]);
    const shareNeedsPassword = ref(false),
      shareUnavailable = ref(false);
    const loadCategories = async () => {
      const rows = await api('/common/album/categories');
      shelfDefs.value = (rows || []).map((c, i) => ({
        ...c,
        key: c.stable_key,
        en: c.english_title,
        color: ['blue', 'navy', 'cyan'][i % 3],
      }));
    };
    const route = ref(location.hash || '#/');
    const allAlbums = ref([]);
    const shelfAlbums = ref([]);
    const active = ref(null);
    const pages = ref([]);
    const pageIndex = ref(0);
    const pageInput = ref(1);
    const zoom = ref(1);
    const loading = ref(false);
    const password = ref('');
    const error = ref('');
    const showThumbs = ref(false);
    const isFullscreen = ref(false);
    const touchStart = ref(null);
    const bookEl = ref(null);
    const pageFlip = ref(null);
    const bookPosition = ref('start');
    const showLibrary = ref(false);
    const libraryTab = ref('company');
    const libraryLoading = ref(false);
    const isReader = computed(
      () =>
        route.value.startsWith('#/album/') || route.value.startsWith('#/share/')
    );
    const shelfKey = computed(() =>
      route.value.startsWith('#/shelf/') ? route.value.split('/')[2] : ''
    );
    const shelf = computed(() =>
      shelfDefs.value.find((item) => item.key === shelfKey.value)
    );
    const albumByCategory = computed(() =>
      shelfDefs.value.map((def) => ({
        ...def,
        albums: allAlbums.value.filter(
          (album) => Number(album.category_id) === Number(def.id)
        ),
      }))
    );
    const libraryAlbums = computed(() =>
      allAlbums.value.filter(
        (album) =>
          Number(album.category_id) ===
          Number(
            shelfDefs.value.find((item) => item.key === libraryTab.value)?.id
          )
      )
    );
    const canPrev = computed(() => pageIndex.value > 0);
    const canNext = computed(() => pageIndex.value < pages.value.length - 1);
    const currentLabel = computed(() => pageIndex.value + 1);
    const progress = computed(() =>
      pages.value.length
        ? `${((pageIndex.value + 1) / pages.value.length) * 100}%`
        : '0%'
    );
    const hasSingleBackCover = computed(
      () => pages.value.length > 1 && pages.value.length % 2 === 0
    );
    const bookTransform = computed(
      () =>
        `translateX(${
          bookPosition.value === 'start'
            ? '-25%'
            : bookPosition.value === 'end'
            ? '25%'
            : '0%'
        }) scale(${zoom.value})`
    );
    const applyBookTransform = () =>
      nextTick(() => {
        if (bookEl.value) bookEl.value.style.transform = bookTransform.value;
      });
    const setBookPosition = (position) => {
      bookPosition.value = position;
      applyBookTransform();
    };
    let routeController, routeSequence = 0;
    const loadCatalogue = async (categoryId, target, expectedRoute) => {
      const sequence = routeSequence;
      const cached = catalogueCache.get(String(categoryId));
      target.value = cached?.items || [];
      loading.value = !cached;
      error.value = '';
      if (cached && Date.now() - cached.at < 60000) return;
      try {
        await fetchAlbums(categoryId, items => {
          if (sequence === routeSequence && route.value === expectedRoute) {
            target.value = items;
            loading.value = false;
          }
        }, routeController.signal);
      } catch (e) {
        if (sequence === routeSequence && e.name !== 'AbortError') error.value = publicError(e);
      } finally {
        if (sequence === routeSequence) loading.value = false;
      }
    };
    const loadHome = () => loadCatalogue('', allAlbums, '#/');
    const ensureLibraryAlbums = async () => {
      if (allAlbums.value.length) return;
      libraryLoading.value = true;
      try {
        const cached = catalogueCache.get('');
        if (cached && Date.now()-cached.at<60000) allAlbums.value=cached.items;
        else allAlbums.value = await fetchAlbums();
      } catch (e) {
        error.value = publicError(e);
      } finally {
        libraryLoading.value = false;
      }
    };
    const loadShelf = (key) => loadCatalogue(
      shelfDefs.value.find(item => item.key === key)?.id || 0,
      shelfAlbums, `#/shelf/${key}`
    );
    const destroyBook = (restoreHost = false) => {
      if (!pageFlip.value) return;
      const oldHost = bookEl.value;
      const hostParent = oldHost?.parentElement;
      pageFlip.value.destroy();
      pageFlip.value = null;
      if (restoreHost && hostParent?.isConnected) {
        const newHost = document.createElement('div');
        newHost.className = 'pageflip-book';
        hostParent.appendChild(newHost);
        bookEl.value = newHost;
      }
    };
    const initializeBook = async () => {
      await nextTick();
      if (!bookEl.value || !pages.value.length) return;
      const pageNodes = pages.value.map((item, index) => {
        const element = document.createElement('div');
        element.className = 'flip-page';
        element.dataset.density =
          index === 0 || index === pages.value.length - 1 ? 'hard' : 'soft';
        const image = document.createElement('img');
        let retried = false;
        image.onerror = () => {
          if (!retried) {
            retried = true;
            const retry = new URL(item.image_url, location.origin);
            retry.searchParams.set('_retry', String(Date.now()));
            image.src = retry.href;
          } else {
            error.value = copy.imageError;
          }
        };
        image.src = item.image_url;
        image.alt = `${active.value?.title || copy.catalogue} · ${index + 1}`;
        element.appendChild(image);
        return element;
      });
      if (pageFlip.value && bookEl.value.isConnected) {
        bookEl.value.style.transition = 'none';
        pageFlip.value.updateFromHtml(pageNodes);
        pageFlip.value.turnToPage(0);
        pageIndex.value = 0;
        pageInput.value = 1;
        setBookPosition('start');
        requestAnimationFrame(() =>
          requestAnimationFrame(() => {
            if (bookEl.value) bookEl.value.style.removeProperty('transition');
          })
        );
        return;
      }
      destroyBook(true);
      if (!bookEl.value?.isConnected) return;
      bookEl.value.style.transition = 'none';
      setBookPosition('start');
      const instance = new PageFlip(bookEl.value, {
        width: 620,
        height: 860,
        size: 'stretch',
        minWidth: 280,
        maxWidth: 920,
        minHeight: 390,
        maxHeight: 1080,
        showCover: true,
        drawShadow: true,
        maxShadowOpacity: 0.55,
        flippingTime: 760,
        usePortrait: true,
        startZIndex: 1,
        autoSize: true,
        mobileScrollSupport: false,
        useMouseEvents: true,
        swipeDistance: 20,
      });
      instance.on('changeState', (event) => {
        if (event.data === 'read') {
          const current = instance.getCurrentPageIndex();
          setBookPosition(
            current === 0
              ? 'start'
              : hasSingleBackCover.value && current === pages.value.length - 1
              ? 'end'
              : 'center'
          );
          return;
        }
        if (event.data !== 'flipping' && event.data !== 'user_fold') return;
        const direction = instance.getRender().getDirection();
        const current = instance.getCurrentPageIndex();
        if (direction === 0 && current === 0) setBookPosition('center');
        else if (direction === 1 && current === 1) setBookPosition('start');
        else if (
          hasSingleBackCover.value &&
          direction === 0 &&
          current >= pages.value.length - 3
        )
          setBookPosition('end');
        else if (
          hasSingleBackCover.value &&
          direction === 1 &&
          current === pages.value.length - 1
        )
          setBookPosition('center');
      });
      instance.on('flip', (event) => {
        pageIndex.value = event.data;
        pageInput.value = event.data + 1;
        setBookPosition(
          event.data === 0
            ? 'start'
            : hasSingleBackCover.value && event.data === pages.value.length - 1
            ? 'end'
            : 'center'
        );
      });
      instance.loadFromHTML(pageNodes);
      pageFlip.value = instance;
      setBookPosition('start');
      requestAnimationFrame(() =>
        requestAnimationFrame(() => {
          if (bookEl.value) bookEl.value.style.removeProperty('transition');
        })
      );
    };
    const loadAlbum = async (id) => {
      const sequence=routeSequence;
      loading.value = true;
      error.value = '';
      try {
        const data = await api(`/common/album/detail?id=${id}`, {signal:routeController.signal});
        if(sequence!==routeSequence) return;
        active.value = data.album;
        pages.value = data.pages || [];
        pageIndex.value = 0;
        pageInput.value = 1;
        zoom.value = 1;
        await initializeBook();
      } catch (e) {
        if(sequence!==routeSequence || e.name==='AbortError') return;
        destroyBook();
        active.value = null;
        pages.value = [];
        error.value = publicError(e);
      } finally {
        if(sequence===routeSequence) loading.value = false;
      }
    };
    const openShelf = (key) => {
      location.hash = `#/shelf/${key}`;
    };
    const openAlbum = (id) => {
      location.hash = `#/album/${id}`;
    };
    const back = () => {
      const ownerShelf = shelfDefs.value.find(
        (item) => Number(item.id) === Number(active.value?.category_id)
      );
      location.hash = ownerShelf ? `#/shelf/${ownerShelf.key}` : '#/';
    };
    const openLibrary = async () => {
      const ownerShelf = shelfDefs.value.find(
        (item) => Number(item.id) === Number(active.value?.category_id)
      );
      if (ownerShelf) libraryTab.value = ownerShelf.key;
      showThumbs.value = false;
      showLibrary.value = true;
      await ensureLibraryAlbums();
    };
    const closeLibrary = () => {
      showLibrary.value = false;
    };
    const selectLibraryTab = (key) => {
      libraryTab.value = key;
    };
    const openLibraryAlbum = (id) => {
      showLibrary.value = false;
      if (String(active.value?.id) !== String(id))
        location.hash = `#/album/${id}`;
    };
    const prev = () => {
      if (canPrev.value) pageFlip.value?.flipPrev('bottom');
    };
    const next = () => {
      if (canNext.value) pageFlip.value?.flipNext('bottom');
    };
    const goPage = () => {
      const target =
        Math.min(
          pages.value.length,
          Math.max(1, Number(pageInput.value) || 1)
        ) - 1;
      pageFlip.value?.flip(target, target < pageIndex.value ? 'top' : 'bottom');
    };
    const choosePage = (index) => {
      pageFlip.value?.flip(index, index < pageIndex.value ? 'top' : 'bottom');
      showThumbs.value = false;
    };
    const zoomIn = () => {
      zoom.value = Math.min(1.5, Number((zoom.value + 0.1).toFixed(2)));
      applyBookTransform();
    };
    const zoomOut = () => {
      zoom.value = Math.max(0.75, Number((zoom.value - 0.1).toFixed(2)));
      applyBookTransform();
    };
    const toggleFullscreen = async () => {
      try {
        if (!document.fullscreenElement)
          await document.documentElement.requestFullscreen();
        else await document.exitFullscreen();
      } catch (_) {
        error.value = copy.fullscreenError;
      }
    };
    const openShare = async () => {
      loading.value = true;
      error.value = '';
      const key = route.value.split('/')[2];
      try {
        const data = await api('/common/album/share', {
          method: 'POST',
          body: JSON.stringify({ key, password: password.value }),
        });
        if (route.value !== `#/share/${key}`) return;
        active.value = data.album;
        pages.value = data.pages || [];
        pageIndex.value = 0;
        pageInput.value = 1;
        shareNeedsPassword.value = false;
        shareUnavailable.value = false;
        await initializeBook();
      } catch (e) {
        if (route.value !== `#/share/${key}`) return;
        error.value = publicError(e);
        shareNeedsPassword.value =
          e.code === 4101 || e.message === '分享密码错误';
        shareUnavailable.value = !shareNeedsPassword.value;
      } finally {
        loading.value = false;
      }
    };
    const syncRoute = async () => {
      routeSequence++;
      routeController?.abort();
      routeController = new AbortController();
      route.value = location.hash || '#/';
      showThumbs.value = false;
      showLibrary.value = false;
      if (route.value === '#/') {
        destroyBook();
        active.value = null;
        pages.value = [];
        loadHome();
      } else if (route.value.startsWith('#/shelf/')) {
        destroyBook();
        active.value = null;
        pages.value = [];
        loadShelf(route.value.split('/')[2]);
      } else if (route.value.startsWith('#/album/'))
        loadAlbum(route.value.split('/')[2]);
      else if (route.value.startsWith('#/share/')) {
        destroyBook();
        active.value = null;
        pages.value = [];
        password.value = '';
        shareNeedsPassword.value = false;
        shareUnavailable.value = false;
        await openShare();
      }
    };
    const onKey = (event) => {
      if (!isReader.value) return;
      if (event.key === 'Escape' && showLibrary.value) {
        closeLibrary();
        return;
      }
      if (showLibrary.value) return;
      if (event.key === 'ArrowLeft') prev();
      if (event.key === 'ArrowRight' || event.key === ' ') {
        event.preventDefault();
        next();
      }
      if (event.key === 'Escape') showThumbs.value = false;
    };
    // PageFlip owns drag and swipe gestures inside the book canvas.
    const touchBegin = () => {};
    const touchEnd = () => {};
    let libraryPanelApp = null;
    let libraryPanelHost = null;
    let homeNavApp = null;
    let homeNavHost = null;
    const LibraryPanel = {
      setup: () => ({
        branding,
        coverPreview,
        active,
        isReader,
        showLibrary,
        libraryTab,
        libraryLoading,
        libraryAlbums,
        tabs: shelfDefs,
        openLibrary,
        closeLibrary,
        selectLibraryTab,
        openLibraryAlbum,
      }),
      template: `<div v-if="isReader" class="reader-library-ui"><button class="library-launcher" type="button" title="${copy.openLibrary}" @click="openLibrary"><span class="library-launcher-icon">▥</span><b>${copy.library}</b></button><div v-if="showLibrary" class="library-backdrop" @click.self="closeLibrary"><aside class="library-drawer"><header class="library-head"><div><small>{{ branding.name }}</small><strong>${copy.library}</strong></div><button type="button" title="${copy.close}" @click="closeLibrary">×</button></header><nav class="library-tabs" aria-label="${copy.categories}"><button v-for="tab in tabs" :key="tab.key" type="button" :class="{selected:libraryTab===tab.key}" @click="selectLibraryTab(tab.key)"><span>{{tab.name}}</span><small>{{tab.en}}</small></button></nav><div class="library-body"><p v-if="libraryLoading" class="library-state">${copy.loading}</p><p v-else-if="!libraryAlbums.length" class="library-state">${copy.empty}</p><div v-else class="library-album-grid"><button v-for="album in libraryAlbums" :key="album.id" type="button" :class="{current:String(active?.id)===String(album.id)}" @click="openLibraryAlbum(album.id)"><span class="library-album-cover"><img v-if="album.cover_url" :src="coverPreview(album.cover_url)" loading="lazy" decoding="async" :alt="album.title"/><i v-else>{{ branding.name }}</i><em v-if="String(active?.id)===String(album.id)">${copy.reading}</em></span><span class="library-album-copy"><small>{{album.category}}</small><strong>{{album.title}}</strong><b>{{album.page_count}} ${copy.pages} <i>→</i></b></span></button></div></div></aside></div></div>`,
    };
    const HomeCategoryNav = {
      setup: () => ({ route, shelfKey, tabs: shelfDefs, openShelf }),
      template: `<nav v-if="route==='#/'||route.startsWith('#/shelf/')" class="home-category-nav" aria-label="${copy.shelfCategories}"><button v-for="tab in tabs" :key="tab.key" type="button" :class="{selected:shelfKey===tab.key}" @click="openShelf(tab.key)"><span>{{tab.name}}</span><small>{{tab.en}}</small></button></nav>`,
    };
    const onFullscreenChange = () => {
      isFullscreen.value = Boolean(document.fullscreenElement);
    };
    onMounted(async () => {
      document.documentElement.lang = isEnglish ? 'en' : 'zh-CN';
      document.title = branding.title;
      libraryPanelHost = document.createElement('div');
      libraryPanelHost.id = 'reader-library-portal';
      document.body.appendChild(libraryPanelHost);
      libraryPanelApp = createApp(LibraryPanel);
      libraryPanelApp.mount(libraryPanelHost);
      homeNavHost = document.createElement('div');
      homeNavHost.id = 'home-category-portal';
      document.body.appendChild(homeNavHost);
      homeNavApp = createApp(HomeCategoryNav);
      homeNavApp.mount(homeNavHost);
      try {
        await loadCategories();
      } catch (e) {
        error.value = publicError(e);
      }
      window.addEventListener('hashchange', syncRoute);
      window.addEventListener('keydown', onKey);
      document.addEventListener('fullscreenchange', onFullscreenChange);
      syncRoute();
    });
    onUnmounted(() => {
      routeSequence++; routeController?.abort();
      libraryPanelApp?.unmount();
      libraryPanelHost?.remove();
      homeNavApp?.unmount();
      homeNavHost?.remove();
      window.removeEventListener('hashchange', syncRoute);
      window.removeEventListener('keydown', onKey);
      document.removeEventListener('fullscreenchange', onFullscreenChange);
    });
    return {
      shelfDefs,
      coverPreview,
      branding, otherSite,
      shareNeedsPassword,
      shareUnavailable,
      route,
      allAlbums,
      shelfAlbums,
      active,
      pages,
      pageInput,
      zoom,
      loading,
      password,
      error,
      showThumbs,
      isFullscreen,
      shelf,
      shelfKey,
      albumByCategory,
      isReader,
      pageIndex,
      currentLabel,
      progress,
      canPrev,
      canNext,
      bookEl,
      openShelf,
      openAlbum,
      back,
      prev,
      next,
      goPage,
      choosePage,
      zoomIn,
      zoomOut,
      toggleFullscreen,
      openShare,
      touchBegin,
      touchEnd,
    };
  },
  template: `<main class="app-shell"><header v-if="!isReader" class="site-header"><a class="brand" href="#/"><img v-if="branding.logo" :src="branding.logo" :alt="branding.name" class="brand-logo"/><span>{{ branding.name }}</span><i>${
    copy.brand
  }</i></a><nav><a href="#/">${
    copy.home
  }</a><a v-if="branding.website" :href="branding.website" target="_blank" rel="noopener noreferrer">${
    copy.website
  } ↗</a><a v-if="otherSite" :href="otherSite">${
    isEnglish ? '中文' : 'English'
  }</a></nav></header><p v-if="error&&!isReader" class="site-error">{{error}}</p><section v-if="isReader&&!active&&!route.startsWith('#/share/')&&error" class="share-gate"><h1>${
    copy.unavailable
  }</h1><span>{{error}}</span><button @click="back">${
    copy.back
  }</button></section><section v-if="route.startsWith('#/share/')&&!active" class="share-gate"><div class="share-mark"><img v-if="branding.logo" :src="branding.logo" :alt="branding.name" class="brand-logo"/><span>{{ branding.name }}</span></div><p>PRIVATE ALBUM</p><h1>${
    copy.exclusive
  }</h1><span>${
    copy.privateHint
  }</span><input v-if="shareNeedsPassword" v-model="password" type="password" placeholder="${
    copy.password
  }" @keyup.enter="openShare"/><button v-if="!shareUnavailable" :disabled="loading" @click="openShare">{{loading?'${
    copy.verifying
  }':'${
    copy.open
  }'}}</button><b v-if="error">{{error}}</b></section><section v-else-if="route==='#/'" class="home"><div class="shelf-intro"><p>PUBLIC BOOKSHELVES</p><h2>{{ branding.headline }}</h2><span>{{ branding.description }}</span></div><div class="shelf-cards"><article v-for="item in albumByCategory" :key="item.key" :class="item.color" @click="openShelf(item.key)"><div class="shelf-cover"><img v-if="item.cover_url || item.albums[0]?.cover_url" :src="coverPreview(item.cover_url || item.albums[0].cover_url)" loading="lazy" decoding="async"/><div v-else class="shelf-cover-empty">{{ branding.name }}</div></div><div class="shelf-copy"><small>{{item.en}}</small><h2>{{item.name}}</h2><p>{{item.description}}</p><b>{{item.albums.length}} ${
    copy.albums
  } <i>→</i></b></div></article></div></section><section v-else-if="shelf" class="shelf-page"><div class="shelf-banner" :class="shelf.color"><button onclick="location.hash='#/'">← ${
    copy.home
  }</button><p>{{shelf.en}}</p><h1>{{shelf.name}}</h1><span>{{shelf.description}}</span></div><div class="shelf-title"><div><p>BOOKSHELF</p><h2>{{shelf.name}}</h2></div><span>{{shelfAlbums.length}} ${
    copy.books
  }</span></div><p v-if="loading" class="loading-copy">${
    copy.loading
  }</p><div v-show="!loading||shelfAlbums.length" class="album-grid"><article v-for="album in shelfAlbums" :key="album.id" @click="openAlbum(album.id)"><div class="cover-wrap"><img :src="coverPreview(album.cover_url)" loading="lazy" decoding="async" :alt="album.title"/><em>${
    copy.open
  } ↗</em></div><small>{{album.category}}</small><h2>{{album.title}}</h2><p>{{album.description}}</p><b>{{album.page_count}} ${
    copy.pages
  }</b></article></div></section><section v-else-if="active" class="reader-shell"><div class="reader-top"><button class="icon-button back-button" @click="back">←</button><div class="reader-title"><small>{{ branding.name }}</small><strong>{{active.title}}</strong></div><div class="top-actions"><button class="icon-button" @click="showThumbs=!showThumbs">▦</button><button class="icon-button" @click="toggleFullscreen">{{isFullscreen?'⊗':'⛶'}}</button></div></div><div v-if="error" class="reader-error" role="alert">{{error}}</div><div class="reader-stage" @touchstart="touchBegin" @touchend="touchEnd"><button class="edge-nav left" :class="{disabled:!canPrev}" @click="prev"><span>‹</span></button><div class="book-wrap"><div ref="bookEl" class="pageflip-book" :style="{transform:'scale('+zoom+')'}"></div></div><button class="edge-nav right" :class="{disabled:!canNext}" @click="next"><span>›</span></button><div v-if="loading" class="reader-loading">${
    copy.opening
  }</div></div><div class="reader-bottom"><div class="bottom-left"><button @click="showThumbs=!showThumbs">▦ <span>${
    copy.contents
  }</span></button><button @click="zoomOut" :disabled="zoom<=.75">−</button><span>{{Math.round(zoom*100)}}%</span><button @click="zoomIn" :disabled="zoom>=1.5">＋</button></div><div class="page-jump"><input v-model.number="pageInput" @keyup.enter="goPage" @blur="goPage"/><span>/ {{pages.length}}</span></div><div class="bottom-right"><span>{{currentLabel}} ${
    copy.pages
  }</span></div></div><div class="page-progress"><i :style="{width:progress}"></i></div><aside v-if="showThumbs" class="thumb-drawer"><div class="drawer-head"><div><small>TABLE OF CONTENTS</small><strong>{{active.title}}</strong></div><button class="icon-button" @click="showThumbs=false">×</button></div><div class="thumb-list"><button v-for="(item,index) in pages" :key="item.id||index" :class="{selected:pageIndex===index}" @click="choosePage(index)"><img :src="coverPreview(item.thumbnail_url||item.image_url)" loading="lazy" decoding="async"/><span>{{index+1}}</span></button></div></aside></section><footer v-if="!isReader && branding.copyright" class="brand-footer">{{ branding.copyright }}</footer></main>`,
};
