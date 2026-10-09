package com.xiaoqi.video.feature.catalog

import org.junit.Assert.*
import org.junit.Test

class PlayerTouchRulesTest {
    @Test fun upRaisesAndDownLowersOnEitherSide() {
        assertEquals(PlayerLevel.Brightness,PlayerTouchRules.side(99f,200f))
        assertEquals(PlayerLevel.Volume,PlayerTouchRules.side(100f,200f))
        assertEquals(.8f,PlayerTouchRules.level(.5f,-200f,1000f),.001f)
        assertEquals(.2f,PlayerTouchRules.level(.5f,200f,1000f),.001f)
    }
    @Test fun gestureIsBoundedAndIndependentOfDisplaySize() {
        assertEquals(1f,PlayerTouchRules.level(.9f,-1000f,300f),0f)
        assertEquals(0f,PlayerTouchRules.level(.1f,1000f,300f),0f)
        assertEquals(PlayerTouchRules.level(.5f,-100f,500f),PlayerTouchRules.level(.5f,-200f,1000f),.001f)
        assertEquals(.5f,PlayerTouchRules.level(.5f,20f,0f),0f)
    }
    @Test fun actualVideoShapeWinsOverShortDramaCategory() {
        assertEquals(PlayerOrientation.Portrait,PlayerTouchRules.orientation("",720f,1280f,false))
        assertEquals(PlayerOrientation.Landscape,PlayerTouchRules.orientation("",1920f,1080f,true))
        assertEquals(PlayerOrientation.Portrait,PlayerTouchRules.orientation("",0f,0f,true))
    }
    @Test fun manualOrientationSurvivesNewQualityAndEpisodeDimensions() {
        assertEquals(PlayerOrientation.Portrait,PlayerTouchRules.orientation("Portrait",1920f,1080f,false))
        assertEquals(PlayerOrientation.Landscape,PlayerTouchRules.orientation("Landscape",720f,1280f,true))
    }
}
