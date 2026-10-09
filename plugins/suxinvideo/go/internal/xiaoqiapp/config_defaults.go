package app

import (
	"errors"
	"os"
)

const userAgent = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.6 Mobile/15E148 Safari/604.1"

type Config struct {
	adminUsername         string
	adminPassword         string
	adminUserExplicit     bool
	adminPasswordExplicit bool
	settingsLoaded        bool
	dataDir               string
	outputDirSetting      string
	OutputDir             string            `json:"outputDir"`
	FFmpeg                string            `json:"ffmpeg"`
	Concurrency           int               `json:"concurrency"`
	RequestConcurrency    int               `json:"requestConcurrency"`
	RequestIntervalMS     int               `json:"requestIntervalMs"`
	MaxPagesPerSort       int               `json:"maxPagesPerSort"`
	PageSize              int               `json:"pageSize"`
	Retries               int               `json:"retries"`
	SkipBytes             int64             `json:"skipBytes"`
	InsecureTLS           bool              `json:"insecureTLS"`
	FullCategories        bool              `json:"fullCategories"`
	DisableAdFilter       bool              `json:"disableAdFilter"`
	ProxyURL              string            `json:"proxyURL,omitempty"`
	ProxySubscriptionURL  string            `json:"proxySubscriptionURL,omitempty"`
	HuangguoAIURL         string            `json:"huangguoAIURL,omitempty"`
	HuangguoVideoURL      string            `json:"huangguoVideoURL,omitempty"`
	HuangdouURL           string            `json:"huangdouURL,omitempty"`
	HongguoURL            string            `json:"hongguoURL,omitempty"`
	SourceURLs            map[string]string `json:"sourceURLs,omitempty"`
	SourcePriority        []string          `json:"sourcePriority,omitempty"`
}

func defaultConfig() Config {
	return Config{
		OutputDir: defaultOutputDir(), FFmpeg: "ffmpeg", Concurrency: 2,
		RequestConcurrency: 2, RequestIntervalMS: 500, MaxPagesPerSort: 20,
		PageSize: 50,
		Retries:  3, SkipBytes: 512 * 1024,
		SourceURLs: make(map[string]string), SourcePriority: append([]string(nil), workSourcePriority...),
	}
}

func defaultOutputDir() string { return "小柒影视下载" }

func applyConfigEnvironment(config *Config) {
	if config.SourceURLs == nil {
		config.SourceURLs = make(map[string]string)
	}
	for variable, target := range map[string]*string{
		"XIAOQI_HONGGUO_URL":            &config.HongguoURL,
		"XIAOQI_HUANGDOU_URL":           &config.HuangdouURL,
		"XIAOQI_HUANGGUO_AI_URL":        &config.HuangguoAIURL,
		"XIAOQI_HUANGGUO_VIDEO_URL":     &config.HuangguoVideoURL,
		"XIAOQI_PROXY_URL":              &config.ProxyURL,
		"XIAOQI_PROXY_SUBSCRIPTION_URL": &config.ProxySubscriptionURL,
		"XIAOQI_OUTPUT_DIR":             &config.OutputDir,
		"XIAOQI_FFMPEG":                 &config.FFmpeg,
	} {
		if value := os.Getenv(variable); value != "" {
			*target = value
		}
	}
	if value := os.Getenv("XIAOQI_FULL_CATEGORIES"); value == "1" || value == "true" {
		config.FullCategories = true
	}
	if value := os.Getenv("XIAOQI_DISABLE_AD_FILTER"); value == "1" || value == "true" {
		config.DisableAdFilter = true
	}
	envSourceMap := map[string]string{
		"XIAOQI_LZ_URL":  sourceLZ,
		"XIAOQI_FF_URL":  sourceFF,
		"XIAOQI_WJ_URL":  sourceWJ,
		"XIAOQI_BF_URL":  sourceBF,
		"XIAOQI_HN_URL":  sourceHN,
		"XIAOQI_SD_URL":  sourceSD,
		"XIAOQI_BD_URL":  sourceBD,
		"XIAOQI_XL_URL":  sourceXL,
		"XIAOQI_YQK_URL": sourceYQK,
	}
	for envVar, src := range envSourceMap {
		if val := os.Getenv(envVar); val != "" {
			config.SourceURLs[src] = val
		}
	}
	if value := os.Getenv("XIAOQI_4KVM_URL"); value != "" {
		config.SourceURLs[source4KVM] = value
	}
}

func (config Config) validate() error {
	if _, err := configuredProxy(config.ProxyURL); err != nil {
		return err
	}
	if err := validateProxySubscriptionURL(config.ProxySubscriptionURL); err != nil {
		return err
	}
	if config.HongguoURL != "" && !isProviderHTTPMediaURL(config.HongguoURL) {
		return errors.New("红果站点地址必须是有效的 HTTP/HTTPS URL")
	}
	return nil
}
