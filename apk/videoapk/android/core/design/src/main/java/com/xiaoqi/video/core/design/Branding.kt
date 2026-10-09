package com.xiaoqi.video.core.design

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.unit.dp
import coil.ImageLoader
import coil.compose.AsyncImage
import com.xiaoqi.video.core.network.SiteHttp

/** The supplied black glyph always has white backing, including on dark CMS themes. */
@Composable fun CinemaBrandLogo(url:String="",modifier:Modifier=Modifier,description:String="小柒影视图标") {
    BrandedImage(url,R.drawable.xiaoqi_logo,description,modifier.clip(RoundedCornerShape(5.dp)))
}

@Composable fun CinemaAvatar(url:String?=null,modifier:Modifier=Modifier) {
    BrandedImage(url.orEmpty(),R.drawable.default_avatar,"用户头像",modifier.clip(CircleShape))
}

@Composable private fun BrandedImage(url:String,defaultResource:Int,description:String,modifier:Modifier) {
    val fallback=painterResource(defaultResource)
    Box(modifier.background(Color.White)) {
        if(url.isBlank())Image(fallback,description,Modifier.fillMaxSize(),contentScale=ContentScale.Fit)
        else {
            val context=LocalContext.current
            val address=SiteHttp.absolute(url)
            val loader=remember(address,context) { ImageLoader.Builder(context).okHttpClient(SiteHttp.client(address)).build() }
            AsyncImage(model=address,imageLoader=loader,contentDescription=description,placeholder=fallback,error=fallback,fallback=fallback,contentScale=ContentScale.Fit,modifier=Modifier.fillMaxSize())
        }
    }
}
