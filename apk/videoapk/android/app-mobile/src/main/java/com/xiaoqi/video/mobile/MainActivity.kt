package com.xiaoqi.video.mobile
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
 override fun onCreate(savedInstanceState:Bundle?) {
   super.onCreate(savedInstanceState);enableEdgeToEdge()
   intent?.data?.let { uri -> if(uri.host=="pair")pairCode=uri.getQueryParameter("code").orEmpty() else initialFilmId=uri.getQueryParameter("id")?.toLongOrNull()?:0 }
   setContent { NativeApp(tv=false,initialFilmId=initialFilmId,pairCode=pairCode,filmIntentVersion=filmIntentVersion) }
   if(Build.VERSION.SDK_INT>=33 && checkSelfPermission(android.Manifest.permission.POST_NOTIFICATIONS)!=android.content.pm.PackageManager.PERMISSION_GRANTED) requestPermissions(arrayOf(android.Manifest.permission.POST_NOTIFICATIONS),410)
 }
 override fun onNewIntent(intent:Intent) { super.onNewIntent(intent);setIntent(intent);intent.data?.let { if(it.host=="pair")pairCode=it.getQueryParameter("code").orEmpty() else { initialFilmId=it.getQueryParameter("id")?.toLongOrNull()?:0;filmIntentVersion++ } } }
 // Visible PiP keeps the Activity started; onStop means playback is no longer visible.
 override fun onStop() { PlayerHub.engine.pauseForBackground(false);super.onStop() }
 override fun onStart() { super.onStart();PlayerHub.engine.restoreForeground() }
 override fun onDestroy() { PlayerHub.engine.saveProgress();super.onDestroy() }
}

