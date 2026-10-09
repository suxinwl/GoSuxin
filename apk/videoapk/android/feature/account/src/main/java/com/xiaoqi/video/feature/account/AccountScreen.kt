package com.xiaoqi.video.feature.account

import android.content.Intent
import android.net.Uri
import android.util.Base64
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.Alignment
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import coil.compose.AsyncImage
import com.xiaoqi.video.core.data.AppRepository
import com.xiaoqi.video.core.model.*
import com.xiaoqi.video.core.design.*
import com.xiaoqi.video.core.cast.*
import com.xiaoqi.video.core.network.JsonWire.string
import com.xiaoqi.video.core.network.JsonWire.array
import com.xiaoqi.video.core.network.JsonWire
import com.xiaoqi.video.core.network.SiteHttp
import kotlinx.coroutines.*
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

@Composable fun AccountScreen(repo:AppRepository,tv:Boolean=false) {
    val session by repo.session.collectAsState()
    val context=LocalContext.current
    val scope=rememberCoroutineScope()
    var mode by remember { mutableStateOf("login") }
    var email by remember { mutableStateOf("") };var name by remember { mutableStateOf("") };var password by remember { mutableStateOf("") };var emailCode by remember { mutableStateOf("") }
    var captcha by remember { mutableStateOf<Captcha?>(null) };var captchaText by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) };var packages by remember { mutableStateOf<List<Package>>(emptyList()) }
    var pairing by remember { mutableStateOf("") };var payType by remember { mutableStateOf("epay") };var payMenu by remember { mutableStateOf(false) }
    val cast=remember { DeviceCast(repo) }
    var payment by remember { mutableStateOf<com.google.gson.JsonObject?>(null) }
    var config by remember { mutableStateOf<SiteConfig?>(null) }
    var configLoading by remember { mutableStateOf(true) };var configError by remember { mutableStateOf<String?>(null) };var configRetry by remember { mutableIntStateOf(0) }
    var orders by remember { mutableStateOf<List<com.google.gson.JsonObject>>(emptyList()) };var showOrders by remember { mutableStateOf(false) };var requestFilm by remember { mutableStateOf(false) };var requestedTitle by remember { mutableStateOf("") };var requestNote by remember { mutableStateOf("") }
    fun perform(block:suspend ()->Unit) { scope.launch { busy=true;try { block() } catch(e:CancellationException) { throw e } catch(e:Throwable) { repo.error(e) } finally { busy=false } } }
    suspend fun refreshCaptcha() { captcha=repo.captcha();captchaText="" }
    fun changeMode(next:String) { if(mode!=next) { mode=next;emailCode="";config=null;configError=null;configLoading=true } }
    // Auth policy must come from the server, never from the catalogue's offline cache.
    // Re-entering a form also refreshes settings changed by the administrator.
    LaunchedEffect(mode,configRetry,session?.user?.id) {
        config=null;configError=null;configLoading=true
        try {
            val latest=JsonWire.config(repo.api.request("config",accessToken=""))
            config=latest;payType=latest.paymentMethods.firstOrNull()?.code.orEmpty()
        } catch(e:CancellationException) { throw e }
        catch(e:Throwable) { configError=e.localizedMessage?:"读取账号设置失败" }
        finally { configLoading=false }
    }
    LaunchedEffect(session?.user?.id,mode) {
        try { if(session==null && !tv)refreshCaptcha() else if(session!=null) { packages=repo.packages();repo.me() } }
        catch(e:CancellationException) { throw e }
        catch(e:Throwable) { repo.error(e) }
    }
    if(tv && session==null) { TvPairing(repo,cast);return }
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(if(tv)32.dp else 20.dp),verticalArrangement=Arrangement.spacedBy(14.dp)) {
        if(session==null) {
            AccountAuthForm(mode,config,AccountInput(email,name,password,emailCode,captchaText),captcha,busy,configLoading,configError,
                onInputChanged={ email=it.email;name=it.name;password=it.password;emailCode=it.emailCode;captchaText=it.captchaText },
                onModeChanged=::changeMode,onRetryConfig={ configRetry++ },onRefreshCaptcha={ perform { refreshCaptcha() } },
                onSendCode={ perform {
                    val latest=config
                    require((mode=="register" && latest?.registrationEnabled==true && latest.registrationRequiresEmailCode) || (mode=="reset" && latest?.passwordResetEnabled==true)) { "邮箱验证当前不可用，请重试" }
                    repo.emailCode(email,captcha?.challengeId.orEmpty(),captchaText,if(mode=="reset")"reset" else "register");repo.notice.value="验证码已发送";refreshCaptcha()
                } },onSubmit={ perform {
                try {
                    val latest=config
                    when(mode) {
                        "register"->{ require(latest?.registrationEnabled==true) { "注册设置未就绪，请重试" };repo.register(email,name,password,if(latest.registrationRequiresEmailCode)emailCode else "",captcha?.challengeId.orEmpty(),captchaText);changeMode("login");repo.notice.value="注册成功，请登录" }
                        "reset"->{ require(latest?.passwordResetEnabled==true) { "邮箱服务未启用，请联系管理员重置密码" };repo.reset(email,emailCode,password,captcha?.challengeId.orEmpty(),captchaText);changeMode("login");repo.notice.value="密码已更新，请登录" }
                        else->repo.login(email,password,captcha?.challengeId.orEmpty(),captchaText)
                    }
                } catch(e:CancellationException) { throw e }
                catch(e:Throwable) { runCatching { refreshCaptcha() };throw e }
            } })
        } else {
            val u=session!!.user
            Row(verticalAlignment=Alignment.CenterVertically,horizontalArrangement=Arrangement.spacedBy(14.dp)) {
                CinemaAvatar(u.avatar,Modifier.size(if(tv)72.dp else 64.dp))
                Text(u.name.ifBlank { u.email },style=MaterialTheme.typography.headlineMedium)
            }
            Text("积分：${u.points}    ${if(u.isVip())"VIP 至 "+SimpleDateFormat("yyyy-MM-dd",Locale.CHINA).format(Date(u.vipExpire*1000)) else "普通会员"}")
            Row { Button(onClick={ perform { repo.api.request("sign","POST");repo.me();repo.notice.value="签到成功" } }) { Text("每日签到") };Spacer(Modifier.width(12.dp));OutlinedButton(onClick={ perform { repo.me() } }) { Text("刷新权益") };Spacer(Modifier.width(12.dp));TextButton(onClick={ perform { repo.logout() } }) { Text("退出登录") } }
            Row { OutlinedButton(onClick={ perform { val d=repo.api.request("orders");orders=(if(d.isJsonArray)d.asJsonArray else d.asJsonObject.array("items","orders")).map { it.asJsonObject };showOrders=true } }) { Text("我的订单") };Spacer(Modifier.width(12.dp));OutlinedButton(onClick={ requestFilm=true }) { Text("我要找片") } }
            if(!tv) {
                Text("连接 TV",style=MaterialTheme.typography.titleLarge)
                Row(verticalAlignment=Alignment.CenterVertically) { OutlinedTextField(pairing,{ pairing=it },label={ Text("TV 配对码") },singleLine=true,modifier=Modifier.weight(1f));TextButton(enabled=pairing.isNotBlank(),onClick={ perform { cast.approve(pairing);pairing="";repo.notice.value="TV 已连接，可在播放页投屏" } }) { Text("确认配对") } }
            }
            Text("会员与充值",style=MaterialTheme.typography.titleLarge)
            if(tv)Text("请在手机 APP 或网站购买会员，完成后刷新权益") else {
                Box { OutlinedButton(onClick={ payMenu=true }) { Text("支付方式：${config?.paymentMethods?.find { it.code==payType }?.name?:"未启用"}") };DropdownMenu(payMenu,{ payMenu=false }) { config?.paymentMethods.orEmpty().forEach { method->DropdownMenuItem(text={ Text(method.name) },onClick={ payType=method.code;payMenu=false }) } } }
                packages.forEach { p -> Card(Modifier.fillMaxWidth()) { Row(Modifier.padding(16.dp),verticalAlignment=Alignment.CenterVertically) { Column(Modifier.weight(1f)) { Text(p.name,style=MaterialTheme.typography.titleMedium);Text("¥${p.price} · ${p.days} 天 · ${p.points} 积分") };Button(enabled=!busy&&payType.isNotBlank(),onClick={ perform {
                    val o=repo.api.request("payment/create","POST",mapOf("goods_id" to p.id,"pay_type" to payType)).asJsonObject
                    val url=o.string("url","cashier_url");if(o.string("type")=="qrcode"||o.string("qr").isNotBlank())payment=o else if(url.isNotBlank())context.startActivity(Intent(Intent.ACTION_VIEW,Uri.parse(SiteHttp.absolute(url)))) else payment=o
                } }) { Text("购买") } } } }
                if(packages.isEmpty())EmptyState("暂无可购买套餐")
            }
            Text("账号与设备",style=MaterialTheme.typography.titleLarge)
            Text("同一账号同步收藏和观看进度。离线授权最长 7 天，会员内容受会员到期时间限制。")
        }
    }
    payment?.let { p -> AlertDialog(onDismissRequest={ payment=null },title={ Text("完成支付") },text={ Column { val qr=p.string("qr");val image=p.string("qr_image");if(qr.isNotBlank())QrCode(qr,Modifier.size(220.dp)) else if(image.isNotBlank())AsyncImage(image,"支付二维码",Modifier.size(220.dp));Text(p.string("tip",fallback="请支付后刷新权益"));Text(p.string("order_no"));if(qr.isNotBlank()&&Uri.parse(qr).scheme in listOf("http","https","weixin","alipays"))TextButton(onClick={ runCatching { context.startActivity(Intent(Intent.ACTION_VIEW,Uri.parse(qr))) }.onFailure(repo::error) }) { Text("打开支付") } } },confirmButton={ TextButton(onClick={ perform { repo.me();payment=null } }) { Text("刷新权益") } }) }
    if(showOrders)AlertDialog(onDismissRequest={ showOrders=false },title={ Text("我的订单") },text={ androidx.compose.foundation.lazy.LazyColumn(Modifier.heightIn(max=360.dp),verticalArrangement=Arrangement.spacedBy(12.dp)) { if(orders.isEmpty())item { Text("暂无订单") };items(orders.size) { i->val o=orders[i];Column { Text(o.string("title"),style=MaterialTheme.typography.titleMedium);Text("¥${o.string("amount")} · "+when(o.string("status")) { "1"->"已支付";"2"->"已取消";else->"待支付" });Text(o.string("order_no"),style=MaterialTheme.typography.labelSmall);HorizontalDivider() } } } },confirmButton={ TextButton(onClick={ showOrders=false }) { Text("关闭") } })
    if(requestFilm)AlertDialog(onDismissRequest={ requestFilm=false },title={ Text("求片") },text={ Column(verticalArrangement=Arrangement.spacedBy(12.dp)) { OutlinedTextField(requestedTitle,{ requestedTitle=it },label={ Text("影片名称") });OutlinedTextField(requestNote,{ requestNote=it },label={ Text("补充说明") }) } },confirmButton={ TextButton(enabled=requestedTitle.isNotBlank()&&!busy,onClick={ perform { repo.api.request("film-request","POST",mapOf("title" to requestedTitle.trim(),"note" to requestNote.trim()));requestFilm=false;requestedTitle="";requestNote="";repo.notice.value="求片已提交" } }) { Text("提交") } },dismissButton={ TextButton(onClick={ requestFilm=false }) { Text("取消") } })
}

