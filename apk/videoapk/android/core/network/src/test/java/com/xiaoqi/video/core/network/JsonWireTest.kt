package com.xiaoqi.video.core.network
import com.google.gson.JsonParser
import com.xiaoqi.video.core.model.Source
import org.junit.Assert.*
import org.junit.Test
class JsonWireTest {
    @Test fun featuredCatalogueKeepsRealTopicsNoticeAndLocalFilmIdentity() {
        val page=JsonWire.filmPage(JsonParser.parseString("""{"items":[{"id":2164,"name":"精选影片"}],"total":61,"page":2,"size":30,"pages":3,"topics":[{"id":91,"name":"近期精选"},{"id":0,"name":"无效专题"}],"notice":"频道精选"}"""),2,30)
        assertEquals(2164L,page.items.single().id)
        assertEquals(91L,page.topics.single().id)
        assertEquals("近期精选",page.topics.single().name)
        assertEquals("频道精选",page.notice)
        assertEquals(3,page.pages)
    }

    @Test fun cursorCatalogueDoesNotPresentAnUnknownTotalAsExactPages() {
        val page=JsonWire.filmPage(JsonParser.parseString("""{"items":[],"page":2,"pages":3,"exact_pages":false}"""))
        assertFalse(page.exactPages)
        assertEquals(3,page.pages)
    }

    @Test fun realChannelResponseKeepsAllThirtyCardsAndServerPagination() {
        val json=javaClass.classLoader!!.getResourceAsStream("catalog-channel-30-response.json")!!.bufferedReader().use { it.readText() }
        val envelope=JsonParser.parseString(json)
        val page=JsonWire.filmPage(envelope,1,30)
        assertEquals(30,page.items.size)
        assertEquals(30,page.items.map { it.id }.distinct().size)
        assertEquals(95022L,page.items.first().id)
        assertEquals(1,page.page)
        assertEquals(27418,page.total)
        assertEquals(914,page.pages)
        assertEquals(page,JsonWire.filmPage(envelope.asJsonObject.get("data"),1,30))
    }

