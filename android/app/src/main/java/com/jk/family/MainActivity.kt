package com.jk.family

import android.Manifest
import android.annotation.SuppressLint
import android.app.Activity
import android.app.AlertDialog
import android.app.NotificationManager
import android.content.Intent
import android.content.ClipboardManager
import android.content.pm.PackageManager
import android.graphics.Color
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.os.PowerManager
import android.net.Uri
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
    private lateinit var pastePairing: Button
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
        pastePairing = Button(this).apply { text = "粘贴配对信息"; setOnClickListener { importPairing(automatic = false) } }
        fields.addView(pastePairing)
        pair = Button(this).apply { text = "配对此手机"; setOnClickListener { pairDevice() } }
        fields.addView(pair)
        label("在这台手机上复制网页的完整配对信息，打开 App 可自动填写空白表单；也可点击“粘贴配对信息”。请核对服务器地址、填写设备名称，再点击配对。配对成功后共享仍保持关闭。", 14f)
        status = label(AppRuntime.message, 18f)
        permissions = label("", 14f)
        start = button("开启共享") { beginSharing() }
        stop = button("停止共享") { SharingService.requestStop(this); refresh() }
        button("隐私说明与授权") { showPrivacy(false) }
        button("系统定位设置") { startActivity(Intent(Settings.ACTION_LOCATION_SOURCE_SETTINGS)) }
        button("允许后台运行") { openBackgroundSettings() }
        if (Build.VERSION.SDK_INT >= 29) button("允许重启后恢复定位") { openBackgroundLocationSettings() }
        if (Build.VERSION.SDK_INT >= 31) button("允许准时暂停/恢复") { openScheduleSettings() }
        label("开启一次后会记住选择，重新打开 App、手机重启及更新安装包时尝试恢复；停止共享后不再恢复。vivo 请允许后台高耗电、自启动，并在最近任务中锁定本应用。若 i 管家自启动列表没有“家庭位置”，请先安装最新版并打开一次，再检查列表；列表由系统管理，仍找不到时先设置后台高耗电和任务锁定。持续定位会增加耗电。", 14f)
        forget = button("解除本机配对") {
            AlertDialog.Builder(this).setTitle("解除本机配对？")
                .setMessage("会立即停止本机定位并删除本机凭证。还请在家人网页中撤销这台设备；离线时最后位置仍会保留，请在网页撤销设备以清除。")
                .setNegativeButton("取消", null).setPositiveButton("停止并解除") { _, _ ->
                    SharingService.requestStop(this)
                    runCatching { store.clear() }.onSuccess {
                        AppRuntime.credentialsRevision++
                        AppRuntime.message = "已解除本机配对，请在网页撤销原设备。"
                    }.onFailure { AppRuntime.message = "本机凭证删除失败，请重试。" }
                    refresh()
                }.show()
        }
        label("白天移动约 1 分钟更新，静止约 3 分钟并使用低功耗定位；仅近似位置授权时使用兼容模式。每天按手机本地时间 22:00–08:00 暂停采集和上报，保留最后位置；08:00 后恢复。断网暂停请求，联网失败逐步延长重试至最多 5 分钟。停止后的服务器状态以页面提示为准。", 14f)
    }

    override fun onResume() {
        super.onResume()
        revision = -1
        SharingService.requestResume(this)
        handler.post(refreshTask)
    }
    override fun onPause() { handler.removeCallbacks(refreshTask); super.onPause() }

    override fun onWindowFocusChanged(hasFocus: Boolean) {
        super.onWindowFocusChanged(hasFocus)
        // Android 10+ permits clipboard reads only while our window has input focus.
        if (hasFocus && ::pastePairing.isInitialized) importPairing(automatic = true)
    }

    private fun importPairing(automatic: Boolean) {
        refresh()
        if (credentials != null || credentialError || AppRuntime.pairing) return
        if (automatic && listOf(origin, pairingId, code).any { it.text.isNotBlank() }) return
        val info = runCatching {
            val clip = getSystemService(ClipboardManager::class.java).primaryClip
            // Only plain text: do not dereference a clipboard URI or launch an Intent.
            PairingInfo.parse(if (clip != null && clip.itemCount > 0) clip.getItemAt(0).text else null)
        }.getOrNull()
        if (info == null) {
            if (!automatic) showMessage("未读取到有效配对信息，请复制网页的完整 JSON（含地址、标识和数字码），或手工填写。")
            return
        }
        origin.setText(info.origin)
        pairingId.setText(info.id)
        code.setText(info.code)
        showMessage("已填入配对信息，请核对服务器地址、填写设备名称，再点击配对此手机。")
    }

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
        val battery = if (getSystemService(PowerManager::class.java).isIgnoringBatteryOptimizations(packageName))
            "已允许后台运行；仍需检查厂商省电设置" else "后台运行可能受省电限制，请点击下方允许后台运行"
        val recovery = if (Build.VERSION.SDK_INT < 29) "已支持重启恢复；仍需允许厂商自启动"
        else if (checkSelfPermission(Manifest.permission.ACCESS_BACKGROUND_LOCATION) == PackageManager.PERMISSION_GRANTED)
            "已允许重启后恢复定位；仍需允许厂商自启动"
        else "重启自动恢复需允许始终访问位置；未允许时打开 App 会恢复"
        val schedule = if (SharingSchedule.canScheduleExactly(this)) "已允许定时暂停/恢复；仍受厂商后台限制"
            else "未允许精确定时：夜间暂停/早晨恢复可能延迟，请点击允许准时暂停/恢复"
        permissions.setTextIfChanged(getString(R.string.permission_status, location, notification) + "\n" + battery + "\n" + recovery + "\n" + schedule)
        start.isEnabled = credentials != null && !AppRuntime.active && !AppRuntime.pairing
        start.setTextIfChanged(if (AppRuntime.active) "共享已开启" else if (store.sharingRequested) "重试恢复共享" else "开启共享")
        stop.isEnabled = AppRuntime.active || store.sharingRequested
        forget.isEnabled = (credentials != null || credentialError) && !AppRuntime.pairing
        pair.isEnabled = !AppRuntime.pairing
        pastePairing.isEnabled = !AppRuntime.pairing
        pair.setTextIfChanged(if (AppRuntime.pairing) "正在配对…" else "配对此手机")
    }

    // Opt-in exemption for this direct-installed family location app's core lock-screen sharing.
    @SuppressLint("BatteryLife")
    private fun openBackgroundSettings() {
        val power = getSystemService(PowerManager::class.java)
        val intent = if (power.isIgnoringBatteryOptimizations(packageName))
            Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS)
        else Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, Uri.parse("package:$packageName"))
        runCatching { startActivity(intent) }.onFailure {
            runCatching { startActivity(Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.parse("package:$packageName"))) }
                .onFailure { showMessage("请在手机设置中允许家庭位置后台运行，关闭对此应用的省电限制。") }
        }
    }

    private fun openBackgroundLocationSettings() {
        AlertDialog.Builder(this).setTitle("允许重启后恢复定位")
            .setMessage("为了在手机重启后继续你已开启的位置共享，请在系统应用设置的“权限 → 位置信息”中选择“始终允许”。请先授予普通定位权限。该权限只用于恢复已开启的共享；点击停止后不会恢复。暂不允许也可使用共享，重启后打开 App 即可恢复。")
            .setNegativeButton("暂不设置", null)
            .setPositiveButton("去设置") { _, _ ->
                runCatching { startActivity(Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.parse("package:$packageName"))) }
                    .onFailure { showMessage("请在手机设置 → 应用 → 家庭位置 → 权限中设置位置访问权限。") }
            }.show()
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

    private fun openScheduleSettings() {
        if (Build.VERSION.SDK_INT < 31) return
        AlertDialog.Builder(this).setTitle("准时暂停与恢复")
            .setMessage("请在系统中允许“闹钟和提醒”，用于每天按手机本地时间 22:00 暂停定位、08:00 恢复。未允许时系统可能延迟切换，夜间收到的位置仍会丢弃且不上报。此授权不会开启声音闹钟，也不会自动开启共享。")
            .setNegativeButton("暂不设置", null)
            .setPositiveButton("前往设置") { _, _ ->
                runCatching { startActivity(Intent(Settings.ACTION_REQUEST_SCHEDULE_EXACT_ALARM, Uri.parse("package:$packageName"))) }
                    .onFailure { showMessage("请在系统应用设置中为家庭位置允许“闹钟和提醒”。") }
            }.show()
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
                SharingService.requestStop(this, "已撤回隐私授权")
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
