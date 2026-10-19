package com.jk.family

import android.content.ClipData
import android.content.ClipboardManager
import android.os.Looper
import android.view.View
import android.view.ViewGroup
import android.widget.Button
import android.widget.EditText
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import org.robolectric.annotation.LooperMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [25, 33])
@LooperMode(LooperMode.Mode.PAUSED)
class PairingImportTest {
    private val json = """{"server_url":"http://192.0.2.1:8080/","pairing_id":"ABCDEFGH2345","code":"00123456"}"""

    @Test fun focusedEmptyFormImportsClipboardWithoutPairingOrStartingSharing() {
        withForm { activity, inputs ->
            inputs[3].setText("爸爸的手机")
            copy(json)
            activity.onWindowFocusChanged(true)
            shadowOf(Looper.getMainLooper()).idle()
            assertEquals(listOf("http://192.0.2.1:8080", "ABCDEFGH2345", "00123456", "爸爸的手机"), inputs.map { it.text.toString() })
            assertFalse(AppRuntime.pairing)
            assertFalse(CredentialStore(activity).sharingRequested)
            assertNull(shadowOf(activity.application).nextStartedService)
        }
    }

    @Test fun manualFieldsAreProtectedButExplicitPasteReplacesAllPairingFields() {
        withForm { activity, inputs ->
            inputs[0].setText("http://192.0.2.2")
            copy(json)
            activity.onWindowFocusChanged(true)
            assertEquals("http://192.0.2.2", inputs[0].text.toString())
            assertEquals("", inputs[1].text.toString())
            paste(activity)
            assertEquals("http://192.0.2.1:8080", inputs[0].text.toString())
            assertEquals("00123456", inputs[2].text.toString())
        }
    }

    @Test fun invalidClipboardNeverPartiallyChangesFields() {
        withForm { activity, inputs ->
            val original = listOf("http://192.0.2.2", "old-id", "87654321", "家人")
            inputs.zip(original).forEach { (input, value) -> input.setText(value) }
            listOf("unrelated text", "12345678", "{}", json.replace("\"00123456\"", "12345678"),
                json.replace("00123456", "123"), json.replace("http://192.0.2.1:8080/", "https://user:password@example.com/"),
                json.replace("http://192.0.2.1:8080/", "https://example.com/path"), " ".repeat(4097) + json).forEach {
                copy(it)
                paste(activity)
                assertEquals(original, inputs.map { field -> field.text.toString() })
            }
        }
    }

    @Test fun oldPairingIdCanStillBeImportedAndFocusLossDoesNothing() {
        withForm { activity, inputs ->
            copy(json.replace("ABCDEFGH2345", "x".repeat(43)))
            activity.onWindowFocusChanged(false)
            assertEquals("", inputs[1].text.toString())
            activity.onWindowFocusChanged(true)
            assertEquals("x".repeat(43), inputs[1].text.toString())
        }
    }

    private fun copy(text: String) {
        RuntimeEnvironment.getApplication().getSystemService(ClipboardManager::class.java)
            .setPrimaryClip(ClipData.newPlainText("test pairing", text))
    }
    private fun paste(activity: MainActivity) {
        val button = descendants(activity.findViewById(android.R.id.content)).filterIsInstance<Button>()
            .firstOrNull { it.text.toString() == "粘贴配对信息" }
        assertNotNull("a paste fallback must be available", button)
        button!!.performClick()
    }
    private fun withForm(check: (MainActivity, List<EditText>) -> Unit) {
        AppRuntime.pairing = false
        val controller = Robolectric.buildActivity(MainActivity::class.java).setup().visible()
        try {
            shadowOf(Looper.getMainLooper()).idle()
            check(controller.get(), descendants(controller.get().findViewById(android.R.id.content)).filterIsInstance<EditText>())
        } finally { controller.pause().stop().destroy() }
    }
    private fun descendants(view: View): List<View> = listOf(view) +
        if (view is ViewGroup) (0 until view.childCount).flatMap { descendants(view.getChildAt(it)) } else emptyList()
}
