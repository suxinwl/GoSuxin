package com.xiaoqi.video.core.design

import androidx.compose.foundation.basicMarquee
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp

/** basicMarquee starts only when the text is wider than its bounded viewport. */
@Composable fun OverflowTitle(
    text:String,
    modifier:Modifier=Modifier,
    style:TextStyle=MaterialTheme.typography.bodyMedium,
    color:Color=LocalCinemaStyle.current.text,
    animate:Boolean=true
) {
    val scrolling=if(animate)Modifier.clipToBounds().basicMarquee(
        iterations=Int.MAX_VALUE,
        initialDelayMillis=1500,
        repeatDelayMillis=2000,
        velocity=22.dp
    ) else Modifier
    Text(text,modifier=modifier.then(scrolling),style=style,color=color,
        maxLines=1,softWrap=false,overflow=if(animate)TextOverflow.Clip else TextOverflow.Ellipsis)
}

/** A complete selectable title for narrow cards and banner captions. */
@Composable fun FilmTitleDialog(title:String,onDismiss:()->Unit) {
    AlertDialog(
        onDismissRequest=onDismiss,
        title={ Text("影片完整标题") },
        text={ Column(Modifier.heightIn(max=360.dp).verticalScroll(rememberScrollState())) {
            SelectionContainer { Text(title,softWrap=true,color=LocalCinemaStyle.current.text) }
        } },
        confirmButton={ TextButton(onClick=onDismiss) { Text("关闭") } }
    )
}
