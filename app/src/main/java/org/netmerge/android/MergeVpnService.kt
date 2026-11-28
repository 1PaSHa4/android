package org.netmerge.android

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.VpnService
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.os.ParcelFileDescriptor
import android.service.quicksettings.TileService
import android.util.Log
import java.util.concurrent.Executors
import org.json.JSONArray
import org.json.JSONObject

/**
 * Captures all traffic with a TUN and hands it to the Go engine, which re-dials every flow over
 * Wi-Fi or mobile data. The app itself is excluded from the VPN, so the engine's own sockets
 * (pinned to a specific network) leave the phone directly.
 */
class MergeVpnService : VpnService() {

    companion object {
        const val ACTION_START = "org.netmerge.android.START"
        const val ACTION_STOP = "org.netmerge.android.STOP"
        private const val TAG = "NetMerge"
        private const val CHANNEL = "status"
        private const val NOTIF_ID = 1
        private val MTU = if (BuildConfig.EXPERIMENTAL_VPN) 1500 else 9000

        /** How long to wait for the first network before bringing the VPN up anyway. */
        private const val NETWORK_WAIT_MS = 3000L

        val running: Boolean get() = Engine.running

        fun start(ctx: Context) {
            ctx.startForegroundService(Intent(ctx, MergeVpnService::class.java).setAction(ACTION_START))
        }

        fun stop(ctx: Context) {
            ctx.startService(Intent(ctx, MergeVpnService::class.java).setAction(ACTION_STOP))
        }
    }

