package com.jk.family

import android.Manifest
import android.app.Activity
import android.app.AlertDialog
import android.app.NotificationManager
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Color
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.provider.Settings
import android.text.InputType
import android.text.util.Linkify
import android.view.View
import android.widget.Button
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import com.jk.family.core.ApiException
import com.jk.family.core.DeviceApi

class MainActivity : Activity() {
    private val handler = Handler(Looper.getMainLooper())
    private lateinit var store: CredentialStore
    private lateinit var content: LinearLayout
    private lateinit var fields: LinearLayout
    private lateinit var origin: EditText
    private lateinit var pairingId: EditText
    private lateinit var code: EditText
    private lateinit var name: EditText
    private lateinit var pair: Button
    private lateinit var start: Button
    private lateinit var stop: Button
    private lateinit var forget: Button
    private lateinit var device: TextView
    private lateinit var status: TextView
    private lateinit var permissions: TextView
    private var credentials: Credentials? = null
    private var credentialError = false
    private var revision = -1
    private val refreshTask = object : Runnable {
        override fun run() { refresh(); handler.postDelayed(this, 1000) }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        store = CredentialStore(applicationContext)
        val scroll = ScrollView(this).apply {
            isFillViewport = true
            // Paint the viewport itself so scrolling/capture never relies on the window's clear pass.
            setBackgroundResource(R.color.page_background)
        }
        content = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(24), dp(24), dp(24), dp(32))
        }
        scroll.addView(content)
        // Android 15 edge-to-edge and older status/navigation bars use the same safe content insets.
        scroll.setOnApplyWindowInsetsListener { view, insets ->
            view.setPadding(insets.systemWindowInsetLeft, insets.systemWindowInsetTop, insets.systemWindowInsetRight, insets.systemWindowInsetBottom)
            insets
        }
        setContentView(scroll)
        label("家庭位置", 28f)
        label("把最新位置分享给家人，由你决定何时开启和停止。", 16f)
        device = label("尚未配对", 18f)
        fields = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL }
        content.addView(fields)
        origin = input("服务器地址，例如 http://192.168.1.10:8080", InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_URI)
        pairingId = input("网页生成的配对标识", InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_VISIBLE_PASSWORD)
        code = input("8 位配对码", InputType.TYPE_CLASS_NUMBER or InputType.TYPE_NUMBER_VARIATION_PASSWORD)
        name = input("设备名称，例如 爸爸的手机", InputType.TYPE_CLASS_TEXT)
        pair = Button(this).apply { text = "配对此手机"; setOnClickListener { pairDevice() } }
        fields.addView(pair)
        label("在家人网页的“设备配对”中生成标识和数字码。配对成功后共享仍保持关闭。", 14f)
        status = label(AppRuntime.message, 18f)
        permissions = label("", 14f)
        start = button("开启共享") { beginSharing() }
        stop = button("停止共享") { AppRuntime.controller?.stop(); refresh() }
        button("隐私说明与授权") { showPrivacy(false) }
        button("系统定位设置") { startActivity(Intent(Settings.ACTION_LOCATION_SOURCE_SETTINGS)) }
        forget = button("解除本机配对") {
            AlertDialog.Builder(this).setTitle("解除本机配对？")
                .setMessage("会立即停止本机定位并删除本机凭证。还请在家人网页中撤销这台设备；离线时服务器位置最迟在最后成功上传后 7 分钟隐藏。")
                .setNegativeButton("取消", null).setPositiveButton("停止并解除") { _, _ ->
                    AppRuntime.controller?.stop()
                    runCatching { store.clear() }.onSuccess {
                        AppRuntime.credentialsRevision++
                        AppRuntime.message = "已解除本机配对，请在网页撤销原设备。"
                    }.onFailure { AppRuntime.message = "本机凭证删除失败，请重试。" }
                    refresh()
                }.show()
        }
        label("移动时目标约 30 秒更新，静止约 3 分钟。只保存最新位置；断网和省电限制可能延迟更新。停止后的服务器状态以页面提示为准。", 14f)
    }

    override fun onResume() { super.onResume(); revision = -1; handler.post(refreshTask) }
    override fun onPause() { handler.removeCallbacks(refreshTask); super.onPause() }

    private fun refresh() {
        if (revision != AppRuntime.credentialsRevision) {
            credentialError = false
            credentials = try { store.read() } catch (_: Exception) { credentialError = true; null }
            revision = AppRuntime.credentialsRevision
        }
        fields.visibility = if (credentials == null && !credentialError) View.VISIBLE else View.GONE
        device.setTextIfChanged(credentials?.let { "已配对：${it.name}\n${it.origin}" }
            ?: if (credentialError) "凭证无法读取，请解除本机配对后重新配对。" else "尚未配对")
        status.setTextIfChanged(AppRuntime.message)
        val fine = checkSelfPermission(Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED
        val location = if (fine) "精确定位已授权" else if (AmapSource.hasLocationPermission(this)) "仅近似位置，精度受限" else "定位尚未授权"
        val notification = if (getSystemService(NotificationManager::class.java).areNotificationsEnabled()) "通知已开启" else "通知未开启，可在 App 内停止共享"
        permissions.setTextIfChanged(getString(R.string.permission_status, location, notification))
        start.isEnabled = credentials != null && !AppRuntime.active && !AppRuntime.pairing
        stop.isEnabled = AppRuntime.active
        forget.isEnabled = (credentials != null || credentialError) && !AppRuntime.pairing
        pair.isEnabled = !AppRuntime.pairing
        pair.setTextIfChanged(if (AppRuntime.pairing) "正在配对…" else "配对此手机")
    }

    private fun pairDevice() {
        val address = try { DeviceApi.normalizeOrigin(origin.text.toString().trim()) }
        catch (_: Exception) { showMessage("请输入 HTTP(S) 服务器地址，仅包含主机和端口。"); return }
        val id = pairingId.text.toString().trim()
        val digits = code.text.toString().trim()
        val deviceName = name.text.toString().trim()
        if (id.isBlank() || !digits.matches(Regex("[0-9]{8}")) || deviceName.isBlank() || deviceName.codePointCount(0, deviceName.length) > 80) {
            showMessage("请填写配对标识、8 位数字码和 1–80 字设备名称。"); return
        }
        AppRuntime.pairing = true
        code.text.clear()
        refresh()
        val appContext = applicationContext
        AppRuntime.io.execute {
            val result = runCatching {
                val paired = DeviceApi(address).pair(id, digits, deviceName)
                CredentialStore(appContext).save(Credentials(address, paired.token, paired.deviceName))
            }
            handler.post {
                AppRuntime.pairing = false
                AppRuntime.credentialsRevision++
                AppRuntime.message = if (result.isSuccess) "配对成功，共享保持关闭。准备好后点击开启。"
                else when ((result.exceptionOrNull() as? ApiException)?.status) {
                    400 -> "配对标识或数字码无效，请在网页重新生成。"
                    429 -> "配对过于频繁，请 10 分钟后重试。"
                    else -> "配对未完成，请检查网络；若配对码已被消费，请在网页撤销该设备并生成新码。"
                }
                if (!isDestroyed && !isFinishing) refresh()
            }
        }
    }

    private fun beginSharing() {
        if (credentials == null || AppRuntime.active) return
        val key = packageManager.getApplicationInfo(packageName, PackageManager.GET_META_DATA).metaData?.getString("com.amap.api.v2.apikey")
        if (key.isNullOrBlank()) { showMessage("此安装包尚未配置高德 Android Key，请按项目 ANDROID 文档配置后重新构建。"); return }
        if (!store.consent) { showPrivacy(true); return }
        if (!AmapSource.hasLocationPermission(this)) {
            requestPermissions(arrayOf(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_COARSE_LOCATION), LOCATION_REQUEST)
            return
        }
        requestNotificationsThenStart()
    }

    private fun requestNotificationsThenStart() {
        val prefs = getSharedPreferences("ui", MODE_PRIVATE)
        if (Build.VERSION.SDK_INT >= 33 && checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED && !prefs.getBoolean("notification_requested", false)) {
            prefs.edit().putBoolean("notification_requested", true).apply()
            requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), NOTIFICATION_REQUEST)
        } else launchSharing()
    }

    private fun launchSharing() {
        val intent = Intent(this, SharingService::class.java).setAction(SharingService.START)
        try {
            if (Build.VERSION.SDK_INT >= 26) startForegroundService(intent) else startService(intent)
            showMessage("正在开启共享…")
        } catch (_: Exception) { showMessage("系统未允许启动共享，请保持 App 在前台并检查权限。") }
    }

    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<out String>, grantResults: IntArray) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        when (requestCode) {
            LOCATION_REQUEST -> if (AmapSource.hasLocationPermission(this)) requestNotificationsThenStart()
                else showMessage("未授权定位，共享保持关闭。可在系统应用权限中重新授权。")
            NOTIFICATION_REQUEST -> launchSharing()
        }
    }

    private fun showPrivacy(startAfterConsent: Boolean) {
        val text = TextView(this).apply {
            setPadding(dp(24), dp(16), dp(24), dp(16))
            setText(R.string.privacy_body)
            autoLinkMask = Linkify.WEB_URLS
            Linkify.addLinks(this, Linkify.WEB_URLS)
        }
        AlertDialog.Builder(this).setTitle("位置共享与隐私")
            .setView(ScrollView(this).apply { addView(text) })
            .setNegativeButton("不同意 / 撤回授权") { _, _ ->
                AppRuntime.controller?.stop("已撤回隐私授权")
                store.consent = false
                refresh()
            }
            .setPositiveButton("同意") { _, _ ->
                store.consent = true
                if (startAfterConsent) beginSharing()
            }.show()
    }

    private fun showMessage(message: String) { AppRuntime.message = message; refresh() }
    private fun TextView.setTextIfChanged(value: String) {
        if (text.toString() != value) text = value
    }
    private fun label(text: String, size: Float): TextView = TextView(this).apply {
        this.text = text; textSize = size; setTextColor(Color.rgb(32, 48, 40))
        setPadding(0, dp(8), 0, dp(8)); content.addView(this)
    }
    private fun input(hint: String, type: Int): EditText = EditText(this).apply {
        this.hint = hint; inputType = type; isSingleLine = true; isSaveEnabled = false
        if (Build.VERSION.SDK_INT >= 26) importantForAutofill = View.IMPORTANT_FOR_AUTOFILL_NO
        fields.addView(this, LinearLayout.LayoutParams(-1, dp(56)))
    }
    private fun button(text: String, click: () -> Unit): Button = Button(this).apply {
        this.text = text; setOnClickListener { click() }; content.addView(this)
    }
    private fun dp(value: Int) = (value * resources.displayMetrics.density).toInt()

    companion object { private const val LOCATION_REQUEST = 1; private const val NOTIFICATION_REQUEST = 2 }
}
