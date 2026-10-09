package com.xiaoqi.video.core.player

import android.app.Activity
import android.content.Context
import android.media.AudioManager
import android.os.Build
import android.provider.Settings
import kotlin.math.roundToInt

enum class PlayerLevel { Brightness, Volume }
enum class PlayerOrientation { Portrait, Landscape }

object PlayerTouchRules {
    fun side(x:Float,width:Float)=if(x<width/2f)PlayerLevel.Brightness else PlayerLevel.Volume
    fun level(initial:Float,deltaY:Float,height:Float):Float {
        if(height<=0 || !deltaY.isFinite())return initial.coerceIn(0f,1f)
        return (initial-deltaY/height*1.5f).coerceIn(0f,1f)
    }
    fun orientation(manual:String,width:Float,height:Float,shortDrama:Boolean):PlayerOrientation =
        PlayerOrientation.entries.find { it.name==manual } ?: when {
            width>0 && height>0 -> if(height>width)PlayerOrientation.Portrait else PlayerOrientation.Landscape
            shortDrama -> PlayerOrientation.Portrait
            else -> PlayerOrientation.Landscape
        }
}

/** Shared by live and VOD: window brightness and the system music-volume stream. */
class PlayerTouchController(private val activity:Activity?) {
    private val audio=activity?.getSystemService(Context.AUDIO_SERVICE) as? AudioManager
    private val originalBrightness=activity?.window?.attributes?.screenBrightness?:-1f
    private var brightnessChanged=false
    fun read(kind:PlayerLevel):Float = when(kind) {
        PlayerLevel.Brightness -> activity?.window?.attributes?.screenBrightness?.takeIf { it>=0 } ?: runCatching {
            Settings.System.getInt(activity?.contentResolver,Settings.System.SCREEN_BRIGHTNESS)/255f
        }.getOrDefault(.5f)
        PlayerLevel.Volume -> audio?.let {
            val min=minimumVolume(it);val range=it.getStreamMaxVolume(AudioManager.STREAM_MUSIC)-min
            if(range>0)(it.getStreamVolume(AudioManager.STREAM_MUSIC)-min).toFloat()/range else 0f
        }?:0f
    }
    fun write(kind:PlayerLevel,level:Float):Float {
        val safe=level.coerceIn(0f,1f)
        when(kind) {
            PlayerLevel.Brightness -> activity?.window?.let { window->
                window.attributes=window.attributes.apply { screenBrightness=safe.coerceAtLeast(.02f) };brightnessChanged=true
            }
            PlayerLevel.Volume -> audio?.let {
                if(!it.isVolumeFixed) {
                    val min=minimumVolume(it);val max=it.getStreamMaxVolume(AudioManager.STREAM_MUSIC)
                    it.setStreamVolume(AudioManager.STREAM_MUSIC,(min+safe*(max-min)).roundToInt().coerceIn(min,max),0)
                }
            }
        }
        return read(kind).coerceIn(0f,1f)
    }
    fun restoreBrightness() {
        if(brightnessChanged)activity?.window?.let { window->window.attributes=window.attributes.apply { screenBrightness=originalBrightness } }
    }
    private fun minimumVolume(manager:AudioManager)=if(Build.VERSION.SDK_INT>=28)manager.getStreamMinVolume(AudioManager.STREAM_MUSIC) else 0
}
