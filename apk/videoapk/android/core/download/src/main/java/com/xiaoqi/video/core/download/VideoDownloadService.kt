@file:androidx.annotation.OptIn(androidx.media3.common.util.UnstableApi::class)
package com.xiaoqi.video.core.download
import android.app.Notification
import androidx.media3.exoplayer.offline.*
import androidx.media3.exoplayer.scheduler.Scheduler
import androidx.media3.exoplayer.workmanager.WorkManagerScheduler

class VideoDownloadService:DownloadService(3101,1000,"xiaoqi_downloads",com.xiaoqi.video.core.download.R.string.download_channel,0) {
    override fun getDownloadManager():DownloadManager { Downloads.initialize(this);return Downloads.manager }
    override fun getScheduler():Scheduler=WorkManagerScheduler(this,"xiaoqi_downloads")
    override fun getForegroundNotification(downloads:MutableList<Download>,notMetRequirements:Int):Notification = DownloadNotificationHelper(this,"xiaoqi_downloads").buildProgressNotification(this,android.R.drawable.stat_sys_download,null,"小柒影视离线下载",downloads,notMetRequirements)
}
