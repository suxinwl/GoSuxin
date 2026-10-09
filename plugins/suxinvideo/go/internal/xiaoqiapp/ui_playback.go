package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const playbackIdleTimeout = 10 * time.Minute
const playbackMIME = `video/mp4; codecs="avc1.42C01F, mp4a.40.2"`

type playbackSession struct {
	accountID       string
	dramaID         string
	workID          string
	id              string
	viewer          *viewerRecords
	tasks           []Task
	downloadIDs     []string
	prepared        map[int]bool
	expires         time.Time
	timer           *time.Timer
	cancel          context.CancelFunc
	run             uint64
	state           string
	error           string
	duration        float64
	currentIndex    int
	prefetchVersion uint64
	prefetch        *playbackPrefetch
	native          *playbackNative
	quality         int
	streamVersion   uint64
	openedAt        time.Time
	historySequence uint64
	historyRuns     map[uint64]playbackHistoryRun
}

type playbackEpisode struct {
	VIP       bool                   `json:"vip,omitempty"`
	Index     int                    `json:"index"`
	Episode   string                 `json:"episode"`
	Title     string                 `json:"title"`
	TaskID    string                 `json:"taskId,omitempty"`
	Danmaku   bool                   `json:"danmaku,omitempty"`
	ChapterID string                 `json:"chapterId,omitempty"`
	Number    int                    `json:"number"`
	Total     int                    `json:"total"`
	Source    string                 `json:"source,omitempty"`
	Sources   []playbackSourceChoice `json:"sources,omitempty"`
}

type playbackSourceChoice struct {
	Source    string `json:"source"`
	Label     string `json:"label"`
	Available bool   `json:"available"`
}

type playbackView struct {
	State    string                `json:"state"`
	Error    string                `json:"error,omitempty"`
	Run      uint64                `json:"run"`
	Duration float64               `json:"duration"`
	Prefetch *playbackPrefetchView `json:"prefetch,omitempty"`
}

func (app *UIApp) registerPlaybackRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/ui/playback/open", app.handlePlaybackOpen)
	mux.HandleFunc("/api/ui/playback/source", app.handlePlaybackSource)
	mux.HandleFunc("/api/ui/playback/prepare", app.handlePlaybackPrepare)
	mux.HandleFunc("/api/ui/playback/stream", app.handlePlaybackStream)
	mux.HandleFunc("/api/ui/playback/hls/open", app.handlePlaybackNativeOpen)
	mux.HandleFunc("/api/ui/playback/hls/index.m3u8", app.handlePlaybackNativeAsset)
	mux.HandleFunc("/api/ui/playback/hls/segment.ts", app.handlePlaybackNativeAsset)
	mux.HandleFunc("/api/ui/playback/control", app.handlePlaybackControl)
	mux.HandleFunc("/api/ui/playback/status", app.handlePlaybackStatus)
	mux.HandleFunc("/api/ui/playback/prefetch", app.handlePlaybackPrefetch)
	mux.HandleFunc("/api/ui/playback/danmaku", app.handlePlaybackDanmaku)
	mux.HandleFunc("/api/ui/playback/history", app.handlePlaybackHistory)
	mux.HandleFunc("/api/ui/playback/history/remove", app.handlePlaybackHistoryRemove)
	mux.HandleFunc("/api/ui/playback/progress", app.handlePlaybackProgress)
}

func playbackRequestAllowed(writer http.ResponseWriter, request *http.Request, method string) bool {
	writer.Header().Set("Cache-Control", "private, no-store, no-transform")
	writer.Header().Set("X-Accel-Buffering", "no")
	writer.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	if request.Method != method {
		writer.Header().Set("Allow", method)
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "请求方法不支持"})
		return false
	}
	switch request.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return true
	case "":
	default:
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "请从剧库页面发起播放"})
		return false
	}
	if origin := request.Header.Get("Origin"); origin != "" {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.User != nil || parsed.Scheme != "http" && parsed.Scheme != "https" || !originHostMatches(parsed, request.Host) {
			writeJSON(writer, http.StatusForbidden, map[string]string{"error": "不允许跨站播放请求"})
			return false
		}
	}
	return true
}

