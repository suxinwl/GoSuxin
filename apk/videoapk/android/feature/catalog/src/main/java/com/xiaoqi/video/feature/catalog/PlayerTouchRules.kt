package com.xiaoqi.video.feature.catalog

internal typealias PlayerLevel=com.xiaoqi.video.core.player.PlayerLevel
internal typealias PlayerOrientation=com.xiaoqi.video.core.player.PlayerOrientation

/** A gesture keeps the side and initial level chosen at touch-down. */
internal object PlayerTouchRules {
    fun side(x:Float,width:Float)=com.xiaoqi.video.core.player.PlayerTouchRules.side(x,width)
    fun level(initial:Float,deltaY:Float,height:Float)=com.xiaoqi.video.core.player.PlayerTouchRules.level(initial,deltaY,height)
    fun orientation(manual:String,width:Float,height:Float,shortDrama:Boolean)=com.xiaoqi.video.core.player.PlayerTouchRules.orientation(manual,width,height,shortDrama)
}
