package org.netmerge.android

import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.VpnService

/** An explicit denylist: selected applications bypass the VPN by design. */
internal object VpnAppExclusions {
    private const val PREFS = "experimental_vpn_exclusions"
    private const val KEY = "packages"

    internal data class App(val packageName: String, val label: String)

    fun visibleApps(context: Context): List<App> {
        val pm = context.packageManager
        val intent = Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER)
        @Suppress("DEPRECATION")
        return pm.queryIntentActivities(intent, 0)
            .map { App(it.activityInfo.packageName, it.loadLabel(pm).toString()) }
            .filter { it.packageName != context.packageName }
            .distinctBy { it.packageName }
            .sortedBy { it.label.lowercase() }
    }

    fun load(context: Context): Set<String> =
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
            .getStringSet(KEY, emptySet()).orEmpty().toSet() - context.packageName

    fun save(context: Context, packages: Set<String>) {
        val visible = visibleApps(context).map { it.packageName }.toSet()
        val safe = packages.intersect(visible) - context.packageName
        check(context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
            .edit().putStringSet(KEY, safe).commit())
    }

    /** Apply only while constructing a new experimental TUN, before establish(). */
    fun applyTo(context: Context, builder: VpnService.Builder) {
        for (name in load(context)) {
            try {
                builder.addDisallowedApplication(name)
            } catch (_: PackageManager.NameNotFoundException) {
                // App no longer installed; no exclusion required.
            }
        }
    }
}
