package com.xiaoqi.video.mobile

import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.*
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import com.xiaoqi.video.core.design.CinemaTheme
import com.xiaoqi.video.core.model.Captcha
import com.xiaoqi.video.core.model.SiteConfig
import com.xiaoqi.video.feature.account.AccountAuthForm
import com.xiaoqi.video.feature.account.AccountInput
import org.junit.*
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class MobileRegistrationPolicyTest {
    @get:Rule val compose=createAndroidComposeRule<MainActivity>()

    private fun show(mode:String,config:SiteConfig?,loading:Boolean=false,error:String?=null) {
        compose.activityRule.scenario.onActivity { activity->activity.setContent {
            var input by remember { mutableStateOf(AccountInput(email="member@example.test",name="测试会员",password="test-password",captchaText="1234")) }
            CinemaTheme { Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp)) {
                AccountAuthForm(mode,config,input,Captcha("challenge","","image/png",0),false,loading,error,
                    onInputChanged={ input=it },onModeChanged={},onRetryConfig={},onRefreshCaptcha={},onSendCode={},onSubmit={})
            } }
        } }
    }

    @Test fun noSmtpRegistrationKeepsGraphicCaptchaAndNeedsNoEmailCode() {
        show("register",SiteConfig(registrationRequiresEmailCode=false,passwordResetEnabled=false))
        compose.onNodeWithTag("account-email-code").assertDoesNotExist()
        compose.onNodeWithTag("account-send-code").assertDoesNotExist()
        compose.onNodeWithTag("account-captcha").assertExists()
        compose.onNodeWithTag("account-submit").assertIsEnabled()
    }

    @Test fun configuredSmtpRegistrationRequiresEmailCodeAsWellAsGraphicCaptcha() {
        show("register",SiteConfig(registrationRequiresEmailCode=true,passwordResetEnabled=true))
        compose.onNodeWithTag("account-email-code").assertExists()
        compose.onNodeWithTag("account-send-code").assertExists()
        compose.onNodeWithTag("account-captcha").assertExists()
        compose.onNodeWithTag("account-submit").assertIsNotEnabled()
        compose.onNodeWithTag("account-email-code").performTextInput("123456")
        compose.onNodeWithTag("account-submit").assertIsEnabled()
    }

    @Test fun pendingPolicyDoesNotBrieflyRenderRegistrationRequirements() {
        show("register",null,loading=true)
        compose.onNodeWithTag("account-email-code").assertDoesNotExist()
        compose.onNodeWithTag("account-submit").assertDoesNotExist()
        compose.onNodeWithText("正在读取注册与找回密码设置…").assertExists()
    }

    @Test fun policyFailureAllowsLoginAndOffersRetry() {
        show("login",null,error="网络连接失败")
        compose.onNodeWithTag("account-config-retry").assertExists()
        compose.onNodeWithTag("account-submit").assertIsEnabled()
        compose.onNodeWithTag("account-email-code").assertDoesNotExist()
    }

    @Test fun noSmtpNeverOffersResetWithoutEmailVerification() {
        show("reset",SiteConfig(registrationRequiresEmailCode=false,passwordResetEnabled=false))
        compose.onNodeWithText("邮箱服务未启用，请联系管理员重置密码").assertExists()
        compose.onNodeWithTag("account-submit").assertDoesNotExist()
        compose.onNodeWithTag("account-email-code").assertDoesNotExist()
    }

    @Test fun disabledRegistrationRemainsClosedEvenWithoutSmtp() {
        show("register",SiteConfig(registrationEnabled=false,registrationRequiresEmailCode=false,passwordResetEnabled=false))
        compose.onNodeWithText("当前已关闭注册，请返回登录").assertExists()
        compose.onNodeWithTag("account-submit").assertDoesNotExist()
    }

    @Test fun loginAndRegistrationUseTheSiteBrandRatherThanAnAvatar() {
        listOf("login","register").forEach { mode->
            show(mode,SiteConfig(registrationRequiresEmailCode=false,passwordResetEnabled=false))
            compose.onNodeWithTag("account-brand-logo").assertIsDisplayed()
            compose.onNodeWithContentDescription("小柒影视图标").assertExists()
            compose.onNodeWithContentDescription("用户头像").assertDoesNotExist()
        }
    }
}
