package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type configDocument struct {
	Version  int               `json:"version"`
	Download downloadSettings  `json:"download"`
	Network  networkSettings   `json:"network"`
	Sources  *sourceSettings   `json:"sources,omitempty"`
	Advanced *advancedSettings `json:"advanced,omitempty"`
}

type downloadSettings struct {
	Directory   string `json:"directory"`
	Concurrency int    `json:"concurrency"`
	FFmpeg      string `json:"ffmpeg,omitempty"`
}

type networkSettings struct {
	Proxy              string `json:"proxy"`
	ProxySubscription  string `json:"proxySubscription,omitempty"`
	RequestConcurrency int    `json:"requestConcurrency"`
	RequestIntervalMS  int    `json:"requestIntervalMs"`
}

type sourceSettings struct {
	Priority      []string      `json:"priority,omitempty"`
	Hongguo       *siteSettings `json:"hongguo,omitempty"`
	LZ            *siteSettings `json:"lz,omitempty"`
	FF            *siteSettings `json:"ff,omitempty"`
	WJ            *siteSettings `json:"wj,omitempty"`
	BF            *siteSettings `json:"bf,omitempty"`
	HN            *siteSettings `json:"hn,omitempty"`
	SD            *siteSettings `json:"sd,omitempty"`
	BD            *siteSettings `json:"bd,omitempty"`
	XL            *siteSettings `json:"xl,omitempty"`
	YQK           *siteSettings `json:"yqk,omitempty"`
	KVM           *siteSettings `json:"4kvm,omitempty"`
	Huangdou      *siteSettings `json:"huangdou,omitempty"`
	HuangguoAI    *siteSettings `json:"huangguoAI,omitempty"`
	HuangguoVideo *siteSettings `json:"huangguoVideo,omitempty"`
}

type siteSettings struct {
	BaseURL string `json:"baseURL,omitempty"`
}

type advancedSettings struct {
	MaxPagesPerSort int   `json:"maxPagesPerSort,omitempty"`
	PageSize        int   `json:"pageSize,omitempty"`
	Retries         int   `json:"retries,omitempty"`
	SkipBytes       int64 `json:"skipBytes,omitempty"`
	InsecureTLS     bool  `json:"insecureTLS,omitempty"`
	FullCategories  bool  `json:"fullCategories,omitempty"`
	DisableAdFilter bool  `json:"disableAdFilter,omitempty"`
}

func documentFromConfig(cfg Config) configDocument {
	defaults := defaultConfig()
	document := configDocument{
		Version:  1,
		Download: downloadSettings{Directory: firstNonEmpty(cfg.outputDirSetting, cfg.OutputDir), Concurrency: cfg.Concurrency},
		Network:  networkSettings{Proxy: firstNonEmpty(cfg.ProxyURL, "auto"), ProxySubscription: cfg.ProxySubscriptionURL, RequestConcurrency: cfg.RequestConcurrency, RequestIntervalMS: cfg.RequestIntervalMS},
	}
	if cfg.FFmpeg != "" && cfg.FFmpeg != defaults.FFmpeg {
		document.Download.FFmpeg = cfg.FFmpeg
	}
	sources := sourceSettings{}
	if cfg.HongguoURL != "" {
		sources.Hongguo = &siteSettings{BaseURL: cfg.HongguoURL}
	}
	if cfg.SourceURLs != nil {
		if u := cfg.SourceURLs[sourceLZ]; u != "" {
			sources.LZ = &siteSettings{BaseURL: u}
		}
		if u := cfg.SourceURLs[sourceFF]; u != "" {
			sources.FF = &siteSettings{BaseURL: u}
		}
		if u := cfg.SourceURLs[sourceWJ]; u != "" {
			sources.WJ = &siteSettings{BaseURL: u}
		}
		if u := cfg.SourceURLs[sourceBF]; u != "" {
			sources.BF = &siteSettings{BaseURL: u}
		}
		if u := cfg.SourceURLs[sourceHN]; u != "" {
			sources.HN = &siteSettings{BaseURL: u}
		}
		if u := cfg.SourceURLs[sourceSD]; u != "" {
			sources.SD = &siteSettings{BaseURL: u}
		}
		if u := cfg.SourceURLs[sourceBD]; u != "" {
			sources.BD = &siteSettings{BaseURL: u}
		}
		if u := cfg.SourceURLs[sourceXL]; u != "" {
			sources.XL = &siteSettings{BaseURL: u}
		}
		if u := cfg.SourceURLs[sourceYQK]; u != "" {
			sources.YQK = &siteSettings{BaseURL: u}
		}
		if u := cfg.SourceURLs[source4KVM]; u != "" {
			sources.KVM = &siteSettings{BaseURL: u}
		}
	}
	if cfg.HuangdouURL != "" {
		sources.Huangdou = &siteSettings{BaseURL: cfg.HuangdouURL}
	}
	if cfg.HuangguoAIURL != "" {
		sources.HuangguoAI = &siteSettings{BaseURL: cfg.HuangguoAIURL}
	}
	if cfg.HuangguoVideoURL != "" {
		sources.HuangguoVideo = &siteSettings{BaseURL: cfg.HuangguoVideoURL}
	}
	if sources.Hongguo != nil || sources.LZ != nil || sources.FF != nil || sources.WJ != nil || sources.BF != nil || sources.HN != nil || sources.SD != nil || sources.BD != nil || sources.XL != nil || sources.YQK != nil || sources.KVM != nil || sources.Huangdou != nil || sources.HuangguoAI != nil || sources.HuangguoVideo != nil || len(sources.Priority) > 0 {
		sources.Priority = normalizeSourcePriority(cfg.SourcePriority)
		document.Sources = &sources
	}
	advanced := advancedSettings{InsecureTLS: cfg.InsecureTLS, FullCategories: cfg.FullCategories, DisableAdFilter: cfg.DisableAdFilter}
	if cfg.MaxPagesPerSort != defaults.MaxPagesPerSort {
		advanced.MaxPagesPerSort = cfg.MaxPagesPerSort
	}
	if cfg.PageSize != defaults.PageSize {
		advanced.PageSize = cfg.PageSize
	}
	if cfg.Retries != defaults.Retries {
		advanced.Retries = cfg.Retries
	}
	if cfg.SkipBytes != defaults.SkipBytes {
		advanced.SkipBytes = cfg.SkipBytes
	}
	if advanced != (advancedSettings{}) {
		document.Advanced = &advanced
	}
	return document
}

