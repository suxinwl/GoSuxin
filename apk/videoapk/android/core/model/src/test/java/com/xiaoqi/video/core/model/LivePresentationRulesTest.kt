package com.xiaoqi.video.core.model

import org.junit.Assert.*
import org.junit.Test

class LivePresentationRulesTest {
    @Test fun technicalNamesBecomeNumberedChineseFallbacksButConfiguredChineseNamesRemain() {
        assertEquals("主线路",LivePresentationRules.streamName(LiveStream(name="primary-1080p"),0))
        assertEquals("备用线路 2",LivePresentationRules.streamName(LiveStream(name="8AVC"),2))
        assertEquals("凤凰中文高清",LivePresentationRules.streamName(LiveStream(name="凤凰中文高清"),1))
    }
    @Test fun qualityNeverInventsResolutionFromAStreamBrand() {
        assertEquals("原始清晰度",LivePresentationRules.qualityLabel("8AVC"))
        assertEquals("原始清晰度",LivePresentationRules.qualityLabel(""))
        assertEquals("高清",LivePresentationRules.qualityLabel("HD"))
        assertEquals("720P",LivePresentationRules.qualityLabel("720p"))
        assertEquals("1920×1080",LivePresentationRules.qualityLabel("原画",1080,1920))
    }
    @Test fun actualFailureTakesPrecedenceOverEarlierProbeHealth() {
        val stream=LiveStream(health="healthy")
        assertEquals("已检测可用",LivePresentationRules.streamStatus(stream))
        assertEquals("本轮连接失败",LivePresentationRules.streamStatus(stream,true))
    }
}
