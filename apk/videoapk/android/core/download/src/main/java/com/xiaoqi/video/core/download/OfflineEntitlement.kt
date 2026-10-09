package com.xiaoqi.video.core.download

object OfflineEntitlement {
    const val MAX_SECONDS=7L*24*60*60
    fun refusal(owner:Long,activeOwner:Long,device:String,activeDevice:String,issued:Long,expires:Long,now:Long,clockFloor:Long=issued):String? = when {
        owner<=0 || owner!=activeOwner -> "请登录下载该影片的账号"
        device.isBlank() || device!=activeDevice -> "离线授权不属于当前设备"
        issued<=0 || expires<=issued || expires-issued>MAX_SECONDS -> "离线授权无效，请联网更新"
        now<maxOf(issued,clockFloor)-300 -> "设备时间已回退，请联网校验离线授权"
        now>=expires -> "离线授权已到期，请联网更新"
        else -> null
    }
}