func (document configDocument) config() (Config, error) {
	if document.Version != 1 {
		return Config{}, fmt.Errorf("不支持的配置版本 %d，请使用兼容的程序版本", document.Version)
	}
	cfg := defaultConfig()
	cfg.OutputDir = document.Download.Directory
	cfg.Concurrency = document.Download.Concurrency
	cfg.FFmpeg = firstNonEmpty(document.Download.FFmpeg, cfg.FFmpeg)
	cfg.ProxyURL = document.Network.Proxy
	cfg.ProxySubscriptionURL = document.Network.ProxySubscription
	cfg.RequestConcurrency = document.Network.RequestConcurrency
	cfg.RequestIntervalMS = document.Network.RequestIntervalMS
	if sources := document.Sources; sources != nil {
		cfg.SourcePriority = normalizeSourcePriority(sources.Priority)
		if sources.Hongguo != nil {
			cfg.HongguoURL = sources.Hongguo.BaseURL
		}
		if cfg.SourceURLs == nil {
			cfg.SourceURLs = make(map[string]string)
		}
		if sources.LZ != nil {
			cfg.SourceURLs[sourceLZ] = sources.LZ.BaseURL
		}
		if sources.FF != nil {
			cfg.SourceURLs[sourceFF] = sources.FF.BaseURL
		}
		if sources.WJ != nil {
			cfg.SourceURLs[sourceWJ] = sources.WJ.BaseURL
		}
		if sources.BF != nil {
			cfg.SourceURLs[sourceBF] = sources.BF.BaseURL
		}
		if sources.HN != nil {
			cfg.SourceURLs[sourceHN] = sources.HN.BaseURL
		}
		if sources.SD != nil {
			cfg.SourceURLs[sourceSD] = sources.SD.BaseURL
		}
		if sources.BD != nil {
			cfg.SourceURLs[sourceBD] = sources.BD.BaseURL
		}
		if sources.XL != nil {
			cfg.SourceURLs[sourceXL] = sources.XL.BaseURL
		}
		if sources.YQK != nil {
			cfg.SourceURLs[sourceYQK] = sources.YQK.BaseURL
		}
		if sources.KVM != nil {
			cfg.SourceURLs[source4KVM] = sources.KVM.BaseURL
		}
		if sources.Huangdou != nil {
			cfg.HuangdouURL = sources.Huangdou.BaseURL
		}
		if sources.HuangguoAI != nil {
			cfg.HuangguoAIURL = sources.HuangguoAI.BaseURL
		}
		if sources.HuangguoVideo != nil {
			cfg.HuangguoVideoURL = sources.HuangguoVideo.BaseURL
		}
	}
	if advanced := document.Advanced; advanced != nil {
		if advanced.MaxPagesPerSort > 0 {
			cfg.MaxPagesPerSort = advanced.MaxPagesPerSort
		}
		if advanced.PageSize > 0 {
			cfg.PageSize = advanced.PageSize
		}
		if advanced.Retries > 0 {
			cfg.Retries = advanced.Retries
		}
		if advanced.SkipBytes > 0 {
			cfg.SkipBytes = advanced.SkipBytes
		}
		cfg.InsecureTLS = advanced.InsecureTLS
		cfg.FullCategories = advanced.FullCategories
		cfg.DisableAdFilter = advanced.DisableAdFilter
	}
	settings := runtimeSettings{Concurrency: cfg.Concurrency, RequestConcurrency: cfg.RequestConcurrency, RequestIntervalMS: cfg.RequestIntervalMS, ProxyURL: &cfg.ProxyURL, ProxySubscriptionURL: &cfg.ProxySubscriptionURL, OutputDir: &cfg.OutputDir}
	if err := settings.validate(); err != nil {
		return Config{}, err
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func readConfigDocument(path string) (configDocument, error) {
	document := documentFromConfig(defaultConfig())
	file, err := os.Open(path)
	if err != nil {
		return document, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return document, err
	}
	if info.Size() > 1<<20 {
		return document, errors.New("配置文件超过 1 MB")
	}
	decoder := json.NewDecoder(io.LimitReader(file, (1<<20)+1))
	content := &document
	if err := decoder.Decode(&content); err != nil {
		return document, fmt.Errorf("配置文件格式无效: %w", err)
	}
	if content == nil {
		return document, errors.New("配置文件必须是 JSON 对象")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return document, errors.New("配置文件含有多余内容")
	}
	if _, err := document.config(); err != nil {
		return document, err
	}
	return document, nil
}

func writeConfigDocument(path string, document configDocument) error {
	if _, err := document.config(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".config-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(document)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), path)
}

func loadApplicationConfig(dataDirectory, importPath, outputOverride string) (Config, error) {
	dataDirectory, err := filepath.Abs(dataDirectory)
	if err != nil {
		return Config{}, err
	}
	path := runtimeSettingsPath(dataDirectory)
	document, err := readConfigDocument(path)
	if errors.Is(err, os.ErrNotExist) {
		cfg, legacyDirectories, importErr := importLegacyConfig(firstNonEmpty(importPath, "config.json"), outputOverride)
		if importErr != nil {
			return Config{}, importErr
		}
		document = documentFromConfig(cfg)
		if _, err := document.config(); err != nil {
			return Config{}, err
		}
		if err := migrateLegacyData(dataDirectory, legacyDirectories); err != nil {
			return Config{}, err
		}
		if err := writeConfigDocument(path, document); err != nil {
			return Config{}, fmt.Errorf("首次生成配置失败: %w", err)
		}
	} else if err != nil {
		return Config{}, fmt.Errorf("无法读取 %s，原文件未修改: %w", path, err)
	}
	cfg, err := document.config()
	if err != nil {
		return Config{}, err
	}
	cfg.settingsLoaded = true
	cfg.dataDir = dataDirectory
	applyConfigEnvironment(&cfg)
	if outputOverride != "" {
		cfg.OutputDir = outputOverride
	}
	return cfg, nil
}

func importLegacyConfig(path, outputOverride string) (Config, []string, error) {
	cfg := defaultConfig()
	body, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(body, &cfg); err != nil {
			return cfg, nil, fmt.Errorf("旧配置格式错误，未覆盖原文件: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return cfg, nil, err
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = defaultOutputDir()
	}
	if cfg.RequestConcurrency <= 0 {
		cfg.RequestConcurrency = 2
	}
	if cfg.RequestIntervalMS <= 0 {
		cfg.RequestIntervalMS = 500
	}
	directories := []string{firstNonEmpty(outputOverride, cfg.OutputDir), cfg.OutputDir}
	candidates := []string{filepath.Join(filepath.Dir(path), "xiaoqi-settings.json")}
	for _, directory := range directories {
		candidates = append(candidates, filepath.Join(directory, "settings.json"))
	}
	for _, candidate := range candidates {
		body, err = os.ReadFile(candidate)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return cfg, nil, err
		}
		var settings runtimeSettings
		if err := json.Unmarshal(body, &settings); err != nil {
			return cfg, nil, fmt.Errorf("旧设置格式错误: %w", err)
		}
		if err := settings.validate(); err != nil {
			return cfg, nil, err
		}
		applyRuntimeSettings(&cfg, settings)
		break
	}
	return cfg, append(directories, cfg.OutputDir, filepath.Dir(path)), nil
}

func migrateLegacyData(directory string, candidates []string) error {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"library.json", "ui-state.json", "failed.json"} {
		target := filepath.Join(directory, name)
		if _, err := os.Stat(target); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		for _, candidate := range candidates {
			if sameDirectory(directory, candidate) {
				continue
			}
			original := filepath.Join(candidate, name)
			body, err := os.ReadFile(original)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return err
			}
			if !json.Valid(body) {
				return fmt.Errorf("旧数据 %s 格式无效，原文件未修改", original)
			}
			file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if errors.Is(err, os.ErrExist) {
				break
			}
			if err != nil {
				return err
			}
			_, err = file.Write(body)
			if err == nil {
				err = file.Sync()
			}
			closeErr := file.Close()
			if err != nil || closeErr != nil {
				_ = os.Remove(target)
				return errors.Join(err, closeErr)
			}
			break
		}
	}
	return nil
}
