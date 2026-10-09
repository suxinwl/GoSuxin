package com.xiaoqi.video.feature.catalog

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp

/** Preferences describe card density, not a fixed viewport height; pages always scroll vertically. */
@Composable internal fun CatalogDisplayDialog(
    columns:Int,
    rows:Int,
    onApply:(Int,Int)->Unit,
    onDismiss:()->Unit
) {
    var selectedColumns by remember(columns) { mutableIntStateOf(columns) }
    var pageRows by remember(rows) { mutableIntStateOf(rows) }
    AlertDialog(onDismissRequest=onDismiss,title={ Text("影片显示数量") },text={
        Column(Modifier.verticalScroll(rememberScrollState()),verticalArrangement=Arrangement.spacedBy(12.dp)) {
            Text("每行影片数（横向）")
            ColumnChoices(selectedColumns,{ selectedColumns=it })
            Text("每页行数（上下滚动查看）")
            Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()),horizontalArrangement=Arrangement.spacedBy(8.dp)) {
                listOf(6,8,10,12).forEach { count->FilterChip(pageRows==count,{ pageRows=count },label={ Text("$count 行") },modifier=Modifier.testTag("catalog-rows-$count")) }
            }
            Text("首页、频道和搜索共用此设置；小屏幕会自动适配。",style=MaterialTheme.typography.bodySmall)
        }
    },confirmButton={ TextButton(onClick={ onApply(selectedColumns,pageRows) },modifier=Modifier.testTag("catalog-display-apply")) { Text("保存") } },dismissButton={ TextButton(onClick=onDismiss) { Text("取消") } })
}

@Composable private fun ColumnChoices(selected:Int,onSelect:(Int)->Unit) {
    Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()),horizontalArrangement=Arrangement.spacedBy(8.dp)) {
        (listOf(0)+listOf(2,3,4,5,6)).forEach { count->FilterChip(selected==count,{ onSelect(count) },label={ Text(if(count==0)"自动" else "$count 部") },modifier=Modifier.testTag("catalog-columns-$count")) }
    }
}
