package com.xiaoqi.video.core.player

import androidx.media3.session.MediaSession
import androidx.media3.session.MediaSessionService

class PlaybackService : MediaSessionService() {
    private var session: MediaSession? = null
    override fun onCreate() { super.onCreate();PlayerHub.initialize(this);session=MediaSession.Builder(this,PlayerHub.engine.player).build() }
    override fun onGetSession(controllerInfo: MediaSession.ControllerInfo):MediaSession?=session
    override fun onDestroy() { session?.release();session=null;super.onDestroy() }
}
