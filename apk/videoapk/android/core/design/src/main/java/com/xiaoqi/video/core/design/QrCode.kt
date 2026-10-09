package com.xiaoqi.video.core.design
import android.graphics.Bitmap
import androidx.compose.foundation.Image
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asImageBitmap
import com.google.zxing.BarcodeFormat
import com.google.zxing.MultiFormatWriter
@Composable fun QrCode(value:String,modifier:Modifier=Modifier) {
    val bitmap=remember(value) { val matrix=MultiFormatWriter().encode(value,BarcodeFormat.QR_CODE,384,384);Bitmap.createBitmap(384,384,Bitmap.Config.ARGB_8888).apply { for(y in 0 until 384)for(x in 0 until 384)setPixel(x,y,if(matrix[x,y])android.graphics.Color.BLACK else android.graphics.Color.WHITE) } }
    Image(bitmap.asImageBitmap(),contentDescription="扫码连接",modifier=modifier)
}
