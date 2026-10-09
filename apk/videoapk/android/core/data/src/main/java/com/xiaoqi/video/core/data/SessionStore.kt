package com.xiaoqi.video.core.data

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import androidx.datastore.preferences.core.*
import androidx.datastore.preferences.preferencesDataStore
import com.xiaoqi.video.core.model.Session
import com.xiaoqi.video.core.model.AppearanceRules
import com.xiaoqi.video.core.model.CatalogDisplay
import com.xiaoqi.video.core.network.JsonWire
import com.xiaoqi.video.core.network.SiteHttp
import kotlinx.coroutines.flow.*
import java.security.KeyStore
import java.util.UUID
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec
import android.util.Base64
import java.io.IOException

private val Context.memberData by preferencesDataStore("member")

internal object SessionPartitions {
 val legacyKey = stringPreferencesKey("encrypted_session")
 val legacyOriginKey = stringPreferencesKey("encrypted_session_origin")
 fun key(origin:String)=stringPreferencesKey("encrypted_session:$origin")
 fun revisionKey(origin:String)=longPreferencesKey("encrypted_session_revision:$origin")
 fun nextRevision(prefs:MutablePreferences,origin:String):Long {
   val key=revisionKey(origin)
   return ((prefs[key]?:0L)+1).also { prefs[key]=it }
 }
 fun migrate(prefs:MutablePreferences,defaultOrigin:String,normalize:(String)->String?) {
   val legacy=prefs[legacyKey]?:return
   val markedOrigin=prefs[legacyOriginKey]
   // An unmarked session belongs only to the original installation's default site.
   val origin=if(markedOrigin==null)defaultOrigin else normalize(markedOrigin)?:return
   val destination=key(origin)
   if(prefs[destination]==null)prefs[destination]=legacy
   prefs.remove(legacyKey)
   prefs.remove(legacyOriginKey)
 }
}

internal data class PersistedSession(val session:Session?,val revision:Long)

class SessionStore(private val context: Context) {
 private val device = stringPreferencesKey("device_id")
 private val appearanceKey = stringPreferencesKey("appearance")
 private val catalogColumnsKey = intPreferencesKey("catalog_columns")
 private val catalogRowsKey = intPreferencesKey("catalog_rows")
 private val preferences=context.memberData.data.catch { e -> if(e is IOException)emit(emptyPreferences()) else throw e }
 internal fun sessions(origin:String):Flow<PersistedSession> = preferences
   .map { prefs ->
     val session=prefs[SessionPartitions.key(origin)]?.let { runCatching { JsonWire.gson.fromJson(decrypt(it),Session::class.java) }.getOrNull() }
     PersistedSession(session,prefs[SessionPartitions.revisionKey(origin)]?:0L)
   }
   .onStart {
     try { context.memberData.edit { SessionPartitions.migrate(it,SiteHttp.BASE,SiteHttp::normalizeOrigin) } }
     catch(_:IOException) { /* The preferences flow preserves anonymous startup when storage is unreadable. */ }
   }
   .distinctUntilChanged()
 val appearance:Flow<String> = preferences.map { AppearanceRules.preference(it[appearanceKey]?:"follow") }.distinctUntilChanged()
 val catalogDisplay:Flow<CatalogDisplay> = preferences.map { prefs ->
   CatalogDisplay(columns=prefs[catalogColumnsKey]?:0,rows=prefs[catalogRowsKey]?:10).normalized()
 }.distinctUntilChanged()
 suspend fun setAppearance(value:String) { context.memberData.edit { it[appearanceKey]=AppearanceRules.preference(value) } }
 suspend fun setCatalogDisplay(value:CatalogDisplay) {
   val display=value.normalized()
   context.memberData.edit { prefs ->
     prefs[catalogColumnsKey]=display.columns
     prefs[catalogRowsKey]=display.rows
   }
 }
 suspend fun deviceId(): String {
   context.memberData.data.first()[device]?.let { return it }
   val id=UUID.randomUUID().toString();context.memberData.edit { it[device]=id };return id
 }
 suspend fun save(session:Session?,origin:String):Long {
   var revision=0L
   context.memberData.edit {
     SessionPartitions.migrate(it,SiteHttp.BASE,SiteHttp::normalizeOrigin)
     val encrypted=SessionPartitions.key(origin)
     if(session==null)it.remove(encrypted) else it[encrypted]=encrypt(JsonWire.gson.toJson(session))
     revision=SessionPartitions.nextRevision(it,origin)
   }
   return revision
 }
 private fun key(): SecretKey {
   val store=KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
   (store.getKey("xiaoqi_member",null) as? SecretKey)?.let { return it }
   return KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES,"AndroidKeyStore").apply {
     init(KeyGenParameterSpec.Builder("xiaoqi_member",KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT).setBlockModes(KeyProperties.BLOCK_MODE_GCM).setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE).build())
   }.generateKey()
 }
 private fun encrypt(value:String):String {
   val cipher=Cipher.getInstance("AES/GCM/NoPadding").apply { init(Cipher.ENCRYPT_MODE,key()) }
   return Base64.encodeToString(cipher.iv+cipher.doFinal(value.toByteArray(Charsets.UTF_8)),Base64.NO_WRAP)
 }
 private fun decrypt(value:String):String {
   val bytes=Base64.decode(value,Base64.NO_WRAP)
   return Cipher.getInstance("AES/GCM/NoPadding").run { init(Cipher.DECRYPT_MODE,key(),GCMParameterSpec(128,bytes.copyOfRange(0,12)));String(doFinal(bytes.copyOfRange(12,bytes.size)),Charsets.UTF_8) }
 }
}

