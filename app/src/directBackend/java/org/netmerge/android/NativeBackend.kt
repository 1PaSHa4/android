package org.netmerge.android

import android.content.Context
import android.os.ParcelFileDescriptor

internal object NativeBackend {
    fun start(context: Context, pfd: ParcelFileDescriptor, mtu: Int, mode: Engine.Mode) {
        check(mode == Engine.Mode.MERGE_ONLY)
        netmerge.Netmerge.start(pfd.detachFd().toLong(), mtu.toLong())
    }
    fun stop() = netmerge.Netmerge.stop()
    fun setNetworks(json: String) = netmerge.Netmerge.setNetworks(json)
    fun stats(): String = netmerge.Netmerge.stats()
    fun setAutoWeights(on: Boolean) = netmerge.Netmerge.setAutoWeights(on)
    fun validate(link: String, endpoint: String): Boolean = false
}