func originHostMatches(origin *url.URL, host string) bool {
	port := ":80"
	if origin.Scheme == "https" {
		port = ":443"
	}
	return strings.EqualFold(strings.TrimSuffix(origin.Host, port), strings.TrimSuffix(host, port))
}

func readPlaybackRequest(writer http.ResponseWriter, request *http.Request, destination any) bool {
	if !playbackRequestAllowed(writer, request, http.MethodPost) {
		return false
	}
	contentType, _, _ := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if contentType != "application/json" {
		writeJSON(writer, http.StatusUnsupportedMediaType, map[string]string{"error": "请求必须使用 JSON"})
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "播放请求无效"})
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "播放请求包含多余数据"})
		return false
	}
	return true
}

func (app *UIApp) handlePlaybackOpen(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		DramaID     string `json:"dramaId"`
		WorkID      string `json:"workId"`
		TaskID      string `json:"taskId"`
		Resume      bool   `json:"resume"`
		FromHistory bool   `json:"fromHistory"`
	}
	if !readPlaybackRequest(writer, request, &input) {
		return
	}
	if input.TaskID != "" && !requireDownload(writer, request) {
		return
	}
	if input.TaskID != "" && input.DramaID != "" {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "请选择剧库播放或合集播放"})
		return
	}
	viewer := requestViewer(writer, request)
	if viewer == nil {
		return
	}
	requestedWorkID := strings.TrimSpace(input.WorkID)
	if requestedWorkID == "" && strings.HasPrefix(strings.TrimSpace(input.DramaID), "work:") {
		requestedWorkID = strings.TrimSpace(input.DramaID)
	}
	if requestedWorkID != "" {
		app.mu.Lock()
		works := app.workCatalogLocked()
		work, found := workByID(works, requestedWorkID)
		if !found {
			// Media-type corrections change the derived Work hash. Resolve old
			// bookmarks before selecting the provider binding, then continue
			// with the canonical current Work ID for source switching/history.
			work, found = workForLegacyID(works, requestedWorkID)
			if found {
				requestedWorkID = work.ID
			}
		}
		if found {
			work = filterWorkSources(request.Context(), work, "")
		}
		if found && len(work.Sources) > 0 {
			input.DramaID = work.Sources[0].DramaID
		}
		app.mu.Unlock()
		if !found || len(work.Sources) == 0 {
			writeJSON(writer, http.StatusNotFound, map[string]string{"error": "此影片没有可用片源"})
			return
		}
	}
	if input.DramaID != "" && !app.requireDramaSources(writer, request, []string{input.DramaID}) {
		return
	}
	openedAt := time.Now()
	history := viewer.playbackHistory()
	previous, hasPrevious := history.get(input.DramaID)
	if input.FromHistory && (!input.Resume || !hasPrevious || input.TaskID != "") {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "此观看记录已移除，请从剧库重新选择"})
		return
	}
	var drama Drama
	var title string
	var tasks []Task
	var downloadIDs []string
	initialIndex := 1
	var collectionErr error
	app.mu.Lock()
	if input.FromHistory && previous.Mode == "collection" && downloadAllowed(request.Context()) {
		candidate := app.tasks[previous.TaskID]
		if isPlayableDownloadTask(candidate) && candidate.DramaID == previous.DramaID {
			input.TaskID = candidate.ID
		} else {
			for _, taskID := range app.taskOrder {
				candidate = app.tasks[taskID]
				if isPlayableDownloadTask(candidate) && candidate.DramaID == previous.DramaID {
					input.TaskID = candidate.ID
					break
				}
			}
		}
	}
	if input.TaskID != "" {
		if !app.requireTaskSourcesLocked(writer, request, taskSelection{IDs: []string{input.TaskID}}) {
			app.mu.Unlock()
			return
		}
		title, tasks, downloadIDs, initialIndex, collectionErr = app.collectionPlaybackTasksLocked(input.TaskID)
	} else {
		for _, candidate := range app.dramas {
			if candidate.ID == input.DramaID && input.DramaID != "" {
				drama = candidate
				break
			}
		}
	}
	app.mu.Unlock()
	if input.TaskID != "" && len(tasks) > 0 && input.Resume {
		previous, hasPrevious = history.get(tasks[0].DramaID)
	}
	if input.TaskID == "" && drama.ID == "" && input.Resume && hasPrevious {
		drama = Drama{ID: previous.DramaID, Source: previous.Source, Title: previous.Title}
	}
	if collectionErr != nil {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": app.redactError(collectionErr)})
		return
	}
	if input.TaskID == "" && drama.ID == "" {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "此短剧不在当前剧库中，请刷新剧库后重试"})
		return
	}
	if drama.ID != "" && !dramaAllowed(request.Context(), drama.ID, drama.Source) {
		writeSourceDenied(writer)
		return
	}
	for _, task := range tasks {
		if !taskSourceAllowed(request.Context(), task) {
			writeSourceDenied(writer)
			return
		}
	}
	sessionDramaID := drama.ID
	if len(tasks) > 0 {
		sessionDramaID = tasks[0].DramaID
	}
	if _, err := app.downloader.ensureFFmpeg(request.Context()); err != nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": app.redactError(err)})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 90*time.Second)
	defer cancel()
	id := randomHex(24)
	if id == "" {
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "无法创建播放会话"})
		return
	}
	session := &playbackSession{downloadIDs: downloadIDs, dramaID: sessionDramaID, id: id, viewer: viewer, state: "opening", cancel: cancel, openedAt: openedAt, historyRuns: make(map[uint64]playbackHistoryRun)}
	if !app.registerPlayback(session) {
		writeJSON(writer, http.StatusTooManyRequests, map[string]string{"error": playbackSessionLimitMessage})
		return
	}
	ready := false
	defer func() {
		if !ready {
			app.closePlayback(id)
		}
	}()
	if input.TaskID == "" {
		candidates := []Drama{drama}
		catalog := app.workCatalogSnapshot()
		if work, found := workForDramaID(catalog, drama.ID); found {
			for _, binding := range work.Sources {
				if binding.DramaID == drama.ID || !sourceAllowed(request.Context(), binding.Source) {
					continue
				}
				if candidate, ok := dramaByID(appDramas(app), binding.DramaID); ok {
					candidates = append(candidates, candidate)
				}
				if len(candidates) >= 2 {
					break
				} // one automatic alternate keeps a failed open bounded
			}
		}
		var fetchedTitle string
		var chapters []Chapter
		var err error
		for _, candidate := range candidates {
			fetchedTitle, chapters, err = app.downloader.GetDramaChapters(ctx, candidate.ID)
			if err == nil && len(chapters) > 0 {
				drama = candidate
				break
			}
			if err == nil {
				err = errors.New("站点未返回可播放的分集")
			}
		}
		if err != nil {
			writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "获取选集失败：" + app.redactError(err)})
			return
		}
		title = firstNonEmpty(fetchedTitle, drama.DisplayTitle())
		for index, chapter := range chapters {
			tasks = append(tasks, Task{DramaID: drama.ID, DramaTitle: title, Chapter: chapter, Index: index + 1, Total: len(chapters)})
		}
	}
	for _, task := range tasks {
		if !taskSourceAllowed(request.Context(), task) {
			writeSourceDenied(writer)
			return
		}
	}
	episodes := app.playbackEpisodes(request.Context(), tasks, downloadIDs)
	app.playbackMu.Lock()
	if app.playbacks[id] == session && ctx.Err() == nil {
		session.tasks = tasks
		session.downloadIDs = downloadIDs
		session.prepared = make(map[int]bool)
		session.cancel = nil
		session.state = "ready"
		app.touchPlaybackLocked(session)
		ready = true
	}
	app.playbackMu.Unlock()
	if !ready {
		writeJSON(writer, http.StatusRequestTimeout, map[string]string{"error": "播放准备超时，请重新打开本剧"})
		return
	}
	mode := "online"
	if len(downloadIDs) > 0 {
		mode = "collection"
	}
	initialPosition, resumePaused, resumeMessage := float64(0), false, ""
	if input.Resume && hasPrevious {
		initialIndex, initialPosition, resumePaused, resumeMessage = playbackHistoryResume(previous, tasks, initialIndex)
	}
	dramaID, source, _ := playbackHistoryIdentity(tasks[0].DramaID)
	workID := requestedWorkID
	if workID == "" {
		app.mu.Lock()
		if work, ok := workForDramaID(app.workCatalogLocked(), tasks[0].DramaID); ok {
			workID = work.ID
		}
		app.mu.Unlock()
	}
	app.playbackMu.Lock()
	if current := app.playbacks[id]; current != nil {
		current.workID = workID
	}
	app.playbackMu.Unlock()
	writeJSON(writer, http.StatusOK, map[string]any{"session": id, "dramaId": dramaID, "workId": workID, "source": source, "title": title, "releaseStatus": dramaReleaseStatus(drama), "vip": drama.VIP, "episodes": episodes, "mimeType": playbackMIME, "mode": mode, "initialIndex": initialIndex, "initialPosition": initialPosition, "resumePaused": resumePaused, "resumeMessage": resumeMessage})
}

