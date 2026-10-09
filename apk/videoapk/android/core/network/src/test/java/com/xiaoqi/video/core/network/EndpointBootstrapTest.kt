package com.xiaoqi.video.core.network

import org.junit.Assert.*
import org.junit.Test

class EndpointBootstrapTest {
    private fun manifest(base: String = SiteHttp.BASE, schema: String = "1", project: String = "xiaoqi-video", api: String = "/suxinvideo/app/v1") =
        """{"schema":$schema,"project":"$project","base_url":"$base","api_path":"$api"}"""

    @Test fun validDomainAndPublicIpOriginsAreNormalized() {
        assertEquals(SiteHttp.BASE, EndpointBootstrap.parseManifest(manifest()))
        assertEquals("https://203.0.113.7:8600", EndpointBootstrap.parseManifest(manifest("https://203.0.113.7:8600/")))
    }

    @Test fun emptySharesExtractionHtmlAndMalformedJsonAreIgnored() {
        listOf("", "  ", "<html>提取文件</html>", "{", "null", "[]", "{}")
            .forEach { assertNull(it, EndpointBootstrap.parseManifest(it)) }
    }

    @Test fun schemaProjectAndApiPathMustMatchThisApp() {
        listOf(
            manifest(schema = "2"), manifest(schema = "1.5"), manifest(schema = "\"1\""),
            manifest(project = "another-project"), manifest(api = "/admin"),
            manifest(api = "/suxinvideo/app/v1/" )
        ).forEach { assertNull(it, EndpointBootstrap.parseManifest(it)) }
    }

    @Test fun originsCannotContainPlainHttpCredentialsPathsOrParameters() {
        listOf(
            "http://example.com:8600", "https://user:pass@example.com:8600",
            "https://example.com:8600/path", "https://example.com:8600/?token=secret",
            "https://example.com:8600/#fragment", "not-a-server"
        ).forEach { assertNull(it, EndpointBootstrap.parseManifest(manifest(it))) }
    }

    @Test fun passwordRedirectsOnlyRemainOnTheOriginalHttpsAuthority() {
        assertTrue(EndpointBootstrap.allowedShareUrl(EndpointBootstrap.SOURCE))
        assertTrue(EndpointBootstrap.allowedShareUrl("https://59.36.165.33:8976/another-path.json"))
        listOf(
            "http://59.36.165.33:8976/file.json", "https://59.36.165.33/file.json",
            "https://59.36.165.34:8976/file.json", "https://59.36.165.33.other.example:8976/file.json",
            "https://user:pass@59.36.165.33:8976/file.json"
        ).forEach { assertFalse(it, EndpointBootstrap.allowedShareUrl(it)) }
    }
}
