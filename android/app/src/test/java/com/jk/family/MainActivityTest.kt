package com.jk.family

import android.os.Looper
import android.os.Build
import android.content.ComponentName
import android.content.pm.ActivityInfo
import android.text.Editable
import android.text.TextWatcher
import android.view.View
import android.view.ViewGroup
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import java.time.Duration
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import org.robolectric.annotation.LooperMode

/** Android framework checks; these cannot emulate vivo's GPU driver or long-screenshot compositor. */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [25, 33], qualifiers = "w360dp-h640dp-mdpi")
@LooperMode(LooperMode.Mode.PAUSED)
class MainActivityTest {
    @Test fun legacyActivityUsesSoftwareRenderingWithoutDisablingModernRendering() {
        val controller = Robolectric.buildActivity(MainActivity::class.java).setup().visible()
        try {
            val activity = controller.get()
            val info = activity.packageManager.getActivityInfo(ComponentName(activity, MainActivity::class.java), 0)
            val hardware = info.flags and ActivityInfo.FLAG_HARDWARE_ACCELERATED != 0
            assertEquals("Android 7.1 uses the compatibility renderer; modern devices keep hardware drawing", Build.VERSION.SDK_INT > 25, hardware)
        } finally { controller.pause().stop().destroy() }
    }

    @Test fun repeatedStatusPollsDoNotRewriteUnchangedTextButNewStatusStillAppears() {
        val controller = Robolectric.buildActivity(MainActivity::class.java).setup().visible()
        try {
            val activity = controller.get()
            shadowOf(Looper.getMainLooper()).idle()
            val views = descendants(activity.findViewById(android.R.id.content)).filterIsInstance<TextView>()
            val previous = AppRuntime.message
            val status = views.first { it.text.toString() == previous }
            var changes = 0
            views.forEach { view ->
                view.addTextChangedListener(object : TextWatcher {
                    override fun beforeTextChanged(s: CharSequence?, start: Int, count: Int, after: Int) {}
                    override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) { changes++ }
                    override fun afterTextChanged(s: Editable?) {}
                })
            }
            shadowOf(Looper.getMainLooper()).idleFor(Duration.ofSeconds(3))
            assertEquals("unchanged polling must not invalidate text every second", 0, changes)
            AppRuntime.message = "测试状态已更新"
            shadowOf(Looper.getMainLooper()).idleFor(Duration.ofSeconds(1))
            assertEquals("测试状态已更新", status.text.toString())
            assertEquals("only changed status text should be replaced", 1, changes)
            AppRuntime.message = previous
        } finally { controller.pause().stop().destroy() }
    }

    @Test fun formAndActionsRemainSeparatedWhileScrollingAndRefreshing() {
        val controller = Robolectric.buildActivity(MainActivity::class.java).setup().visible()
        try {
            val activity = controller.get()
            val root = activity.findViewById<ViewGroup>(android.R.id.content)
            val scroll = descendants(root).filterIsInstance<ScrollView>().single()
            val content = scroll.getChildAt(0) as LinearLayout
            repeat(3) {
                scroll.measure(View.MeasureSpec.makeMeasureSpec(360, View.MeasureSpec.EXACTLY),
                    View.MeasureSpec.makeMeasureSpec(640, View.MeasureSpec.EXACTLY))
                scroll.layout(0, 0, 360, 640)
                val visible = (0 until content.childCount).map(content::getChildAt).filter { it.visibility == View.VISIBLE }
                assertTrue(visible.size > 5)
                visible.zipWithNext().forEach { (before, after) ->
                    assertTrue("adjacent controls overlap", before.bottom <= after.top)
                }
                scroll.scrollTo(0, it * 150)
                shadowOf(Looper.getMainLooper()).idleFor(Duration.ofSeconds(1))
            }
        } finally { controller.pause().stop().destroy() }
    }

    @Test fun backgroundSettingsButtonOpensSystemBatteryExemptionRequest() {
        val controller = Robolectric.buildActivity(MainActivity::class.java).setup().visible()
        try {
            val activity = controller.get()
            val action = descendants(activity.findViewById(android.R.id.content))
                .filterIsInstance<android.widget.Button>().firstOrNull { it.text.toString() == "允许后台运行" }
            assertNotNull("battery guidance must be available on both target Android generations", action)
            action!!.performClick()
            val intent = shadowOf(activity).nextStartedActivity
            assertEquals(android.provider.Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, intent.action)
            assertEquals("package:com.jk.family", intent.data.toString())
        } finally { controller.pause().stop().destroy() }
    }

    private fun descendants(view: View): List<View> = listOf(view) +
        if (view is ViewGroup) (0 until view.childCount).flatMap { descendants(view.getChildAt(it)) } else emptyList()
}
