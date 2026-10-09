package com.xiaoqi.video.core.network

import kotlinx.coroutines.*
import org.junit.Assert.*
import org.junit.Test
import java.io.IOException
import java.util.concurrent.TimeUnit

class EndpointDiscoveryTest {
    private fun runCase(test:suspend (CoroutineScope)->Unit)=runBlocking {
        val scope=CoroutineScope(SupervisorJob()+Dispatchers.Unconfined)
        try { withTimeout(5000) { test(scope) } } finally { scope.cancel() }
    }

    @Test fun concurrentChecksJoinOneBoundedAnonymousDiscoveryAndLaterChecksCanRetry()=runCase { scope->
        val gate=CompletableDeferred<Unit>();var reads=0;var verifies=0;val saved=mutableListOf<String>();val deadlines=mutableListOf<Long>()
        val discovery=EndpointDiscovery(scope,fetch={ deadline->reads++;deadlines+=deadline;gate.await();"https://new.example:8600" },
            verify={ _,deadline->verifies++;deadlines+=deadline;true },remember={ saved+=it;true },now={ 20L })
        assertTrue(discovery.check());assertTrue(discovery.state.value.checking)
        assertFalse(discovery.check());assertEquals(1,reads)
        gate.complete(Unit);yield()
        assertFalse(discovery.state.value.checking)
        assertEquals("https://new.example:8600",discovery.state.value.verifiedCandidate)
        assertEquals(listOf(20L+TimeUnit.SECONDS.toNanos(4),20L+TimeUnit.SECONDS.toNanos(4)),deadlines)
        assertEquals(1,verifies);assertEquals(1,saved.size)
        assertTrue(discovery.check());yield();assertEquals(2,reads)
    }

    @Test fun malformedUnhealthyAndUntrustedCandidatesAreNeverPersisted()=runCase { scope->
        var content:String?=null;var valid=false;var saved=0
        val discovery=EndpointDiscovery(scope,fetch={ content },verify={ _,_->valid },remember={ saved++;true })
        discovery.check();yield();assertEquals(0,saved);assertEquals("",discovery.state.value.verifiedCandidate)
        content="https://invalid.example:8600";discovery.check();yield()
        assertEquals(0,saved);assertEquals("",discovery.state.value.verifiedCandidate)
        valid=true;discovery.check();yield();assertEquals(1,saved)
    }

    @Test fun failedRecheckClearsOldCandidateAndEnglishTransportErrorsUseChineseText()=runCase { scope->
        var fail=false
        val discovery=EndpointDiscovery(scope,fetch={ if(fail)throw IOException("timeout");"https://new.example:8600" },verify={ _,_->true },remember={ true })
        discovery.check();yield();assertTrue(discovery.state.value.verifiedCandidate.isNotBlank())
        fail=true;discovery.check();yield()
        assertEquals("",discovery.state.value.verifiedCandidate);assertFalse(discovery.state.value.checking)
        assertEquals("服务地址检查失败，请检查网络后重试",discovery.state.value.status)
    }

    @Test fun expiredSharedDeadlinePreventsPersistenceEvenWhenVerificationReturnsSuccess()=runCase { scope->
        var time=10L;var saved=0
        val discovery=EndpointDiscovery(scope,fetch={ "https://new.example:8600" },verify={ _,deadline->time=deadline+1;true },remember={ saved++;true },now={ time })
        discovery.check();yield()
        assertEquals(0,saved);assertEquals("",discovery.state.value.verifiedCandidate)
        assertEquals("服务地址检查超时，请重试",discovery.state.value.status)
    }

    @Test fun cancelledDiscoveryReleasesSingleFlightForAnotherAttempt()=runCase { scope->
        var cancelled=true;var saved=0
        val discovery=EndpointDiscovery(scope,fetch={ if(cancelled)throw CancellationException("stopped");"https://new.example:8600" },verify={ _,_->true },remember={ saved++;false })
        discovery.check();yield();assertFalse(discovery.state.value.checking);assertEquals(0,saved)
        cancelled=false;assertTrue(discovery.check());yield();assertEquals(1,saved)
        assertEquals("当前服务地址有效，可以重试加载",discovery.state.value.status)
    }

