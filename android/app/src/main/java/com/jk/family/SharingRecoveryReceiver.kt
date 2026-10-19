package com.jk.family

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

/** Continue an existing opt-in only; never enable sharing just because the device booted. */
class SharingRecoveryReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != Intent.ACTION_BOOT_COMPLETED && intent.action != Intent.ACTION_MY_PACKAGE_REPLACED) return
        SharingService.requestResume(context, fromBackground = true)
    }
}