    @Test fun pageDecoderRetainsFallbackCapacityForOlderServers() {
        val page=JsonWire.filmPage(JsonParser.parseString("""{"items":[{"id":1,"name":"影片"}],"total":61,"page":2}"""),2,30)
        assertEquals(3,page.pages);assertEquals(2,page.page)
        assertEquals(1,page.items.size)
    }
    @Test fun acceptsNumericStringsFromExistingDatabaseRows() {
        val film=JsonWire.film(JsonParser.parseString("""{"id":"2164","name":"凡人修仙传","score":"9.1","type_id":8,"vip":1,"pic":"/poster.jpg"}"""))
        assertEquals(2164L,film.id);assertEquals(9.1,film.score,0.01);assertTrue(film.vip)
    }
    @Test fun retainsStableVersionAndEpisodeIdentity() {
        val source=JsonWire.decode<Source>(JsonParser.parseString("""{"code":"yqk_1","base_code":"yqk_1","version_key":"tv-cn","name":"小柒APP","episodes":[{"key":"e:193","number":193,"name":"第193集"}]}"""))
        assertEquals("tv-cn",source.versionKey);assertEquals("e:193",source.episodes.single().key)
    }
    @Test fun missingOptionalFilmFieldsUseSafeDefaults() {
        val film=JsonWire.film(JsonParser.parseString("""{"id":4,"name":"测试"}"""));assertEquals("",film.pic);assertEquals(0.0,film.score,0.0)
    }
    @Test fun catalogueIgnoresMalformedRowsAndDuplicateLazyKeys() {
        val rows=JsonWire.films(JsonParser.parseString("""{"items":[null,2,{"id":0,"name":"远端未导入"},{"id":"12.0","name":"有效影片","score":"NaN"},{"id":12,"name":"重复"},{"id":13,"name":""}]}"""))
        assertEquals(1,rows.size);assertEquals(12L,rows.single().id);assertEquals(0.0,rows.single().score,0.0)
    }
    @Test fun realHomeShapeRetainsRecentHotAndChannelSections() {
        val home=JsonWire.home(JsonParser.parseString("""{"banners":[{"vod_id":781,"name":"海报","pic":"/poster.jpg","link":"/suxinvideo/detail?id=781"}],"recent":[{"id":1,"name":"更新"}],"hot":[{"id":2,"name":"热播"}],"sections":[{"id":8,"name":"动漫","items":[{"id":3,"name":"仙逆"}]}]}"""))
        assertEquals(781L,home.banners.single().filmId);assertEquals("/poster.jpg",home.banners.single().image)
        assertEquals(listOf("recent","hot","channel:8"),home.sections.map { it.key })
        assertEquals(listOf(1L,2L,3L),home.sections.flatMap { it.items }.map { it.id })
    }
    @Test fun duplicateOrWrappedHomeSectionsMergeWithoutKeyCollision() {
        val home=JsonWire.home(JsonParser.parseString("""{"recent":{"items":[{"id":1,"name":"电影"}]},"sections":[null,{"id":8,"name":"动漫","items":[{"id":3,"name":"仙逆"},{"id":3,"name":"重复"}]},{"id":8,"name":"动漫","items":[{"id":4,"name":"凡人"}]},{"name":"最近更新","items":[{"id":1,"name":"重复"}]}]}"""))
        assertEquals(2,home.sections.size);assertEquals(home.sections.size,home.sections.map { it.key }.toSet().size)
        assertEquals(listOf(3L,4L),home.sections.first { it.typeId==8L }.items.map { it.id })
    }
    @Test fun configurationFollowsTheWebsiteThemeAndAllowsFourStyles() {
        assertEquals("guoguo",JsonWire.config(JsonParser.parseString("""{"template":"guoguo","payment_methods":null}""")).theme)
        assertEquals("suxinpro",JsonWire.config(JsonParser.parseString("""{"theme":"suxinpro","registration_enabled":false}""")).theme)
        assertFalse(JsonWire.config(JsonParser.parseString("""{"registration_enabled":0}""")).registrationEnabled)
        assertEquals("suxinlite",JsonWire.config(JsonParser.parseString("""{"theme":"unsupported"}""")).theme)
        assertEquals("follow",com.xiaoqi.video.core.model.AppearanceRules.preference("unsupported"))
        assertEquals("iqiyi",com.xiaoqi.video.core.model.AppearanceRules.preference("IQIYI"))
    }
    @Test fun categoriesAndMissingSourceEpisodesUseSafeNonNullLists() {
        val categories=JsonWire.categories(JsonParser.parseString("""{"categories":[null,{"id":"8","name":"动漫","pid":"0"},{"id":8,"name":"重复"}]}"""))
        assertEquals(1,categories.size);assertEquals(8L,categories.single().id)
        val source=JsonWire.source(JsonParser.parseString("""{"code":"yqk_1","name":"小柒APP","episodes":null}"""))
        assertNotNull(source);assertTrue(source!!.episodes.isEmpty())
    }
    @Test fun registrationEmailCapabilityAcceptsBooleanAndExistingFlagRepresentations() {
        listOf("false","0","\"off\"","\"false\"").forEach { flag->
            val config=JsonWire.config(JsonParser.parseString("""{"registration_requires_email_code":$flag,"password_reset_enabled":$flag}"""))
            assertFalse(config.registrationRequiresEmailCode);assertFalse(config.passwordResetEnabled)
        }
        listOf("true","1","\"true\"").forEach { flag->
            val config=JsonWire.config(JsonParser.parseString("""{"registration_requires_email_code":$flag,"password_reset_enabled":$flag}"""))
            assertTrue(config.registrationRequiresEmailCode);assertTrue(config.passwordResetEnabled)
        }
    }
    @Test fun absentOrInvalidEmailCapabilitiesNeverRelaxOlderServerRequirements() {
        listOf("{}","""{"registration_requires_email_code":null,"password_reset_enabled":null}""","""{"registration_requires_email_code":{},"password_reset_enabled":[]}""").forEach { json->
            val config=JsonWire.config(JsonParser.parseString(json))
            assertTrue(config.registrationRequiresEmailCode);assertTrue(config.passwordResetEnabled)
        }
    }
    @Test fun mailCapabilityDoesNotEnableAdministrativelyClosedRegistration() {
        val config=JsonWire.config(JsonParser.parseString("""{"registration_enabled":false,"registration_requires_email_code":false,"password_reset_enabled":false}"""))
        assertFalse(config.registrationEnabled);assertFalse(config.registrationRequiresEmailCode);assertFalse(config.passwordResetEnabled)
    }
}
