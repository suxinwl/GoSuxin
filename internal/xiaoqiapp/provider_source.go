package app

import (
	"net/http"
	"strings"
)

const (
	sourceHongguo       = "hongguo"
	sourceLZ            = "lz"
	sourceFF            = "ff"
	sourceWJ            = "wj"
	sourceBF            = "bf"
	sourceHN            = "hn"
	sourceSD            = "sd"
	sourceBD            = "bd"
	sourceXL            = "xl"
	sourceYQK           = "yqk"
	sourceHuangdou      = "huangdou"
	sourceHuangguoAI    = "huangguoai"
	sourceHuangguoVideo = "huangguo-video"
	source4KVM          = "4kvm"
)

func canonicalProviderSource(source string) string {
	s := strings.ToLower(strings.TrimSpace(source))
	s = strings.TrimPrefix(s, "www.")
	switch s {
	case sourceHongguo, "hongguoduanju.com":
		return sourceHongguo
	case sourceHuangdou, "tideember.cc", "xqjurgek.top":
		return sourceHuangdou
	case "huangguo", sourceHuangguoAI, "huangguoai.com":
		return sourceHuangguoAI
	case sourceHuangguoVideo, "huangguo.video":
		return sourceHuangguoVideo
	case sourceLZ, "liangzi", "lziapi.com", "cj.lziapi.com":
		return sourceLZ
	case sourceFF, "feifan", "ffzyapi.com", "cj.ffzyapi.com":
		return sourceFF
	case sourceWJ, "wuji", "wujikk.com", "api.wujikk.com":
		return sourceWJ
	case sourceBF, "baofeng", "bfzyapi.com":
		return sourceBF
	case sourceHN, "hongniu", "hongniuzy.com", "hongniuzy2.com":
		return sourceHN
	case sourceSD, "shandian", "sdzyapi.com":
		return sourceSD
	case sourceBD, "baidu", "badu", "apibdzy.com", "api.apibdzy.com", "bdzy":
		return sourceBD
	case sourceXL, "xunlei", "xinlangapi.com", "api.xinlangapi.com", "xunlei.cc", "xlzyapi.com":
		return sourceXL
	case sourceYQK, "yiqikan", "yqk88.com", "api.yqk88.com":
		return sourceYQK
	case source4KVM, "4kvm.net", "4kvm.com":
		return source4KVM
	default:
		return ""
	}
}

func isHuangguoProviderSource(source string) bool {
	switch canonicalProviderSource(source) {
	case sourceHuangguoAI, sourceHuangguoVideo, sourceHuangdou, sourceHongguo:
		return true
	default:
		return false
	}
}

func splitProviderDramaID(identifier string) (source, sourceID string, ok bool) {
	identifier = strings.TrimSpace(identifier)
	source, sourceID, prefixed := strings.Cut(identifier, ":")
	if prefixed {
		canon := canonicalProviderSource(source)
		if canon == "" {
			return "", "", false
		}
		source = canon
	} else {
		source = sourceHongguo
		sourceID = identifier
	}
	if source == sourceHongguo {
		sourceID = strings.TrimSpace(strings.TrimPrefix(sourceID, "hg-series-v1:"))
		if !hongguoNumericID.MatchString(sourceID) {
			return "", "", false
		}
	} else {
		sourceID = strings.TrimSpace(sourceID)
		if sourceID == "" || len(sourceID) > 128 {
			return "", "", false
		}
	}
	return source, sourceID, true
}

func normalizeDrama(drama Drama) (Drama, bool) {
	source, sourceID, valid := splitProviderDramaID(drama.ID)
	if !valid {
		if drama.Source != "" && drama.SourceID != "" {
			canon := canonicalProviderSource(drama.Source)
			if canon != "" {
				drama.Source = canon
				drama.ID = providerDramaID(canon, drama.SourceID)
				return drama, true
			}
		}
		return Drama{}, false
	}
	if drama.Source != "" {
		canon := canonicalProviderSource(drama.Source)
		if canon != source {
			return Drama{}, false
		}
	}
	if drama.SourceID != "" && drama.SourceID != sourceID {
		return Drama{}, false
	}
	drama.Source = source
	drama.SourceID = sourceID
	drama.ID = providerDramaID(source, sourceID)
	return drama, true
}

func normalizeHongguoDrama(drama Drama) (Drama, bool) {
	dr, ok := normalizeDrama(drama)
	if !ok || dr.Source != sourceHongguo {
		return Drama{}, false
	}
	return dr, true
}

func onlySupportedDramas(dramas []Drama) []Drama {
	filtered := make([]Drama, 0, len(dramas))
	seen := make(map[string]bool, len(dramas))
	for _, drama := range dramas {
		if normalized, valid := normalizeDrama(drama); valid && !seen[normalized.ID] {
			seen[normalized.ID] = true
			filtered = append(filtered, normalized)
		}
	}
	return filtered
}

func onlyHongguoDramas(dramas []Drama) []Drama {
	filtered := make([]Drama, 0, len(dramas))
	seen := make(map[string]bool, len(dramas))
	for _, drama := range dramas {
		if normalized, valid := normalizeHongguoDrama(drama); valid && !seen[normalized.ID] {
			seen[normalized.ID] = true
			filtered = append(filtered, normalized)
		}
	}
	return filtered
}

func isSupportedTask(task Task) bool {
	_, _, valid := splitProviderDramaID(task.DramaID)
	return valid
}

func isHongguoTask(task Task) bool {
	if task.Chapter.Source != "" && canonicalProviderSource(task.Chapter.Source) != sourceHongguo {
		return false
	}
	source, _, valid := splitProviderDramaID(task.DramaID)
	return valid && source == sourceHongguo
}

func readHongguoIDsRequest(writer http.ResponseWriter, request *http.Request) ([]string, bool) {
	identifiers, valid := readIDsRequest(writer, request)
	if !valid {
		return nil, false
	}
	for index, identifier := range identifiers {
		source, sourceID, supported := splitProviderDramaID(identifier)
		if !supported {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "无效的剧集 ID"})
			return nil, false
		}
		identifiers[index] = providerDramaID(source, sourceID)
	}
	return identifiers, true
}
