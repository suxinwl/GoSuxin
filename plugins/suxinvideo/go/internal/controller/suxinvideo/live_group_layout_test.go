package suxinvideo

import "testing"

func TestLiveAuthoritativeRegionalAndCCTVLayout(t *testing.T) {
	for _, id := range []string{"Jade.hk@HD", "GoldenJade.hk", "J2.hk", "TVBPlus.hk@HD", "PhoenixChineseChannel.hk", "PhoenixInfoNewsChannel.hk", "PhoenixHongKongChannel.hk", "LotusTV.mo", "TVBSNews.tw"} {
		layout, ok := liveSystemChannelLayout(id)
		if !ok || layout.Key != liveRegionalGroupKey || layout.Name != "港澳台" || layout.GroupSort != -10000 {
			t.Fatal("regional channel was classified by genre instead of authority", id)
		}
	}
	for _, id := range []string{"BeijingSatelliteTV.cn", "AXNAsia.sg", "NTDTVAsiaPacific.us", "Unknown.hk", "CCTVExample.cn", ""} {
		if _, ok := liveSystemChannelLayout(id); ok {
			t.Fatal("unrelated or unrecognized channel was forcibly regrouped", id)
		}
	}
	previous := -10001
	for _, id := range []string{"Jade.hk", "PhoenixChineseChannel.hk", "PhoenixInfoNewsChannel.hk", "PhoenixHongKongChannel.hk", "GoldenJade.hk"} {
		layout, _ := liveSystemChannelLayout(id)
		if layout.Sort <= previous {
			t.Fatal("Jade/Phoenix pinning sequence changed", id)
		}
		previous = layout.Sort
	}
	previous = -1
	for _, id := range []string{"CCTV1.cn", "CCTV2.cn", "CCTV3.cn", "CCTV4Asia.cn", "CCTV5.cn", "CCTV5Plus.cn", "CCTV6.cn", "CCTV13.cn", "CCTV17.cn", "CCTV4K.cn", "CCTV8K.cn"} {
		layout, ok := liveSystemChannelLayout(id)
		if !ok || layout.Key != liveCCTVGroupKey || layout.Name != "CCTV" || layout.GroupSort != -9999 || layout.Sort <= previous {
			t.Fatal("CCTV numeric sorting or special ultra-HD order changed", id)
		}
		previous = layout.Sort
	}
	if liveGroupIdentity("港澳台") != liveIdentity(liveRegionalGroupKey) || liveGroupIdentity("CCTV") != liveIdentity(liveCCTVGroupKey) {
		t.Fatal("requested system groups lost stable identities")
	}
}
