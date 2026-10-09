package com.xiaoqi.video.tv
import android.app.Application
import android.os.Bundle
import android.os.Build
import android.content.Intent
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.runtime.*
import com.xiaoqi.video.core.data.AppGraph
import com.xiaoqi.video.core.player.PlayerHub
import com.xiaoqi.video.core.download.Downloads
import com.xiaoqi.video.core.cast.CastHub
import com.xiaoqi.video.core.network.EndpointBootstrap
import com.xiaoqi.video.feature.catalog.NativeApp

class CinemaApplication:Application() {
 override fun onCreate() { super.onCreate();AppGraph.initialize(this);PlayerHub.initialize(this);Downloads.initialize(this);CastHub.initialize(this);EndpointBootstrap.start() }
}
class MainActivity:ComponentActivity() {
 private var pairCode by mutableStateOf("")
 private var initialFilmId by mutableLongStateOf(0)
 private var filmIntentVersion by mutableIntStateOf(0)
 private fun consumeIntent(intent:Intent?) {
   intent?.data?.let { uri ->
     if(uri.host=="pair")pairCode=uri.getQueryParameter("code").orEmpty()
     else uri.getQueryParameter("id")?.toLongOrNull()?.takeIf { it>0 }?.let { id ->
       initialFilmId=id;filmIntentVersion++
     }
   }
 }
 override fun onCreate(savedInstanceState:Bundle?) {
   super.onCreate(savedInstanceState);enableEdgeToEdge()
   consumeIntent(intent)
   setContent { NativeApp(tv=true,initialFilmId=initialFilmId,pairCode=pairCode,filmIntentVersion=filmIntentVersion) }
 }
 override fun onNewIntent(intent:Intent) { super.onNewIntent(intent);setIntent(intent);consumeIntent(intent) }
 override fun onStop() { PlayerHub.engine.pauseForBackground(Build.VERSION.SDK_INT>=24&&isInPictureInPictureMode);super.onStop() }
 override fun onStart() { super.onStart();PlayerHub.engine.restoreForeground() }
 override fun onDestroy() { PlayerHub.engine.saveProgress();super.onDestroy() }
}