func dramaByID(dramas []Drama, id string) (Drama, bool) {
	for _, drama := range dramas {
		if drama.ID == id {
			return drama, true
		}
	}
	return Drama{}, false
}

func playbackSourceLabel(source string) string {
	for _, choice := range accountSourceChoices {
		if choice.ID == source {
			return choice.Name
		}
	}
	return source
}

func (app *UIApp) playbackEpisodes(ctx context.Context, tasks []Task, downloadIDs []string) []playbackEpisode {
	app.mu.Lock()
	works := app.workCatalogLocked()
	app.mu.Unlock()
	episodes := make([]playbackEpisode, 0, len(tasks))
	for index, task := range tasks {
		number := task.Index
		if number < 1 {
			number = index + 1
		}
		total := task.Total
		if total < len(tasks) {
			total = len(tasks)
		}
		if total < number {
			total = number
		}
		episode := playbackEpisode{VIP: task.Chapter.VIP, Index: index + 1, Episode: task.Chapter.EpisodeString(number), Title: task.Chapter.Title, ChapterID: task.Chapter.ID, Number: number, Total: total, Source: dramaProviderFromTask(task)}
		_, _, episode.Danmaku = hongguoPlaybackIDs(task)
		if len(downloadIDs) > 0 && index < len(downloadIDs) {
			episode.TaskID = downloadIDs[index]
		}
		if work, ok := workForDramaID(works, task.DramaID); ok {
			for _, source := range work.Sources {
				if sourceAllowed(ctx, source.Source) && workSourceHasEpisode(source, number) {
					episode.Sources = append(episode.Sources, playbackSourceChoice{Source: source.Source, Label: playbackSourceLabel(source.Source), Available: true})
				}
			}
		}
		episodes = append(episodes, episode)
	}
	return episodes
}

