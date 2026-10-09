package com.xiaoqi.video.core.data

import android.content.Context
import androidx.room.Room
import com.google.gson.JsonElement
import com.google.gson.JsonObject
import com.xiaoqi.video.core.model.*
import com.xiaoqi.video.core.network.*
import com.xiaoqi.video.core.network.JsonWire.array
import com.xiaoqi.video.core.network.JsonWire.obj
import com.xiaoqi.video.core.network.JsonWire.string
import com.xiaoqi.video.core.network.JsonWire.long
import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import java.io.IOException

class AppRepository(val context: Context) {
 val api=ApiClient()
 val scope=CoroutineScope(SupervisorJob()+Dispatchers.Main.immediate)
 val store=SessionStore(context)
 val db=Room.databaseBuilder(context,ClientDatabase::class.java,"xiaoqi.db").build()
 val serverOrigin:StateFlow<String> = SiteHttp.endpoint
 private data class ServerRequest(val origin:String,val generation:Long)
 private fun currentServer()=ServerRequest(SiteHttp.currentBase,SiteHttp.generation)
 private fun ServerRequest.isCurrent()=origin==SiteHttp.currentBase&&generation==SiteHttp.generation
 private fun ServerRequest.requireCurrent() { if(!isCurrent())throw CancellationException("服务器已切换") }
 private fun catalogRecordKey(origin:String,path:String)="$origin|$path"
 private fun progressRecordKey(origin:String,owner:Long,id:Long)="$origin|$owner|$id"
 private val refreshLock=Mutex()
 private val sessionWriteLock=Mutex()
 private val sessionRevisions=mutableMapOf<String,Long>()
 private val catalogueRevisions=DetailRequests<ServerRequest,String>(scope,ttlMs=0,load={ server->
   server.requireCurrent()
   api.request("catalog/revision",accessToken="",expectedOrigin=server.origin).asJsonObject.string("revision").also { server.requireCurrent() }
 })
 private data class CatalogKey(val request:CatalogRequest,val server:ServerRequest)
 private val cataloguePages=CatalogPages<CatalogKey,FilmPage>(scope,revision={ key->catalogueRevisions.get(key.server) },load={ key->readFilmPage(key) })
 private data class DetailKey(val id:Long,val owner:Long,val token:String,val server:ServerRequest)
 private val detailRequests=DetailRequests<DetailKey,Detail>(scope,load={ key->readDetail(key) })
 private val detailWarmupLock=Mutex()
 private var detailWarmupKey:DetailKey?=null
 private var detailWarmupJob:Job?=null
 val session=MutableStateFlow<Session?>(null)
 val ready=MutableStateFlow(false)
 val notice=MutableStateFlow("")
 val changed=MutableStateFlow(0L)
 init {
   SiteHttp.initialize(context)
   api.refresh={ refreshSession() }
   scope.launch {
     serverOrigin.collectLatest { origin->
       store.sessions(origin).collect { saved->
         sessionWriteLock.withLock {
           if(origin==SiteHttp.currentBase&&saved.revision>=(sessionRevisions[origin]?:-1L)) {
             sessionRevisions[origin]=saved.revision
             api.setToken(saved.session?.accessToken.orEmpty(),origin)
             session.value=saved.session
             ready.value=true
           }
         }
       }
     }
   }
 }
 /** Call only after the candidate endpoint has passed its health check. */
 suspend fun changeServer(origin:String):Unit=withContext(Dispatchers.Main.immediate+NonCancellable) {
   val normalized=SiteHttp.normalizeOrigin(origin)?:throw IllegalArgumentException("服务器地址无效")
   if(!SiteHttp.selectEndpoint(normalized))return@withContext
   api.setToken("",normalized)
   session.value=null
   notice.value=""
   detailWarmupLock.withLock { detailWarmupJob?.cancel();detailWarmupJob=null;detailWarmupKey=null }
   detailRequests.clear()
   catalogueRevisions.clear()
   cataloguePages.clear()
   changed.value++
 }
 private suspend fun saveSession(next:Session?,server:ServerRequest,expectedToken:String?=null):Boolean=withContext(Dispatchers.Main.immediate) {
   sessionWriteLock.withLock {
     if(!server.isCurrent()||(expectedToken!=null&&session.value?.accessToken.orEmpty()!=expectedToken))return@withLock false
     val revision=store.save(next,server.origin)
     sessionRevisions[server.origin]=revision
     if(!server.isCurrent())return@withLock false
     api.setToken(next?.accessToken.orEmpty(),server.origin)
     session.value=next
     true
   }
 }
 suspend fun refreshSession():Boolean=refreshLock.withLock {
   val server=currentServer()
   val current=session.value?:return@withLock false
   return@withLock try {
     val next=JsonWire.decode<Session>(api.request("auth/refresh","POST",mapOf("refresh_token" to current.refreshToken,"device_id" to current.deviceId),retry=false,expectedOrigin=server.origin))
     saveSession(next,server,current.accessToken)
   } catch(e:ApiException) {
     if(e.status==401&&server.isCurrent()&&saveSession(null,server,current.accessToken))changed.value++
     false
   }
 }
 suspend fun login(email:String,password:String,challenge:String,captcha:String) {
   val server=currentServer()
   val id=store.deviceId()
   server.requireCurrent()
   val data=api.request("auth/login","POST",mapOf("email" to email,"password" to password,"challenge_id" to challenge,"captcha" to captcha,"device_id" to id,"device_name" to android.os.Build.MODEL),expectedOrigin=server.origin)
   val next=JsonWire.decode<Session>(data)
   if(!saveSession(next,server))throw CancellationException("服务器已切换")
 }
 suspend fun adoptSession(next:Session,origin:String=SiteHttp.currentBase) {
   val server=currentServer().copy(origin=origin)
   if(!saveSession(next,server))throw CancellationException("服务器已切换")
 }
 suspend fun logout(remote:Boolean=true) {
   val server=currentServer()
   val current=session.value
   if(remote)runCatching { api.request("auth/logout","POST",retry=false,accessToken=current?.accessToken.orEmpty(),expectedOrigin=server.origin) }
   if(saveSession(null,server,current?.accessToken.orEmpty()))changed.value++
 }
 suspend fun captcha()=JsonWire.decode<Captcha>(api.request("captcha"))
 suspend fun register(email:String,name:String,password:String,code:String,challenge:String,captcha:String) {
   api.request("auth/register","POST",mapOf("email" to email,"name" to name,"password" to password,"email_code" to code,"challenge_id" to challenge,"captcha" to captcha))
 }
 suspend fun emailCode(email:String,challenge:String,captcha:String,purpose:String="register")=api.request("auth/send-code","POST",mapOf("email" to email,"purpose" to purpose,"challenge_id" to challenge,"captcha" to captcha))
 suspend fun reset(email:String,code:String,password:String,challenge:String,captcha:String)=api.request("auth/reset","POST",mapOf("email" to email,"email_code" to code,"password" to password,"challenge_id" to challenge,"captcha" to captcha))
 suspend fun config():SiteConfig {
   return JsonWire.config(publicCatalogue("config"))
 }
 suspend fun categories():List<Category> = JsonWire.categories(publicCatalogue("categories"))
 suspend fun channels():List<Category> = JsonWire.categories(publicCatalogue("channels"))
 suspend fun home():HomeData {
   return JsonWire.home(publicCatalogue("home"))
 }
 private suspend fun publicCatalogue(path:String):JsonElement {
   val server=currentServer()
   try {
     val result=api.request(path,accessToken="",expectedOrigin=server.origin)
     server.requireCurrent()
     require(result.isJsonObject || result.isJsonArray) { "目录数据格式错误，请重试" }
     withContext(Dispatchers.IO) { db.records().put(StoredRecord("public_catalog",catalogRecordKey(server.origin,path),0,JsonWire.gson.toJson(result),System.currentTimeMillis())) }
     server.requireCurrent()
     return result
   } catch(e:IOException) {
     // A reachable server's authorization/policy/error response always wins over cached content.
     if(e is ApiException)throw e
     server.requireCurrent()
     return (if(path=="home")verifiedCachedHome(server) else cachedCatalogue(path,server))?:throw e
   }
 }
 private suspend fun cachedCatalogue(path:String,server:ServerRequest):JsonElement?=withContext(Dispatchers.IO) {
   server.requireCurrent()
   db.records().get("public_catalog",catalogRecordKey(server.origin,path))?.takeIf { System.currentTimeMillis()-it.updated in 0..10*60*1000L }?.let { runCatching { com.google.gson.JsonParser.parseString(it.json) }.getOrNull() }.also { server.requireCurrent() }
 }
 private suspend fun verifiedCachedHome(server:ServerRequest):JsonElement? {
   val cached=cachedCatalogue("home",server)?:return null
   val revision=cached.takeIf { it.isJsonObject }?.asJsonObject?.string("catalog_revision").orEmpty()
   if(revision.isBlank())return null
   // An unreachable policy check cannot certify old recommendations after an administrator blocks a title.
   val valid=try { withTimeoutOrNull(1000) { catalogueRevisions.get(server)==revision }==true }
   catch(e:CancellationException) { throw e }
   catch(_:IOException) { false }
   server.requireCurrent()
   return cached.takeIf { valid }
 }
 suspend fun cachedHome():HomeData? = verifiedCachedHome(currentServer())?.let(JsonWire::home)
 suspend fun films(query:String="",type:Long=0,page:Int=1,sort:String="time",area:String="",year:String="",channel:Long=0,fresh:Boolean=false):FilmPage =
   films(CatalogRequest(query,channel,type,page,sort,area,year),fresh)
 suspend fun films(request:CatalogRequest,fresh:Boolean=false):FilmPage {
   val server=currentServer()
   return cataloguePages.get(CatalogKey(request.normalized(),server),fresh).also { server.requireCurrent() }
 }
 suspend fun prefetchFilms(request:CatalogRequest):Boolean=cataloguePages.prefetch(CatalogKey(request.normalized(),currentServer()))
 suspend fun resolveFeatured(film:Film):Long {
   if(film.id>0)return film.id
   val server=currentServer()
   server.requireCurrent()
   return api.resolveFeaturedFilm(film,expectedOrigin=server.origin).also { server.requireCurrent() }
 }
 fun cancelCataloguePrefetch()=cataloguePages.pausePrefetch()
 private suspend fun readFilmPage(key:CatalogKey):CatalogResult<FilmPage> {
   key.server.requireCurrent()
   val request=key.request
   val d=api.request(request.apiPath(),query=request.queryParameters(),accessToken="",expectedOrigin=key.server.origin)
   key.server.requireCurrent()
   val result=JsonWire.filmPage(d,request.page,request.size)
   return CatalogResult(result,d.takeIf { it.isJsonObject }?.asJsonObject?.string("catalog_revision").orEmpty())
 }
 suspend fun detail(id:Long,fresh:Boolean=false):Detail {
   require(id>0) { "影片编号无效" }
   val key=DetailKey(id,session.value?.user?.id?:0L,api.token,currentServer())
   val detail=detailRequests.get(key,fresh)
   requireDetailSession(key)
   return detail
 }
 /** Instant metadata for the click-to-player handoff. Authorization is still obtained by resolve. */
 suspend fun cachedDetail(id:Long):Detail? {
   if(id<=0)return null
   val key=DetailKey(id,session.value?.user?.id?:0L,api.token,currentServer())
   requireDetailSession(key)
   val detail=detailRequests.peek(key)
   requireDetailSession(key)
   return detail
 }
 /** At most one focus warmup; a click joins it rather than duplicating the HTTP request. */
 suspend fun prefetchDetail(id:Long) {
   if(id<=0)return
   val key=DetailKey(id,session.value?.user?.id?:0L,api.token,currentServer())
   val job=currentCoroutineContext()[Job]?:return
   val begin=detailWarmupLock.withLock {
     if(detailWarmupKey==key&&detailWarmupJob?.isActive==true)false
     else { detailWarmupJob?.cancel();detailWarmupKey=key;detailWarmupJob=job;true }
   }
   if(!begin)return
   try { withTimeout(4500) { detail(id) } }
   catch(e:CancellationException) { throw e }
   catch(_:Throwable) { /* Focus warmup never interrupts browsing with a network error. */ }
   finally { withContext(NonCancellable) { detailWarmupLock.withLock { if(detailWarmupJob===job) { detailWarmupJob=null;detailWarmupKey=null } } } }
 }
 private suspend fun readDetail(key:DetailKey):Detail {
   val id=key.id
   val requestedOwner=key.owner
   requireDetailSession(key)
   val o=PlaybackAccess(key.owner).request({ session.value?.user?.id }) { token->
     api.request("films/$id",accessToken=token,expectedOrigin=key.server.origin).asJsonObject
   }
   requireDetailSession(key)
   if(o.has("history")&&o.get("history").isJsonObject) {
     val h=o.obj("history");val owner=requestedOwner.takeIf { it==session.value?.user?.id }?:0L
     if(owner>0)withContext(Dispatchers.IO) {
       val recordKey=progressRecordKey(key.server.origin,owner,id)
       val existing=db.records().get("progress",recordKey)
       val updated=h.long("updated")*1000
       if(existing==null||existing.updated<=updated)db.records().put(StoredRecord("progress",recordKey,owner,JsonWire.gson.toJson(h),updated))
     }
   }
   requireDetailSession(key)
   return Detail(JsonWire.film(o.obj("film","vod")),o.array("sources").mapNotNull(JsonWire::source).distinctBy { it.code },o.string("preferred_line"),o.array("comments").filter { it.isJsonObject }.map { JsonWire.decode<Comment>(it) }.distinctBy { it.id },JsonWire.films(o.array("related")))
 }
 private fun requireDetailSession(key:DetailKey) {
   key.server.requireCurrent()
   PlaybackAccess(key.owner).requireCurrent(session.value?.user?.id)
 }
 suspend fun resolve(identity:PlaybackIdentity):Playback {
   val server=currentServer()
   val access=PlaybackAccess(session.value?.user?.id?:0L)
   server.requireCurrent()
   val data=access.request({ session.value?.user?.id }) { token->
     api.request("playback/resolve","POST",playBody(identity),accessToken=token,expectedOrigin=server.origin)
   }
   server.requireCurrent()
   return JsonWire.decode(data)
 }
 suspend fun authorize(identity:PlaybackIdentity):License=JsonWire.decode(api.request("downloads/authorize","POST",playBody(identity)+("device_id" to store.deviceId())))
 suspend fun renewLicense(id:String):License=JsonWire.decode(api.request("downloads/renew","POST",mapOf("id" to id)))
 fun playBody(identity:PlaybackIdentity):Map<String,Any> = mapOf("vod_id" to identity.vodId,"line" to identity.line,"version_key" to identity.versionKey,"episode_key" to identity.episodeKey,"episode" to identity.episode,"quality" to identity.quality,"position_ms" to identity.positionMs,"manual" to identity.manual)
 suspend fun me():User {
   val server=currentServer()
   val current=session.value
   val u=JsonWire.decode<User>(api.request("me",expectedOrigin=server.origin))
   server.requireCurrent()
   session.value?.takeIf { it.user.id==current?.user?.id&&it.user.id==u.id }?.let { saveSession(it.copy(user=u),server,it.accessToken) }
   server.requireCurrent()
   return u
 }
 suspend fun favorites():List<Film> { val d=api.request("favorites");return JsonWire.films(if(d.isJsonArray)d else d.asJsonObject.array("items","films")) }
 suspend fun favorite(id:Long,value:Boolean) {
   api.request("favorites","POST",mapOf("vod_id" to id,"favorite" to value))
   detailRequests.invalidate { it.id==id }
   changed.value++
 }
 suspend fun histories():List<History> {
   val d=api.request("history");val a=if(d.isJsonArray)d.asJsonArray else d.asJsonObject.array("items","history")
   return a.map { e->val o=e.asJsonObject;History(JsonWire.film(if(o.has("film"))o.obj("film") else o),o.string("line","source_code"),o.string("episode_key"),o.string("version_key"),o.long("position_ms"),o.long("duration_ms")) }
 }
 suspend fun record(p:Playback,position:Long,duration:Long) {
   val server=currentServer()
   val current=session.value?:return
   val owner=current.user.id
   val data=mapOf("vod_id" to p.vodId,"line" to p.line,"episode_key" to p.episodeKey,"version_key" to p.versionKey,"position_ms" to position,"duration_ms" to duration,"quality" to p.quality)
   withContext(Dispatchers.IO) { db.records().put(StoredRecord("progress",progressRecordKey(server.origin,owner,p.vodId),owner,JsonWire.gson.toJson(data),System.currentTimeMillis())) }
   if(!server.isCurrent()||session.value?.accessToken!=current.accessToken)return
   runCatching { api.request("history","POST",data,retry=false,accessToken=current.accessToken,expectedOrigin=server.origin) }
   if(server.isCurrent()&&session.value?.accessToken==current.accessToken)changed.value++
 }
 suspend fun progress(id:Long):JsonObject? {
   val server=currentServer()
   val owner=session.value?.user?.id?:0L
   val result=PlaybackAccess(owner).progress({ session.value?.user?.id }) { account->
     withContext(Dispatchers.IO) {
       db.records().get("progress",progressRecordKey(server.origin,account,id))?.takeIf { it.owner==account }?.let { JsonWire.gson.fromJson(it.json,JsonObject::class.java) }
     }
   }
   server.requireCurrent()
   PlaybackAccess(owner).requireCurrent(session.value?.user?.id)
   return result
 }
 suspend fun comment(id:Long,content:String):JsonElement {
   val result=api.request("comments","POST",mapOf("vod_id" to id,"content" to content))
   detailRequests.invalidate { it.id==id }
   return result
 }
 suspend fun unlock(id:Long):JsonElement {
   val result=api.request("unlock","POST",mapOf("vod_id" to id))
   detailRequests.invalidate { it.id==id }
   return result
 }
 suspend fun packages():List<Package> { val d=api.request("packages");val a=if(d.isJsonArray)d.asJsonArray else d.asJsonObject.array("items","packages");return a.map { JsonWire.decode<Package>(it) } }
 suspend fun payment(id:Long,type:String):String=api.request("payment/create","POST",mapOf("goods_id" to id,"pay_type" to type)).asJsonObject.string("url","cashier_url")
 suspend fun release(platform:String,version:Int):Release? {
   // Updates are public and must still work when the member token has expired or login is unavailable.
   val o=api.request("releases/latest",query=mapOf("platform" to platform,"version_code" to version.toString()),accessToken="",retry=false).asJsonObject
   return if(o.string("available")=="true")JsonWire.decode(o.obj("release")) else null
 }
 fun error(e:Throwable) { notice.value=if(e is ApiException)e.message else (e.localizedMessage?: "网络连接失败，请稍后重试") }
}
object AppGraph {
 lateinit var repository:AppRepository
 fun initialize(context:Context) { if(!::repository.isInitialized)repository=AppRepository(context.applicationContext) }
}