    private var tun: ParcelFileDescriptor? = null
    private var tracker: NetworkTracker? = null
    private var ticks = 0
    private val worker = Executors.newSingleThreadExecutor()
    private var generation = 0
    private var activeMode = Engine.Mode.MERGE_VPN
    private var shuttingDown = false
    private val main = Handler(Looper.getMainLooper())
    private val refresh = object : Runnable {
        override fun run() {
            runCatching { Engine.sample() }
            if (ticks++ % 2 == 0) updateNotification()
            if (!shuttingDown) main.postDelayed(this, 1000)
        }
    }
    private val establishAnyway = Runnable { establish() }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) {
            shutdown()
            return START_NOT_STICKY
        }
        // ACTION_START, or the system starting us for always-on VPN (action android.net.VpnService).
        goForeground()
        if (Engine.phase.value == Engine.Phase.OFF && !shuttingDown) begin()
        return START_STICKY
    }

    /**
     * Starts tracking networks first and brings the TUN up once at least one is known. Bringing
     * it up with no networks makes every app's first connections fail (and Android briefly marks
     * the VPN as having no internet), which is what made merging sometimes not work right after
     * switching it on.
     */
    private fun begin() {
        generation++
        shuttingDown = false
        Engine.setError(null)
        Engine.setPhase(Engine.Phase.STARTING)
        Engine.loadSettings(this)
        activeMode = Engine.mode.value
        requestTileUpdate()
        val t = NetworkTracker(this) { nets -> main.post { onNetworks(nets) } }
        tracker = t
        try { t.start() } catch (_: Exception) {
            Engine.setError("Could not track underlying networks")
            shutdown()
            return
        }
        main.postDelayed(establishAnyway, NETWORK_WAIT_MS)
    }

    private fun establish() {
        main.removeCallbacks(establishAnyway)
        if (Engine.phase.value != Engine.Phase.STARTING || tun != null || shuttingDown) return
        val nets = tracker?.current().orEmpty()

        val pfd = try {
        val builder = Builder()
            .setSession(getString(R.string.app_name))
            .setMtu(MTU)
            .addAddress("198.18.0.1", 24)
            .addRoute("0.0.0.0", 0)
            // DNS goes into the TUN like everything else and is balanced over both networks.
            .addDnsServer("1.1.1.1")
            .addDnsServer("8.8.8.8")
            .addDisallowedApplication(packageName)
            .setConfigureIntent(
                PendingIntent.getActivity(
                    this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE,
                ),
            )
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) builder.setMetered(false)
        if (nets.isNotEmpty() || BuildConfig.EXPERIMENTAL_VPN) builder.setUnderlyingNetworks(nets.map { it.network }.toTypedArray())
        if (BuildConfig.EXPERIMENTAL_VPN) VpnAppExclusions.applyTo(this, builder)
        // IPv6: a ULA so apps still prefer IPv4 on dual-stack sites (see upstream tun.go).
        if (BuildConfig.EXPERIMENTAL_VPN) {
            builder.addAddress("fd6e:6d65:7267::1", 64)
            builder.addRoute("::", 0)
            builder.addDnsServer("2606:4700:4700::1111")
        } else {
            runCatching {
                builder.addAddress("fd6e:6d65:7267::1", 64)
                builder.addRoute("::", 0)
                builder.addDnsServer("2606:4700:4700::1111")
            }.onFailure { Log.w(TAG, "IPv6 not captured", it) }
        }
        builder.establish()
        } catch (e: Exception) {
            Log.e(TAG, "establish failed")
            null
        }
        if (pfd == null) {
            Engine.setError("Could not establish VPN interface")
            shutdown()
            return
        }
        tun = pfd
        val token = generation
        val snapshot = networkJson(nets)
        val mode = activeMode
        worker.execute {
            val started = runCatching {
                NativeBackend.setNetworks(snapshot)
                NativeBackend.start(this, pfd, MTU, mode)
            }.isSuccess
            main.post {
                if (generation != token || shuttingDown) return@post
                if (!started) {
                    Engine.setError(if (mode == Engine.Mode.MERGE_VPN) "VPN start failed. Check the saved profile and endpoint IP." else "Merge engine start failed")
                    shutdown()
                    return@post
                }
                Engine.setPhase(Engine.Phase.ON)
                main.post(refresh)
                requestTileUpdate()
            }
        }
    }

    private fun onNetworks(nets: List<NetworkTracker.Tracked>) {
        if (shuttingDown || Engine.phase.value == Engine.Phase.OFF) return
        if (tun == null) {
            if (nets.isNotEmpty()) establish()
            return
        }
        val snapshot = networkJson(nets)
        val token = generation
        worker.execute {
            if (runCatching { NativeBackend.setNetworks(snapshot) }.isFailure) {
                runCatching { NativeBackend.setNetworks("[]") }
                main.post {
                    if (generation == token && !shuttingDown) {
                        Engine.setError("Network update failed; VPN stopped")
                        shutdown()
                    }
                }
            }
        }
        val underlying = nets.map { it.network }.toTypedArray()
        runCatching { setUnderlyingNetworks(if (nets.isEmpty() && !BuildConfig.EXPERIMENTAL_VPN) null else underlying) }
        updateNotification()
    }

    private fun networkJson(nets: List<NetworkTracker.Tracked>): String {
        val arr = JSONArray()
        for (n in nets) {
            arr.put(JSONObject()
                .put("name", n.name)
                .put("handle", n.network.networkHandle.toString())
                .put("v4", n.v4)
                .put("v6", n.v6)
                .put("downKbps", n.downKbps))
        }
        return arr.toString()
    }

    private fun shutdown() {
        if (shuttingDown) return
        shuttingDown = true
        generation++
        main.removeCallbacks(refresh)
        main.removeCallbacks(establishAnyway)
        tracker?.stop()
        tracker = null
        val oldTun = tun
        tun = null
        // Serialize stop after any queued start or network update.
        worker.execute {
            runCatching { NativeBackend.stop() }
            runCatching { NativeBackend.setNetworks("[]") }
            runCatching { oldTun?.close() }
            main.post {
                Engine.setPhase(Engine.Phase.OFF)
                stopForeground(STOP_FOREGROUND_REMOVE)
                stopSelf()
                requestTileUpdate()
            }
        }
    }

    override fun onRevoke() {
        // Another VPN started or the user turned us off in Settings.
        shutdown()
    }

    override fun onDestroy() {
        if (Engine.phase.value != Engine.Phase.OFF) shutdown()
        worker.shutdown()
        super.onDestroy()
    }

    private fun requestTileUpdate() {
        TileService.requestListeningState(this, ComponentName(this, MergeTileService::class.java))
    }

    // --- notification ---

    private fun goForeground() {
        val nm = getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(
            NotificationChannel(CHANNEL, getString(R.string.channel_name), NotificationManager.IMPORTANCE_LOW),
        )
        val n = buildNotification(getString(R.string.notif_waiting))
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            // systemExempted covers active VPN apps; fall back to specialUse if the system disagrees.
            try {
                startForeground(NOTIF_ID, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_SYSTEM_EXEMPTED)
            } catch (e: SecurityException) {
                startForeground(NOTIF_ID, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
            }
        } else {
            startForeground(NOTIF_ID, n)
        }
    }

    private fun buildNotification(text: String): Notification {
        val open = PendingIntent.getActivity(
            this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE,
        )
        val stop = PendingIntent.getService(
            this, 1, Intent(this, MergeVpnService::class.java).setAction(ACTION_STOP), PendingIntent.FLAG_IMMUTABLE,
        )
        return Notification.Builder(this, CHANNEL)
            .setSmallIcon(R.drawable.ic_merge)
            .setContentTitle(if (BuildConfig.EXPERIMENTAL_VPN) (if (activeMode == Engine.Mode.MERGE_ONLY) "Merge Only · no VPN encryption" else "Merge + VPN") else getString(R.string.notif_title))
            .setContentText(text)
            .setStyle(Notification.BigTextStyle().bigText(text))
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setContentIntent(open)
            .addAction(Notification.Action.Builder(null, getString(R.string.stop), stop).build())
            .build()
    }

    private fun updateNotification() {
        if (Engine.phase.value != Engine.Phase.ON) return
        val stats = Engine.stats.value
        val text = if (stats.isEmpty()) getString(R.string.notif_waiting)
        else stats.joinToString("\n") { "${it.name}: ${Stats.rate(it.rateIn + it.rateOut)} · ${it.status}" }
        getSystemService(NotificationManager::class.java).notify(NOTIF_ID, buildNotification(text))
    }
}