func dramaProviderFromTask(task Task) string {
	if source := canonicalProviderSource(task.Chapter.Source); source != "" {
		return source
	}
	return sourceFromDramaID(task.DramaID)
}

func (app *UIApp) handlePlaybackSource(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Session  string  `json:"session"`
		Episode  int     `json:"episode"`
		Source   string  `json:"source"`
		Position float64 `json:"position"`
	}
	if !readPlaybackRequest(writer, request, &input) {
		return
	}
	if input.Episode < 1 {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "分集无效"})
		return
	}
	if input.Position < 0 || input.Position > 24*60*60 || math.IsNaN(input.Position) || math.IsInf(input.Position, 0) {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "播放位置无效"})
		return
	}
	if !app.requirePlaybackOwner(writer, request, input.Session) {
		return
	}
	source := canonicalProviderSource(input.Source)
	if source == "" || !sourceAllowed(request.Context(), source) {
		writeSourceDenied(writer)
		return
	}
	app.playbackMu.Lock()
	session := app.playbacks[input.Session]
	if session == nil || input.Episode > len(session.tasks) || len(session.downloadIDs) > 0 {
		app.playbackMu.Unlock()
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "当前播放不支持切换片源"})
		return
	}
	current := session.tasks[input.Episode-1]
	previousCancel := session.cancel
	app.playbackMu.Unlock()

	app.mu.Lock()
	works := app.workCatalogLocked()
	app.mu.Unlock()
	work, ok := workForDramaID(works, current.DramaID)
	if !ok {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "未找到统一影片"})
		return
	}
	var binding WorkSource
	for _, candidate := range work.Sources {
		if candidate.Source == source {
			binding = candidate
			break
		}
	}
	if binding.DramaID == "" {
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "该片源没有此影片"})
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 90*time.Second)
	title, chapters, err := app.downloader.GetDramaChapters(ctx, binding.DramaID)
	cancel()
	// The browser aborts an obsolete switch when the user selects another
	// source.  Do not publish a response that finished after that request was
	// canceled, even if the provider ignored context cancellation.
	if request.Context().Err() != nil {
		return
	}
	if err != nil || len(chapters) == 0 {
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "片源暂时无法解析，请选择其他来源"})
		return
	}
	if input.Episode > len(chapters) {
		writeJSON(writer, http.StatusBadGateway, map[string]string{"error": "该片源没有当前分集，请选择其他来源"})
		return
	}
	oldEpisode := current.Chapter.EpisodeString(current.Index)
	tasks := make([]Task, 0, len(chapters))
	selected := current.Index
	if selected < 1 {
		selected = 1
	}
	if selected > len(chapters) {
		selected = len(chapters)
	}
	for index, chapter := range chapters {
		number := index + 1
		tasks = append(tasks, Task{DramaID: binding.DramaID, DramaTitle: firstNonEmpty(title, work.Title), Chapter: chapter, Index: number, Total: len(chapters)})
		if oldEpisode != "" && chapter.EpisodeString(number) == oldEpisode {
			selected = number
		}
	}
	app.playbackMu.Lock()
	session = app.playbacks[input.Session]
	if session == nil {
		app.playbackMu.Unlock()
		return
	}
	// A second switch may have committed while this provider request was in
	// flight.  Refuse to overwrite its newer task set with stale chapters.
	if input.Episode > len(session.tasks) || session.tasks[input.Episode-1].DramaID != current.DramaID {
		app.playbackMu.Unlock()
		writeJSON(writer, http.StatusConflict, map[string]string{"error": "播放片源已更新，请重试"})
		return
	}
	session.tasks = tasks
	session.dramaID = binding.DramaID
	session.workID = work.ID
	session.error = ""
	app.touchPlaybackLocked(session)
	app.playbackMu.Unlock()
	if previousCancel != nil {
		previousCancel()
	}
	// Return the canonical provider identity as well as the refreshed episode
	// list.  The player keeps this identity for reconnect/recovery requests;
	// without it, a source switch would continue reopening the old provider
	// after the current session was refreshed.
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "source": source, "dramaId": binding.DramaID, "workId": work.ID, "episode": selected, "position": input.Position, "episodes": app.playbackEpisodes(request.Context(), tasks, nil)})
}