    @Test fun startupRefreshUsesFreshVerifiedOriginWithoutHealthWaitOrSuccessNotice()=runCase { scope->
        val gate=CompletableDeferred<Unit>();var fetches=0;var verifies=0;var remembers=0;var notices=0
        val origin="https://current.example:8600"
        val discovery=EndpointDiscovery(scope,fetch={ fetches++;gate.await();origin },verify={ _,_->verifies++;true },
            remember={ remembers++;false },recentlyVerified={ it==origin },currentOrigin={ origin })
        assertTrue(discovery.refresh { notices++ })
        assertEquals(1,fetches)
        assertEquals(EndpointDiscoveryState(),discovery.state.value)
        assertFalse(discovery.refresh())
        gate.complete(Unit);yield()
        assertEquals(0,verifies);assertEquals(0,remembers);assertEquals(0,notices)
        assertEquals(EndpointDiscoveryState(),discovery.state.value)
    }

    @Test fun startupRefreshVerifiesNewAddressButNeverChangesActiveOrigin()=runCase { scope->
        val origin="https://current.example:8600";val next="https://new.example:8600"
        var verifies=0;var remembered="";var notices=0
        val discovery=EndpointDiscovery(scope,fetch={ next },verify={ candidate,_->assertEquals(next,candidate);verifies++;true },
            remember={ remembered=it;true },recentlyVerified={ it==origin },currentOrigin={ origin })
        discovery.refresh { notices++ };yield()
        assertEquals(1,verifies);assertEquals(next,remembered);assertEquals(1,notices)
        assertEquals(next,discovery.state.value.verifiedCandidate)
    }

    @Test fun backgroundFailureIsSilentAndManualRecoveryStillForcesRealVerification()=runCase { scope->
        val origin="https://current.example:8600";var unavailable=true;var verifies=0;var remembers=0
        val discovery=EndpointDiscovery(scope,fetch={ if(unavailable)throw IOException("timeout");origin },
            verify={ _,_->verifies++;false },remember={ remembers++;false },
            recentlyVerified={ true },currentOrigin={ origin })
        discovery.refresh();yield()
        assertEquals(EndpointDiscoveryState(),discovery.state.value);assertEquals(0,remembers)
        unavailable=false;discovery.check();yield()
        assertEquals(1,verifies);assertEquals(0,remembers)
        assertEquals("配置中的服务暂时无法连接，已保留当前地址",discovery.state.value.status)
    }

    @Test fun firstStartupWithoutVerificationValidatesAndPersistsWithoutAnnouncingCurrentAddress()=runCase { scope->
        val origin="https://current.example:8600";var verifies=0;var remembers=0
        val discovery=EndpointDiscovery(scope,fetch={ origin },verify={ _,_->verifies++;true },
            remember={ remembers++;false },currentOrigin={ origin })
        discovery.refresh();yield()
        assertEquals(1,verifies);assertEquals(1,remembers)
        assertEquals(EndpointDiscoveryState(),discovery.state.value)
    }

    @Test fun manualCheckDuringSilentRefreshJoinsAndShowsItsRealVerificationResult()=runCase { scope->
        val origin="https://current.example:8600";val gate=CompletableDeferred<Unit>()
        var fetches=0;var verifies=0
        val discovery=EndpointDiscovery(scope,fetch={ fetches++;gate.await();origin },verify={ _,_->verifies++;false },
            remember={ fail("Unhealthy candidates must not be saved");false },recentlyVerified={ true },currentOrigin={ origin })
        assertTrue(discovery.refresh());assertEquals(EndpointDiscoveryState(),discovery.state.value)
        assertFalse(discovery.check());assertTrue(discovery.state.value.checking)
        gate.complete(Unit);yield()
        assertEquals(1,fetches);assertEquals(1,verifies)
        assertEquals("配置中的服务暂时无法连接，已保留当前地址",discovery.state.value.status)
        assertFalse(discovery.state.value.checking)
    }
}