data class AccountInput(val email:String="",val name:String="",val password:String="",val emailCode:String="",val captchaText:String="")

/** Native credential fields shared with focused UI checks, without issuing requests from the form. */
@Composable fun AccountAuthForm(
    mode:String,config:SiteConfig?,input:AccountInput,captcha:Captcha?,busy:Boolean,
    configLoading:Boolean,configError:String?,onInputChanged:(AccountInput)->Unit,
    onModeChanged:(String)->Unit,onRetryConfig:()->Unit,onRefreshCaptcha:()->Unit,
    onSendCode:()->Unit,onSubmit:()->Unit
) {
    val formAllowed=mode=="login" || when(mode) {
        "register"->config?.registrationEnabled==true
        "reset"->config?.passwordResetEnabled==true
        else->false
    }
    val requiresEmailCode=when(mode) {
        "register"->config?.registrationRequiresEmailCode==true
        "reset"->true
        else->false
    }
    Column(verticalArrangement=Arrangement.spacedBy(14.dp)) {
        Row(verticalAlignment=Alignment.CenterVertically,horizontalArrangement=Arrangement.spacedBy(12.dp)) {
            CinemaBrandLogo(modifier=Modifier.size(48.dp).testTag("account-brand-logo"))
            Text(when(mode) { "register"->"注册账号";"reset"->"重置密码";else->"登录小柒影视" },style=MaterialTheme.typography.headlineSmall)
        }
        if(config==null) {
            if(configLoading)Text("正在读取注册与找回密码设置…",style=MaterialTheme.typography.bodySmall)
            else {
                Text(configError?:"读取账号设置失败，请重试",color=MaterialTheme.colorScheme.error)
                TextButton(onClick=onRetryConfig,modifier=Modifier.testTag("account-config-retry")) { Text("重试账号设置") }
            }
        } else if(mode=="register" && !config.registrationEnabled)Text("当前已关闭注册，请返回登录")
        else if(mode=="reset" && !config.passwordResetEnabled)Text("邮箱服务未启用，请联系管理员重置密码")
        if(formAllowed) {
            OutlinedTextField(input.email,{ onInputChanged(input.copy(email=it)) },label={ Text("邮箱") },singleLine=true,keyboardOptions=KeyboardOptions(keyboardType=KeyboardType.Email),modifier=Modifier.fillMaxWidth())
            if(mode=="register")OutlinedTextField(input.name,{ onInputChanged(input.copy(name=it)) },label={ Text("昵称") },singleLine=true,modifier=Modifier.fillMaxWidth())
            OutlinedTextField(input.password,{ onInputChanged(input.copy(password=it)) },label={ Text(if(mode=="reset")"新密码" else "密码") },singleLine=true,visualTransformation=PasswordVisualTransformation(),modifier=Modifier.fillMaxWidth())
            Row(verticalAlignment=Alignment.CenterVertically,horizontalArrangement=Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(input.captchaText,{ onInputChanged(input.copy(captchaText=it)) },label={ Text("图形验证码") },singleLine=true,modifier=Modifier.weight(1f).testTag("account-captcha"))
                TextButton(enabled=!busy,onClick=onRefreshCaptcha) { captcha?.let { AsyncImage(model=runCatching { Base64.decode(it.imageBase64.substringAfter(','),Base64.DEFAULT) }.getOrNull(),contentDescription="点击刷新验证码",modifier=Modifier.size(112.dp,48.dp)) }?:Text("刷新") }
            }
            if(requiresEmailCode)Row(verticalAlignment=Alignment.CenterVertically) {
                OutlinedTextField(input.emailCode,{ onInputChanged(input.copy(emailCode=it)) },label={ Text("邮箱验证码") },singleLine=true,modifier=Modifier.weight(1f).testTag("account-email-code"))
                TextButton(enabled=!busy && input.email.isNotBlank() && captcha!=null && input.captchaText.isNotBlank(),onClick=onSendCode,modifier=Modifier.testTag("account-send-code")) { Text("发送验证码") }
            }
            Button(enabled=!busy && input.email.isNotBlank() && input.password.isNotBlank() && captcha!=null && input.captchaText.isNotBlank() && (!requiresEmailCode || input.emailCode.isNotBlank()),onClick=onSubmit,modifier=Modifier.fillMaxWidth().testTag("account-submit")) {
                Text(if(busy)"正在提交…" else when(mode) { "register"->"注册";"reset"->"更新密码";else->"登录" })
            }
        }
        Row {
            if(mode!="login")TextButton(enabled=!busy,onClick={ onModeChanged("login") }) { Text(if(mode=="register")"已有账号" else "返回登录") }
            else {
                if(config?.registrationEnabled==true)TextButton(enabled=!busy,onClick={ onModeChanged("register") }) { Text("注册账号") }
                if(config?.passwordResetEnabled==true)TextButton(enabled=!busy,onClick={ onModeChanged("reset") }) { Text("忘记密码") }
            }
        }
        if(config?.passwordResetEnabled==false && mode!="reset")Text("邮箱服务未启用，忘记密码请联系管理员",style=MaterialTheme.typography.bodySmall)
    }
}