func (app *UIApp) touchPlaybackLocked(session *playbackSession) {
	session.expires = time.Now().Add(playbackIdleTimeout)
	if session.timer != nil {
		session.timer.Reset(playbackIdleTimeout)
	}
}

func (app *UIApp) expirePlayback(id string) {
	app.playbackMu.Lock()
	session := app.playbacks[id]
	if session != nil && time.Now().Before(session.expires) {
		session.timer.Reset(time.Until(session.expires))
		app.playbackMu.Unlock()
		return
	}
	if session != nil {
		delete(app.playbacks, id)
	}
	app.playbackMu.Unlock()
	releasePlaybackSession(session)
}

func (app *UIApp) closePlayback(id string) {
	app.playbackMu.Lock()
	session := app.playbacks[id]
	if session != nil {
		delete(app.playbacks, id)
		session.timer.Stop()
	}
	app.playbackMu.Unlock()
	releasePlaybackSession(session)
}

func (app *UIApp) closePlaybacks() {
	app.playbackMu.Lock()
	sessions := app.playbacks
	app.playbacks = nil
	for _, session := range sessions {
		session.timer.Stop()
	}
	app.playbackMu.Unlock()
	for _, session := range sessions {
		releasePlaybackSession(session)
	}
}

func (app *UIApp) playbackStatus(id string, touch bool) (playbackView, bool) {
	app.playbackMu.Lock()
	defer app.playbackMu.Unlock()
	session := app.playbacks[id]
	if session == nil {
		return playbackView{}, false
	}
	if touch {
		app.touchPlaybackLocked(session)
	}
	view := playbackView{State: session.state, Error: session.error, Run: session.run, Duration: session.duration}
	if session.native != nil {
		state, err := session.native.state()
		view.State = state
		if err != nil {
			view.Error = app.redactError(err)
		}
	}
	if session.prefetch != nil {
		view.Prefetch = session.prefetch.view()
	}
	return view, true
}

