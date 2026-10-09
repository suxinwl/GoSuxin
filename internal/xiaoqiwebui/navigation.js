(() => {
  const pages = ['library', 'following', 'downloads', 'users', 'detail', 'play'];
  const native = new WeakMap();
  const routeFromLocation = () => {
    // Keep the public movie listing URL compatible with the reference site.
    // It uses the same catalogue view as the local category route.
    if (location.pathname === '/movie') return {kind: 'category', value: '电影', path: '/movie'};
    const match = location.pathname.match(/^\/(play|category)\/([^/]+)$/);
    if (match) {
      try {return {kind: match[1], value: decodeURIComponent(match[2])};}
      catch (_) {return {kind: match[1], value: match[2]};}
    }
    return {kind: 'page'};
  };
  let currentRoute = routeFromLocation();
  // A play URL is a standalone page. Keep the list state in its history
  // snapshot, while the shell renders only the player view.
  let currentPage = currentRoute.kind === 'play' ? 'play' : currentRoute.kind === 'category' ? 'library' : (pages.includes(location.hash.slice(1)) ? location.hash.slice(1) : 'library');
  let active = null, pendingBack = false, pendingOpen = null, pendingNavigation = null, pendingClose = '', listener = null, routeListeners = [];
  let entrySequence = 0, currentEntry = history.state?.xiaoqiEntry || 'initial', snapshotHandler = null;
  const snapshots = new Map();
  const initialSnapshot = history.state?.snapshot;
  const state = (dialog = '', layer = '', route = currentRoute) => ({xiaoqi: true, page: currentPage, dialog, layer, route});
  const routeURL = route => {
    if (route?.kind === 'play') return '/play/' + encodeURIComponent(route.value) + location.search;
    if (route?.kind === 'category') return route.path === '/movie' ? '/movie' + location.search : '/category/' + encodeURIComponent(route.value) + location.search;
    const accountPath = location.pathname === '/login' || location.pathname === '/register';
    return (accountPath ? location.pathname : '/') + location.search + '#' + currentPage;
  };
  const url = () => routeURL(currentRoute);
  history.replaceState({...state(), xiaoqiEntry: currentEntry, snapshot: initialSnapshot}, '', url());

  function captureSnapshot(updateHistory = true) {
    if (!snapshotHandler?.capture) return;
    let snapshot;
    try {snapshot = snapshotHandler.capture();} catch (_) {return;}
    snapshots.set(currentEntry, snapshot);
    if (updateHistory) history.replaceState({...history.state, snapshot}, '');
  }

  function writeState(method, dialog = '', layer = '') {
    if (method === 'pushState') currentEntry = 'entry-' + Date.now() + '-' + ++entrySequence;
    history[method]({...state(dialog, layer), xiaoqiEntry: currentEntry}, '', url());
  }

  function notifyRoute(restored = false) {
    routeListeners.slice().forEach(callback => {
      try {callback?.({...currentRoute}, {restored});} catch (_) {}
    });
    window.dispatchEvent(new CustomEvent('xiaoqiroutechange', {detail: {...currentRoute, page: currentPage, restored}}));
  }

  function backdropState() {
    document.body.classList.toggle('modal-open', Boolean(active));
    const toast = document.getElementById('messageText');
    if (toast) (active || document.body).appendChild(toast);
  }

  function finish(dialog) {
    if (dialog?.open) native.get(dialog).close();
    if (active === dialog) active = null;
    backdropState();
  }

  function open(dialog) {
    if (typeof dialog === 'string') dialog = document.getElementById(dialog);
    if (!dialog || dialog.open) return;
    captureSnapshot();
    if (active && history.state?.layer) close(active);
    const replace = Boolean(active);
    if (active) finish(active);
    active = dialog;
    native.get(dialog).show();
    backdropState();
    if (pendingBack) pendingOpen = dialog;
    else writeState(replace ? 'replaceState' : 'pushState', dialog.id);
  }

  function close(dialog) {
    if (!dialog?.open) return;
    const managed = (history.state?.xiaoqi || history.state?.duanju) && history.state.dialog === dialog.id;
    const count = history.state?.layer ? 2 : 1;
    if (pendingOpen === dialog) pendingOpen = null;
    finish(dialog);
    if (managed && !pendingBack) {
      pendingBack = true;
      history.go(-count);
    } else if (managed && pendingBack) pendingClose = dialog.id;
  }

  function navigate(page) {
    if (!pages.includes(page)) return;
    navigateRoute({kind: 'page'}, page);
  }

  function navigateRoute(route, page) {
    // Standalone sign-in pages have not initialized catalogue/admin modules.
    if (document.body.classList.contains('account-page')) {
      const path = route.kind === 'category' ? '/category/' + encodeURIComponent(route.value) : route.kind === 'play' ? '/play/' + encodeURIComponent(route.value) : '/#' + page;
      location.assign(path);
      return;
    }
    if (active) close(active);
    if (pendingBack) {pendingNavigation = {route, page}; return;}
    if (page === currentPage && currentRoute.kind === route.kind && currentRoute.value === route.value) return;
    captureSnapshot();
    currentPage = page;
    currentRoute = route;
    writeState('pushState');
    listener?.(currentPage);
    notifyRoute();
  }

  function navigateCategory(value) {
    const category = String(value || '').trim();
    if (!category) return navigate('library');
    navigateRoute({kind: 'category', value: category}, 'library');
  }

  function navigatePlay(id) {
    const value = String(id || '').trim();
    if (!value) return;
    navigateRoute({kind: 'play', value}, 'play');
  }

  function layer(name, visible) {
    if (!active) return;
    if (visible && history.state?.layer !== name) {
      captureSnapshot();
      writeState(history.state?.layer ? 'replaceState' : 'pushState', active.id, name);
    }
    if (!visible && history.state?.layer === name && !pendingBack) {
      pendingBack = true;
      history.back();
    }
  }

  for (const dialog of document.querySelectorAll('dialog')) {
    native.set(dialog, {show: dialog.showModal.bind(dialog), close: dialog.close.bind(dialog)});
    dialog.showModal = () => open(dialog);
    dialog.close = () => close(dialog);
    dialog.addEventListener('cancel', event => {
      if (event.defaultPrevented) return;
      event.preventDefault();
      if (history.state?.layer) history.back(); else close(dialog);
    });
    dialog.addEventListener('click', event => {
      if (event.target !== dialog) return;
      const bounds = dialog.getBoundingClientRect();
      if (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom) close(dialog);
    });
  }
  document.querySelectorAll('[data-close]').forEach(button => button.addEventListener('click', () => close(button.closest('dialog'))));
  window.addEventListener('popstate', event => {
    const next = event.state || {};
    captureSnapshot(false);
    currentEntry = next.xiaoqiEntry || 'entry-' + Date.now() + '-' + ++entrySequence;
    currentRoute = routeFromLocation();
    currentPage = currentRoute.kind === 'play' ? 'play' : currentRoute.kind === 'category' ? 'library' : pages.includes(next.page) ? next.page : pages.includes(location.hash.slice(1)) ? location.hash.slice(1) : 'library';
    if (pendingBack) {
      pendingBack = false;
      if (pendingClose && next.dialog === pendingClose) {
        pendingClose = '';
        pendingBack = true;
        history.back();
        return;
      }
      pendingClose = '';
      if (pendingOpen?.open) {
        active = pendingOpen;
        pendingOpen = null;
        writeState('pushState', active.id);
        backdropState();
        return;
      }
      pendingOpen = null;
      if (pendingNavigation) {
        const target = pendingNavigation;
        pendingNavigation = null;
        navigateRoute(target.route, target.page);
        return;
      }
    }
    if (active && next.dialog !== active.id) finish(active);
    if (next.dialog && !active) {
      const dialog = document.getElementById(next.dialog);
      if (dialog && native.has(dialog)) {
        active = dialog;
        native.get(dialog).show();
        backdropState();
      }
    }
    listener?.(currentPage);
    notifyRoute(true);
    document.dispatchEvent(new CustomEvent('dialoglayerchange', {detail: next.layer || ''}));
    const snapshot = snapshots.has(currentEntry) ? snapshots.get(currentEntry) : next.snapshot;
    if (snapshot !== undefined) {
      try {snapshotHandler?.restore?.(snapshot);} catch (_) {}
    }
    if (next.dialog && !active) writeState('replaceState');
  });
  window.XiaoqiDialogs = {
    open, close, navigate, navigateCategory, navigatePlay, layer,
    page: () => currentPage,
    route: () => ({...currentRoute}),
    registerSnapshot: handler => {
      snapshotHandler = handler;
      if (initialSnapshot !== undefined) {
        try {handler?.restore?.(initialSnapshot);} catch (_) {}
      }
      return () => {if (snapshotHandler === handler) snapshotHandler = null;};
    },
    listen: callback => {listener = callback;},
    listenRoute: callback => {
      if (typeof callback !== 'function') return;
      routeListeners.push(callback);
      return () => {routeListeners = routeListeners.filter(item => item !== callback);};
    }
  };
})();
