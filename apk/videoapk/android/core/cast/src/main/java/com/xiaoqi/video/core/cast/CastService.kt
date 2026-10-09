package com.xiaoqi.video.core.cast

import android.app.*
import android.content.Context
import android.content.Intent
import android.os.Build
import android.os.IBinder
import com.xiaoqi.video.core.data.AppGraph
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.MutableStateFlow

object CastHub {
    lateinit var device:DeviceCast;private set
    lateinit var dlna:Dlna;private set
    val name=MutableStateFlow("")
    val kind=MutableStateFlow("")
    val dlnaStatus=MutableStateFlow(DlnaStatus())
    private var monitor:Job?=null
    private var mediaJobId:String=""
    private lateinit var context:Context
    fun initialize(ctx:Context) {
        if(::device.isInitialized)return
        context=ctx.applicationContext;AppGraph.initialize(context);device=DeviceCast(AppGraph.repository);dlna=Dlna(context)
        AppGraph.repository.scope.launch { var owner=0L;AppGraph.repository.session.collect { val id=it?.user?.id?:0;if(owner>0&&id!=owner)end();owner=id } }
    }
    fun connected(deviceName:String,type:String) { name.value=deviceName;kind.value=type;context.startForegroundServiceCompat(Intent(context,CastService::class.java));monitor?.cancel();if(type=="dlna")monitor=AppGraph.repository.scope.launch { while(isActive) { runCatching { dlnaStatus.value=dlna.state() };delay(2000) } } }
    fun registerMediaJob(id:String) { mediaJobId=id }
    fun end() { monitor?.cancel();monitor=null;if(kind.value=="tv")runCatching { device.command("stop") };device.close();dlna.stop();name.value="";kind.value="";context.stopService(Intent(context,CastService::class.java));val job=mediaJobId;mediaJobId="";if(job.isNotBlank())AppGraph.repository.scope.launch { runCatching { AppGraph.repository.api.request("cast/prepare/$job/cancel","POST") } } }
    private fun Context.startForegroundServiceCompat(intent:Intent) { if(Build.VERSION.SDK_INT>=26)startForegroundService(intent) else startService(intent) }
}
class CastService:Service() {
    override fun onBind(intent:Intent?):IBinder?=null
    override fun onCreate() {
        super.onCreate();CastHub.initialize(this)
        val manager=getSystemService(NOTIFICATION_SERVICE) as NotificationManager
        if(Build.VERSION.SDK_INT>=26)manager.createNotificationChannel(NotificationChannel("xiaoqi_cast","投屏控制",NotificationManager.IMPORTANCE_LOW))
        val launch=packageManager.getLaunchIntentForPackage(packageName)
        val pending=launch?.let { PendingIntent.getActivity(this,0,it,PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT) }
        val stop=PendingIntent.getService(this,1,Intent(this,CastService::class.java).setAction("stop"),PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
        val builder=if(Build.VERSION.SDK_INT>=26)Notification.Builder(this,"xiaoqi_cast") else Notification.Builder(this)
        startForeground(3201,builder.setSmallIcon(android.R.drawable.ic_media_play).setContentTitle("小柒影视投屏").setContentText(CastHub.name.value).setContentIntent(pending).setOngoing(true).addAction(Notification.Action.Builder(android.R.drawable.ic_media_pause,"结束投屏",stop).build()).build())
    }
    override fun onStartCommand(intent:Intent?,flags:Int,startId:Int):Int { if(intent?.action=="stop") { CastHub.end();stopSelf() };return START_NOT_STICKY }
}
