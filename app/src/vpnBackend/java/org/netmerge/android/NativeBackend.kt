package org.netmerge.android

import android.content.Context
import android.os.ParcelFileDescriptor

// This build contains only the encrypted Go backend. No direct-mode fallback.
internal object NativeBackend {
    fun start(context: Context, pfd: ParcelFileDescriptor, mtu: Int) {
        val profile = VpnProfileStore.load(context) ?: error("VPN profile required")
        vpnmobile.Vpnmobile.validateEndpoint(profile.link, profile.endpointIp)
        vpnmobile.Vpnmobile.start(pfd.detachFd().toLong(), mtu.toLong(), profile.link, profile.endpointIp)
    }
    fun stop() = vpnmobile.Vpnmobile.stop()
    fun setNetworks(json: String) = vpnmobile.Vpnmobile.setNetworks(json)
    fun stats(): String = vpnmobile.Vpnmobile.stats()
    fun setAutoWeights(on: Boolean) = vpnmobile.Vpnmobile.setAutoWeights(on)
    fun validate(link: String, endpoint: String): Boolean =
        runCatching { vpnmobile.Vpnmobile.validateEndpoint(link, endpoint) }.isSuccess
}
