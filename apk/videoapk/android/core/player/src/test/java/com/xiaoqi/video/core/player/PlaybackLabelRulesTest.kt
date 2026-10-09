package com.xiaoqi.video.core.player

import com.xiaoqi.video.core.model.*
import org.junit.Assert.*
import org.junit.Test

class PlaybackLabelRulesTest {
    private val film=Film(id=781,name="仙逆",remarks="第160集")
    private fun detail(vararg sources:Source)=Detail(film,sources.toList(),"",emptyList(),emptyList())
    private fun source(code:String,version:String="vod:781",vararg episodes:Episode)=
        Source(code=code,versionKey=version,episodes=episodes.toList())

    @Test fun filmUpdateRemarkIsNotThePlayingEpisode() {
        val s=source("ecy_aa02",episodes=arrayOf(Episode("episode:160",160,"第160集"),Episode("episode:161",161,"第161集")))
        val resolved=Playback(vodId=781,line=s.code,versionKey=s.versionKey,episodeKey="episode:161",episode=1,name="第161集")
        assertEquals("第161集",PlaybackLabelRules.episodeName(detail(s),resolved,null))
    }

    @Test fun stableKeyIgnoresDifferentPositionsAndPreviewEntries() {
        val s=source("ecy_4k01",episodes=arrayOf(Episode("preview",-1,"预告"),Episode("episode:160",160,"第160集"),Episode("episode:161",161,"第161集")))
        val request=PlaybackIdentity(781,s.code,"episode:161",160,versionKey=s.versionKey)
        assertEquals("第161集",PlaybackLabelRules.episodeName(detail(s),null,request))
    }

    @Test fun newlySelectedEpisodeReplacesPreviousTitleBeforeResolveCompletes() {
        val s=source("yqk_1",episodes=arrayOf(Episode("episode:160",160,"第160集"),Episode("episode:161",161,"第161集")))
        val old=Playback(vodId=781,line=s.code,versionKey=s.versionKey,episodeKey="episode:160",name="第160集")
        val next=PlaybackIdentity(781,s.code,"episode:161",1,versionKey=s.versionKey)
        assertEquals("第161集",PlaybackLabelRules.episodeName(detail(s),old,next))
    }

    @Test fun descriptorTitleSurvivesMetadataNotYetContainingNewEpisode() {
        val old=source("yqk_1",episodes=arrayOf(Episode("episode:160",160,"第160集")))
        val resolved=Playback(vodId=781,line=old.code,versionKey=old.versionKey,episodeKey="episode:161",episode=160,name="第161集")
        val request=PlaybackIdentity(781,old.code,"episode:161",160,versionKey=old.versionKey)
        assertEquals("第161集",PlaybackLabelRules.episodeName(detail(old),resolved,request))
    }

    @Test fun missingKeyAndWrongVersionDoNotGuessFromIndex() {
        val s=source("yqk_1",episodes=arrayOf(Episode("episode:160",160,"第160集")))
        assertEquals("",PlaybackLabelRules.episodeName(detail(s),null,PlaybackIdentity(781,s.code,"episode:161",0,versionKey=s.versionKey)))
        assertEquals("",PlaybackLabelRules.episodeName(detail(s),null,PlaybackIdentity(781,s.code,"episode:160",0,versionKey="season:2")))
        assertEquals("",PlaybackLabelRules.episodeName(detail(s),null,PlaybackIdentity(5532,s.code,"episode:160",0,versionKey=s.versionKey)))
    }

    @Test fun serverNumberFormatsNumericNamesButKeepsMovieLabels() {
        val s=source("4kvm",episodes=arrayOf(Episode("episode:161",161,"161"),Episode("movie",-1,"正片")))
        assertEquals("第161集",PlaybackLabelRules.episodeName(detail(s),null,PlaybackIdentity(781,s.code,"episode:161",0,versionKey=s.versionKey)))
        assertEquals("正片",PlaybackLabelRules.episodeName(detail(s),null,PlaybackIdentity(781,s.code,"movie",1,versionKey=s.versionKey)))
    }

    @Test fun manualSwitchUsesSameEpisodeKeyWhenTargetListHasMissingEpisodes() {
        val old=source("yqk_1",episodes=arrayOf(Episode("episode:159",159,"第159集"),Episode("episode:160",160,"第160集"),Episode("episode:161",161,"第161集")))
        val target=source("yqk_2",episodes=arrayOf(Episode("episode:160",160,"第160集"),Episode("episode:161",161,"第161集")))
        val selected=PlaybackMediaRules.manualSource(listOf(old,target),target,
            PlaybackIdentity(781,old.code,"episode:161",2,versionKey=old.versionKey),12345)!!
        assertEquals(1,selected.episode)
        assertEquals("episode:161",selected.episodeKey)
        assertEquals(12345L,selected.positionMs)
        assertEquals("第161集",PlaybackLabelRules.episodeName(detail(old,target),null,selected))
    }
}