@Composable private fun TvPairing(repo:AppRepository,cast:DeviceCast) {
    var challenge by remember { mutableStateOf<PairChallenge?>(null) };var message by remember { mutableStateOf("正在生成配对码…") };var retry by remember { mutableIntStateOf(0) }
    LaunchedEffect(retry) {
        try { val c=cast.challenge();challenge=c;message="在手机 APP 的账号页输入配对码";while(true) { delay(2500);when(cast.poll(c)) { "approved"->break;"expired"->{ message="配对码已过期，请重新生成";break } } } } catch(e:Throwable) { message=e.localizedMessage?:"配对失败" }
    }
    Column(Modifier.fillMaxSize().padding(40.dp),horizontalAlignment=Alignment.CenterHorizontally,verticalArrangement=Arrangement.spacedBy(20.dp,Alignment.CenterVertically)) {
        Row(verticalAlignment=Alignment.CenterVertically,horizontalArrangement=Arrangement.spacedBy(16.dp)) {
            CinemaBrandLogo(modifier=Modifier.size(64.dp))
            Text("登录小柒影视 TV",style=MaterialTheme.typography.headlineLarge)
        }
        challenge?.let { QrCode(it.qr_url.ifBlank { "xiaoqi://pair?code=${it.code}" },Modifier.size(240.dp));Text(it.code,style=MaterialTheme.typography.displaySmall) }
        Text(message);Button(onClick={ retry++ }) { Text("重新生成配对码") }
    }
}