func (app *UIApp) handlePlaybackControl(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Session  string                   `json:"session"`
		Action   string                   `json:"action"`
		Progress *playbackHistoryProgress `json:"progress,omitempty"`
	}
	if !readPlaybackRequest(writer, request, &input) {
		return
	}
	if !app.requirePlaybackOwner(writer, request, input.Session) {
		return
	}
	if input.Action == "close" {
		var progressError string
		if input.Progress != nil {
			if _, _, err := app.recordPlaybackProgress(contextViewer(request.Context()), input.Session, *input.Progress); err != nil {
				progressError = publicError(err).Error()
			}
		}
		app.closePlayback(input.Session)
		writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "historyError": progressError})
		return
	}
	if input.Action != "heartbeat" {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "不支持此播放操作"})
		return
	}
	if state, ok := app.playbackStatus(input.Session, true); ok {
		writeJSON(writer, http.StatusOK, state)
	} else {
		writeJSON(writer, http.StatusGone, map[string]string{"error": "播放会话已过期，请重新打开本剧"})
	}
}

func (app *UIApp) handlePlaybackStatus(writer http.ResponseWriter, request *http.Request) {
	if !playbackRequestAllowed(writer, request, http.MethodGet) {
		return
	}
	if !app.requirePlaybackOwner(writer, request, request.URL.Query().Get("session")) {
		return
	}
	if state, ok := app.playbackStatus(request.URL.Query().Get("session"), false); ok {
		writeJSON(writer, http.StatusOK, state)
	} else {
		writeJSON(writer, http.StatusGone, map[string]string{"error": "播放会话已过期，请重新打开本剧"})
	}
}

func (app *UIApp) handlePlaybackStream(writer http.ResponseWriter, request *http.Request) {
	if !playbackRequestAllowed(writer, request, http.MethodGet) {
		return
	}
	query := request.URL.Query()
	index, indexErr := strconv.Atoi(query.Get("episode"))
	offset, offsetErr := strconv.ParseFloat(firstNonEmpty(query.Get("start"), "0"), 64)
	if indexErr != nil || index < 1 || offsetErr != nil || math.IsNaN(offset) || math.IsInf(offset, 0) || offset < 0 || offset > 24*60*60 {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "集数或播放位置无效"})
		return
	}
	quality, qualityErr := parsePlaybackQuality(query.Get("quality"))
	version, versionErr := strconv.ParseUint(firstNonEmpty(query.Get("version"), "0"), 10, 64)
	if qualityErr != nil || versionErr != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "清晰度或播放请求编号无效"})
		return
	}
	ctx := context.WithValue(request.Context(), playbackRemuxKey{}, query.Get("remux") == "1")
	run, status, err := app.beginPlayback(ctx, query.Get("session"), index, offset, quality, version, false)
	if err != nil {
		writeJSON(writer, status, map[string]string{"error": err.Error()})
		return
	}
	defer run.stop()
	writer = &playbackActivityWriter{ResponseWriter: writer, app: app, run: run}
	startupTimer := time.AfterFunc(90*time.Second, run.cancel)
	defer startupTimer.Stop()
	ready := func(duration float64) {
		startupTimer.Stop()
		app.playbackRunReady(run, duration)
	}
	used, err := app.servePrefetchedPlayback(run.ctx, writer, run.cache, run.run, ready)
	if !used {
		err = app.streamPlayback(run.ctx, run.cancel, writer, run.task, run.downloadID, offset, run.run, ready)
	}
	app.finishPlaybackRun(run, err, request.Context().Err() != nil)
}
